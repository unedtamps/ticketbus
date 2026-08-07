//go:build integration

// Package integration boots the full stack (Postgres + Kafka + auth/ticketing/
// payment services) via testcontainers and exercises the public HTTP APIs
// end-to-end in a single package — one harness, one container lifecycle.
//
// Running the suite:
//
//	go test -tags integration ./tests/integration/
//
// Per-area filtering — test names follow Test<Area>_<Scenario>[_<Variant>]
// with areas: Auth, Event, Booking, Payment, Webhook, Expiry, Refund, Mock,
// E2E, Concurrency:
//
//	go test -tags integration -run 'TestAuth|TestPayment|TestWebhook' ./tests/integration/
//
// Note: keep every test in this single package. A separate package per
// service would boot a fresh Postgres+Kafka per test binary (each package
// runs in its own process), multiplying infra cost without sharing state.
package integration

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/nedo/TicketSaas/internal/auth/application"
	"github.com/nedo/TicketSaas/internal/auth/bcrypt"
	authhandler "github.com/nedo/TicketSaas/internal/auth/handler"
	"github.com/nedo/TicketSaas/internal/auth/jwt"
	authpostgres "github.com/nedo/TicketSaas/internal/auth/postgres"
	payapp "github.com/nedo/TicketSaas/internal/payment/application"
	payhandler "github.com/nedo/TicketSaas/internal/payment/handler"
	paykafka "github.com/nedo/TicketSaas/internal/payment/kafka"
	paypostgres "github.com/nedo/TicketSaas/internal/payment/postgres"
	"github.com/nedo/TicketSaas/internal/payment/processor"
	sharedkafka "github.com/nedo/TicketSaas/internal/shared/kafka"
	"github.com/nedo/TicketSaas/internal/shared/log"
	"github.com/nedo/TicketSaas/internal/shared/outbox"
	bookingpkg "github.com/nedo/TicketSaas/internal/ticketing/application/booking"
	eventpkg "github.com/nedo/TicketSaas/internal/ticketing/application/event"
	eventhandler "github.com/nedo/TicketSaas/internal/ticketing/handler"
	eventkafka "github.com/nedo/TicketSaas/internal/ticketing/kafka"
	ticketingpayment "github.com/nedo/TicketSaas/internal/ticketing/payment"
	eventpostgres "github.com/nedo/TicketSaas/internal/ticketing/postgres"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour
)

var (
	testEnv *TestEnv
	once    sync.Once
)

func TestMain(m *testing.M) {
	code := m.Run()
	if testEnv != nil {
		testEnv.cleanup()
	}
	os.Exit(code)
}

func startContainers() *TestEnv {
	ctx := context.Background()

	// ── Infrastructure: Postgres (3 DBs) + Kafka ──
	adminPool, ticketingPool, payPool, brokerList, kafkaProducer, pgContainer, kafkaContainer := startInfra(ctx)

	// ── JWT keys ──
	jwtPrivatePEM, jwtPublicPEM := generateRSAKeys()

	// ── Auth Service ──
	authSvc, tokenSvc, authHandler := setupAuthService(adminPool, jwtPrivatePEM, jwtPublicPEM)
	authSrv := httptest.NewServer(authHandler.Routes())

	// ── Payment Service ──
	payLogger := log.New("payment", "warn")
	paySvc, payHandler, mockProcessor := setupPaymentService(payPool, brokerList, payLogger)
	paySrv := httptest.NewServer(payHandler.Routes())
	mockProcessor.SetWebhookURL(paySrv.URL + "/api/payments/webhook")
	mockProcessor.SetCallbackToken("test-webhook-token")

	// ── Ticketing Service ──
	ticketingLogger := log.New("ticketing", "warn")
	eventSvc, invSvc, eventHandler, invHandler := setupTicketingService(ticketingPool, brokerList, paySrv.URL, ticketingLogger)
	ticketingRouter := chi.NewRouter()
	eventHandler.Routes(ticketingRouter)
	invHandler.Routes(ticketingRouter)
	ticketingSrv := httptest.NewServer(ticketingRouter)

	// ── Seed admin ──
	n, err := authSvc.SeedAdmins(ctx, []application.AdminSeed{
		{Email: "admin@test.com", Password: "Admin123!", Name: "Admin"},
	})
	if err != nil {
		fmt.Printf("WARNING: seed admin: %v\n", err)
	} else if n > 0 {
		fmt.Println("  admin seeded")
	}

	// ── Start background goroutines ──
	ctxBg, cancelBg := context.WithCancel(context.Background())

	go outbox.NewWorker(ticketingPool, kafkaProducer, ticketingLogger, 1, 200).Run(ctxBg)
	go outbox.NewWorker(payPool, kafkaProducer, payLogger, 1, 200).Run(ctxBg)

	go func() { _ = invSvc.StartConsumers(ctxBg) }()
	go func() { _ = paySvc.StartConsumer(ctxBg) }()
	paySvc.StartExpiryPoller(ctxBg, 1)

	// Allow the outbox workers and kafka writer connections to warm up;
	// the first produce to a topic can block for seconds on a fresh broker.
	time.Sleep(8 * time.Second)

	return &TestEnv{
		authPool:  adminPool,
		eventPool: ticketingPool,
		invPool:   ticketingPool,
		payPool:   payPool,
		adminPool: adminPool,
		containers: containers{
			pg:    pgContainer,
			kafka: kafkaContainer,
		},
		kafkaProducer: kafkaProducer,
		authSvc:       authSvc,
		eventSvc:      eventSvc,
		invSvc:        invSvc,
		paySvc:        paySvc,
		tokenSvc:      tokenSvc,
		jwtPublicPEM:  jwtPublicPEM,
		authURL:       authSrv.URL,
		eventURL:      ticketingSrv.URL,
		invURL:        ticketingSrv.URL,
		payURL:        paySrv.URL,
		cancelBg:      cancelBg,
		authSrv:       authSrv,
		ticketingSrv:  ticketingSrv,
		paySrv:        paySrv,
	}
}

