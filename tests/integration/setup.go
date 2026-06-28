//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/nedo/TicketSaas/internal/auth/application"
	"github.com/nedo/TicketSaas/internal/auth/bcrypt"
	authhandler "github.com/nedo/TicketSaas/internal/auth/handler"
	"github.com/nedo/TicketSaas/internal/auth/jwt"
	authpostgres "github.com/nedo/TicketSaas/internal/auth/postgres"
	eventapp "github.com/nedo/TicketSaas/internal/event/application"
	eventhandler "github.com/nedo/TicketSaas/internal/event/handler"
	eventkafka "github.com/nedo/TicketSaas/internal/event/kafka"
	eventpostgres "github.com/nedo/TicketSaas/internal/event/postgres"
	eventredis "github.com/nedo/TicketSaas/internal/event/redis"
	invapp "github.com/nedo/TicketSaas/internal/inventory/application"
	invhandler "github.com/nedo/TicketSaas/internal/inventory/handler"
	invkafka "github.com/nedo/TicketSaas/internal/inventory/kafka"
	invpostgres "github.com/nedo/TicketSaas/internal/inventory/postgres"
	invredis "github.com/nedo/TicketSaas/internal/inventory/redis"
	payapp "github.com/nedo/TicketSaas/internal/payment/application"
	payhandler "github.com/nedo/TicketSaas/internal/payment/handler"
	paykafka "github.com/nedo/TicketSaas/internal/payment/kafka"
	paypostgres "github.com/nedo/TicketSaas/internal/payment/postgres"
	"github.com/nedo/TicketSaas/internal/payment/processor"
	"github.com/nedo/TicketSaas/internal/shared/log"
	"github.com/nedo/TicketSaas/internal/shared/outbox"

	sharedkafka "github.com/nedo/TicketSaas/internal/shared/kafka"
	"github.com/testcontainers/testcontainers-go"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
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
	time.Sleep(2 * time.Second) // ⬅ new

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
		pgHost,
		pgPort.Port(),
	)
	adminPool := mustNewPool(adminDSN)

	for _, db := range []string{"event_db", "inventory_db", "payment_db"} {
		_, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", db))
		if err != nil {
			panic(fmt.Sprintf("create db %s: %v", db, err))
		}
	}

	eventDSN := fmt.Sprintf(
		"postgres://ticketsaas:ticketsaas@%s:%s/event_db?sslmode=disable",
		pgHost,
		pgPort.Port(),
	)
	invDSN := fmt.Sprintf(
		"postgres://ticketsaas:ticketsaas@%s:%s/inventory_db?sslmode=disable",
		pgHost,
		pgPort.Port(),
	)
	payDSN := fmt.Sprintf(
		"postgres://ticketsaas:ticketsaas@%s:%s/payment_db?sslmode=disable",
		pgHost,
		pgPort.Port(),
	)

	authPool := mustNewPool(adminDSN)
	eventPool := mustNewPool(eventDSN)
	invPool := mustNewPool(invDSN)
	payPool := mustNewPool(payDSN)

	runMigrations("auth", adminPool)
	runMigrations("event", eventPool)
	runMigrations("inventory", invPool)
	runMigrations("payment", payPool)

	// ── Redis ──
	redisReq := testcontainers.ContainerRequest{
		Image:        "redis:7-alpine",
		ExposedPorts: []string{"6379/tcp"},
		Cmd:          []string{"redis-server", "--notify-keyspace-events", "Ex"},
	}
	redisContainer, err := testcontainers.GenericContainer(
		ctx,
		testcontainers.GenericContainerRequest{
			ContainerRequest: redisReq,
			Started:          true,
		},
	)
	if err != nil {
		panic(fmt.Sprintf("redis: %v", err))
	}

	redisHost, err := redisContainer.Host(ctx)
	if err != nil {
		panic(fmt.Sprintf("redis host: %v", err))
	}
	redisPort, err := redisContainer.MappedPort(ctx, "6379")
	if err != nil {
		panic(fmt.Sprintf("redis port: %v", err))
	}
	redisAddr := fmt.Sprintf("%s:%s", redisHost, redisPort.Port())

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		panic(fmt.Sprintf("redis ping: %v", err))
	}

	// ── Kafka ──
	kafkaContainer, err := tckafka.Run(
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
	brokerList := []string{kafkaBrokers[0]}

	kafkaProducer := sharedkafka.NewProducer(brokerList)
	waitForKafka(brokerList, kafkaProducer)

	// ── JWT keys ──
	jwtPrivatePEM, jwtPublicPEM := generateRSAKeys()

	// ── Auth Service ──
	authLogger := log.New("auth", "warn")
	userRepo := authpostgres.NewUserRepo(authPool)
	tokenRepo := authpostgres.NewRefreshTokenRepo(authPool)
	hasher := bcrypt.NewHasher()
	tokenSvc, err := jwt.NewTokenService(jwtPrivatePEM, jwtPublicPEM, accessTokenTTL)
	if err != nil {
		panic(fmt.Sprintf("token svc: %v", err))
	}
	authOutbox := outbox.NewStore(authPool)
	authSvc := application.NewAuthService(
		userRepo, tokenRepo, hasher, tokenSvc,
		application.TokensConfig{AccessTokenTTL: accessTokenTTL, RefreshTokenTTL: refreshTokenTTL},
		authOutbox,
	)
	authHandler := authhandler.NewAuthHandler(authSvc)
	authSrv := httptest.NewServer(authHandler.Routes())

	authOutboxWorker := outbox.NewWorker(authPool, kafkaProducer, authLogger)

	// ── Event Service ──
	eventLogger := log.New("event", "warn")
	eventRepo := eventpostgres.NewEventRepo(eventPool)
	seatReader := eventredis.NewSeatReader(rdb)
	orgConsumer := eventkafka.NewOrganizerConsumer(brokerList, "event-service-test")
	eventOutbox := outbox.NewStore(eventPool)
	eventSvc := eventapp.NewEventService(eventRepo, orgConsumer, seatReader, eventOutbox)
	eventHandler := eventhandler.NewEventHandler(eventSvc)
	eventSrv := httptest.NewServer(eventHandler.Routes())

	eventOutboxWorker := outbox.NewWorker(eventPool, kafkaProducer, eventLogger)

	// ── Inventory Service ──
	invLogger := log.New("inventory", "warn")
	bookingRepo := invpostgres.NewBookingRepo(invPool)
	reservationCache := invredis.NewReservationCache(rdb)
	seatCounter := invredis.NewSeatCounter(rdb)
	eventStatusRepo := invpostgres.NewEventStatusRepo(invPool)
	invConsumer := invkafka.NewInventoryConsumer(brokerList, "inventory-service-test")
	invOutbox := outbox.NewStore(invPool)
	invSvc := invapp.NewInventoryService(
		bookingRepo,
		reservationCache,
		seatCounter,
		invConsumer,
		eventStatusRepo,
		invOutbox,
		invLogger,
		30,
	)
	invHandler := invhandler.NewInventoryHandler(invSvc)
	invSrv := httptest.NewServer(invHandler.Routes())

	invOutboxWorker := outbox.NewWorker(invPool, kafkaProducer, invLogger)

	// ── Payment Service ──
	payLogger := log.New("payment", "warn")
	txnRepo := paypostgres.NewTransactionRepo(payPool)
	mockProcessor := processor.NewMockProcessor()
	payConsumer := paykafka.NewPaymentConsumer(brokerList, "payment-service-test")
	payOutbox := outbox.NewStore(payPool)
	paySvc := payapp.NewPaymentService(
		txnRepo,
		mockProcessor,
		payConsumer,
		payOutbox,
		payLogger,
		"",
	)
	payHandler := payhandler.NewPaymentHandler(paySvc)
	paySrv := httptest.NewServer(payHandler.Routes())

	payOutboxWorker := outbox.NewWorker(payPool, kafkaProducer, payLogger)

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

	go authOutboxWorker.Run(ctxBg)
	go eventOutboxWorker.Run(ctxBg)
	go invOutboxWorker.Run(ctxBg)
	go payOutboxWorker.Run(ctxBg)

	go eventSvc.StartOrganizerConsumer(ctxBg)
	go func() { _ = invSvc.StartConsumers(ctxBg) }()
	invSvc.StartExpiryListener(ctxBg)
	go func() { _ = paySvc.StartConsumer(ctxBg) }()

	time.Sleep(3 * time.Second)

	return &TestEnv{
		authPool:  authPool,
		eventPool: eventPool,
		invPool:   invPool,
		payPool:   payPool,
		rdb:       rdb,
		adminPool: adminPool,
		containers: containers{
			pg:    pgContainer,
			redis: redisContainer,
			kafka: kafkaContainer,
		},
		kafkaProducer:    kafkaProducer,
		authSvc:          authSvc,
		eventSvc:         eventSvc,
		invSvc:           invSvc,
		paySvc:           paySvc,
		tokenSvc:         tokenSvc,
		jwtPublicPEM:     jwtPublicPEM,
		authURL:          authSrv.URL,
		eventURL:         eventSrv.URL,
		invURL:           invSrv.URL,
		payURL:           paySrv.URL,
		cancelBg:         cancelBg,
		authSrv:          authSrv,
		eventSrv:         eventSrv,
		invSrv:           invSrv,
		paySrv:           paySrv,
		reservationCache: reservationCache,
	}
}

// waitForKafka creates topics then smoke-tests the broker.
func waitForKafka(brokers []string, producer *sharedkafka.Producer) {
	topics := []string{
		"organizer.created",
		"event.created", "event.approved", "event.rejected", "event.updated", "event.cancelled",
		"reservation.created", "reservation.expired", "ticket.issued",
		"payment.initiated", "payment.completed", "payment.failed",
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
	env.eventSrv.Close()
	env.invSrv.Close()
	env.paySrv.Close()
	_ = env.kafkaProducer.Close()
	_ = env.rdb.Close()
	env.authPool.Close()
	env.eventPool.Close()
	env.invPool.Close()
	env.payPool.Close()
	env.adminPool.Close()
	_ = env.containers.pg.Terminate(ctx)
	_ = env.containers.redis.Terminate(ctx)
	_ = env.containers.kafka.Terminate(ctx)
}