// startInfra boots Postgres (3 databases) and Kafka, runs migrations, and
// returns the connection pools, broker list, producer, and container handles.
func startInfra(ctx context.Context) (
	adminPool, ticketingPool, payPool *pgxpool.Pool,
	brokerList []string,
	kafkaProducer *sharedkafka.Producer,
	pgContainer *tcpostgres.PostgresContainer,
	kafkaContainer *tckafka.KafkaContainer,
) {
	// ── Postgres ──
	pgContainer, err := tcpostgres.Run(
		ctx, "postgres:16-alpine",
		tcpostgres.WithUsername("ticketsaas"),
		tcpostgres.WithPassword("ticketsaas"),
		tcpostgres.WithDatabase("auth_db"),
	)
	if err != nil {
		panic(fmt.Sprintf("postgres: %v", err))
	}

	// Allow postgres a moment to stabilize after container start
	time.Sleep(2 * time.Second)

	pgHost, err := pgContainer.Host(ctx)
	if err != nil {
		panic(fmt.Sprintf("pg host: %v", err))
	}
	pgPort, err := pgContainer.MappedPort(ctx, "5432")
	if err != nil {
		panic(fmt.Sprintf("pg port: %v", err))
	}

	adminDSN := fmt.Sprintf(
		"postgres://ticketsaas:ticketsaas@%s:%s/auth_db?sslmode=disable",
		pgHost, pgPort.Port(),
	)
	adminPool = mustNewPool(adminDSN)

	for _, db := range []string{"ticketing_db", "payment_db"} {
		if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", db)); err != nil {
			panic(fmt.Sprintf("create db %s: %v", db, err))
		}
	}

	ticketingPool = mustNewPool(fmt.Sprintf(
		"postgres://ticketsaas:ticketsaas@%s:%s/ticketing_db?sslmode=disable",
		pgHost, pgPort.Port(),
	))
	payPool = mustNewPool(fmt.Sprintf(
		"postgres://ticketsaas:ticketsaas@%s:%s/payment_db?sslmode=disable",
		pgHost, pgPort.Port(),
	))

	runMigrations("auth", adminPool)
	runMigrations("ticketing", ticketingPool)
	runMigrations("payment", payPool)

	// ── Kafka ──
	kafkaContainer, err = tckafka.Run(
		ctx, "confluentinc/cp-kafka:7.7.0",
		testcontainers.WithEnv(map[string]string{
			"CLUSTER_ID": "5L6g3nShT-eMCtK--X86sw",
		}),
	)
	if err != nil {
		panic(fmt.Sprintf("kafka: %v", err))
	}
	kafkaBrokers, err := kafkaContainer.Brokers(ctx)
	if err != nil {
		panic(fmt.Sprintf("kafka brokers: %v", err))
	}
	brokerList = []string{kafkaBrokers[0]}

	kafkaProducer = sharedkafka.NewProducer(brokerList)
	waitForKafka(brokerList, kafkaProducer)
	return
}

// setupAuthService wires the auth service (users, refresh tokens, JWT).
func setupAuthService(
	pool *pgxpool.Pool,
	jwtPrivatePEM, jwtPublicPEM string,
) (*application.AuthService, *jwt.TokenService, *authhandler.AuthHandler) {
	userRepo := authpostgres.NewUserRepo(pool)
	tokenRepo := authpostgres.NewRefreshTokenRepo(pool)
	tokenSvc, err := jwt.NewTokenService(jwtPrivatePEM, jwtPublicPEM, accessTokenTTL)
	if err != nil {
		panic(fmt.Sprintf("token svc: %v", err))
	}
	authSvc := application.NewAuthService(
		userRepo, tokenRepo, bcrypt.NewHasher(), tokenSvc,
		application.TokensConfig{AccessTokenTTL: accessTokenTTL, RefreshTokenTTL: refreshTokenTTL},
	)
	return authSvc, tokenSvc, authhandler.NewAuthHandler(authSvc)
}

// setupPaymentService wires the payment service with the mock processor.
func setupPaymentService(
	pool *pgxpool.Pool,
	brokers []string,
	logger *slog.Logger,
) (*payapp.PaymentService, *payhandler.PaymentHandler, *processor.MockProcessor) {
	txnRepo := paypostgres.NewTransactionRepo(pool)
	refundRepo := paypostgres.NewRefundRepo(pool)
	mockProcessor := processor.NewMockProcessor("")
	payConsumer := paykafka.NewPaymentConsumer(brokers, "payment-service-test", 1)
	payOutbox := outbox.NewStore(pool)
	paySvc := payapp.NewPaymentService(
		txnRepo, refundRepo, mockProcessor, payConsumer, payOutbox,
		logger, "mock", 5, []string{"ID_QRIS", "ID_BCA_VA"},
	)
	return paySvc, payhandler.NewPaymentHandler(paySvc, "test-internal-key", "test-webhook-token"), mockProcessor
}

// setupTicketingService wires the ticketing service (events + bookings).
func setupTicketingService(
	pool *pgxpool.Pool,
	brokers []string,
	payURL string,
	logger *slog.Logger,
) (*eventpkg.EventService, *bookingpkg.BookingService, *eventhandler.EventHandler, *eventhandler.BookingHandler) {
	eventRepo := eventpostgres.NewEventRepo(pool)
	seatReader := eventpostgres.NewSeatReader(pool)
	ticketingOutbox := outbox.NewStore(pool)
	bookingRepo := eventpostgres.NewBookingRepo(pool)
	seatCounter := eventpostgres.NewSeatCounter(pool)
	eventStatusRepo := eventpostgres.NewEventStatusRepo(pool)
	invConsumer := eventkafka.NewTicketingConsumer(brokers, "ticketing-service-test", 1)
	paymentClient := ticketingpayment.NewClient(payURL, "test-internal-key", 5)
	invSvc := bookingpkg.NewBookingService(
		bookingRepo, paymentClient, seatCounter, invConsumer, eventStatusRepo,
		ticketingOutbox, logger, 3600,
	)
	eventSvc := eventpkg.NewEventService(eventRepo, seatReader, seatCounter, ticketingOutbox)
	return eventSvc, invSvc, eventhandler.NewEventHandler(eventSvc), eventhandler.NewBookingHandler(invSvc)
}

// waitForKafka creates topics then smoke-tests the broker.
func waitForKafka(brokers []string, producer *sharedkafka.Producer) {
	topics := []string{
		"event.cancelled",
		"ticket.issued",
		"payment.completed", "payment.expired",
		"test-smoke",
	}

	// Phase 1: create topics
	for i := 0; i < 15; i++ {
		if i > 0 {
			time.Sleep(2 * time.Second)
		}
		if sharedkafka.EnsureTopics(brokers, topics, 1, 1) == nil {
			break
		}
		if i == 14 {
			panic("kafka: ensure topics failed after 15 retries")
		}
	}

	// Phase 2: smoke test — broker must accept a produce
	for i := 0; i < 20; i++ {
		if i > 0 {
			time.Sleep(time.Second)
		}
		if producer.Produce(context.Background(), "test-smoke", "healthcheck",
			map[string]string{"ping": "pong"}) == nil {
			return
		}
	}
	panic(fmt.Sprintf("kafka: smoke test failed after 20 retries (broker: %s)", brokers[0]))
}

func mustNewPool(dsn string) *pgxpool.Pool {
	var pool *pgxpool.Pool
	var err error
	for i := 0; i < 10; i++ {
		if i > 0 {
			time.Sleep(time.Second)
		}
		pool, err = pgxpool.New(context.Background(), dsn)
		if err != nil {
			continue
		}
		if err = pool.Ping(context.Background()); err == nil {
			return pool
		}
		pool.Close()
	}
	panic(fmt.Sprintf("mustNewPool failed after retries (last err: %v)", err))
}

func runMigrations(service string, pool *pgxpool.Pool) {
	dir := filepath.Join("../../migrations", service)
	entries, err := os.ReadDir(dir)
	if err != nil {
		panic(fmt.Sprintf("migrations dir %s: %v", dir, err))
	}

	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		path := filepath.Join(dir, f)
		sql, err := os.ReadFile(path)
		if err != nil {
			panic(fmt.Sprintf("read %s: %v", f, err))
		}
		if _, err := pool.Exec(context.Background(), string(sql)); err != nil {
			panic(fmt.Sprintf("migrate %s/%s: %v\n%s", service, f, err, string(sql)))
		}
	}
}

func generateRSAKeys() (privatePEM, publicPEM string) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("rsa: %v", err))
	}
	privBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privatePEM = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes}))
	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		panic(fmt.Sprintf("pub: %v", err))
	}
	publicPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes}))
	return
}

func getTestEnv() *TestEnv {
	once.Do(func() { testEnv = startContainers() })
	return testEnv
}

func (env *TestEnv) cleanup() {
	ctx := context.Background()
	env.cancelBg()
	env.authSrv.Close()
	env.ticketingSrv.Close()
	env.paySrv.Close()
	_ = env.kafkaProducer.Close()
	env.authPool.Close()
	env.eventPool.Close()
	env.payPool.Close()
	env.adminPool.Close()
	_ = env.containers.pg.Terminate(ctx)
	_ = env.containers.kafka.Terminate(ctx)
}
