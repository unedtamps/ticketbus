//go:build integration

// Package integration is the ticketing service's integration suite. It boots
// Postgres and Kafka via pkg/testinfra, wires the event and booking handlers
// against them in-process, and exercises the public HTTP API.
//
// The suite imports nothing from the other services. Caller identity comes from
// synthesized X-Authenticated-* headers, which WithUserContext trusts without
// signature verification; the payment service is replaced by a fake
// domain.PaymentClient driven by the outbox REST dispatcher; and upstream
// payment events are published straight to Kafka, standing in for the payment
// service.
//
// Running the suite:
//
//	go test -tags=integration ./service/ticketing/tests/integration/
//
// Per-area filtering — test names follow Test<Area>_<Scenario>[_<Variant>]
// with areas: Event, Booking, Concurrency:
//
//	go test -tags=integration -run 'TestEvent|TestBooking' ./service/ticketing/tests/integration/
package integration

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/pkg/log"
	"github.com/unedtamps/ticketbus/pkg/outbox"
	"github.com/unedtamps/ticketbus/pkg/s3"
	"github.com/unedtamps/ticketbus/pkg/testinfra"
	bookingpkg "github.com/unedtamps/ticketbus/service/ticketing/internal/application/booking"
	eventpkg "github.com/unedtamps/ticketbus/service/ticketing/internal/application/event"
	ticketinghandler "github.com/unedtamps/ticketbus/service/ticketing/internal/handler"
	eventkafka "github.com/unedtamps/ticketbus/service/ticketing/internal/kafka"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/payment"
	eventpostgres "github.com/unedtamps/ticketbus/service/ticketing/internal/postgres"
)

const consumerGroup = "ticketing-service-test"

var (
	testEnv *TestEnv
	once    sync.Once
)

// TestEnv holds the container handles, the wired services, and the base URL the
// tests drive.
type TestEnv struct {
	pool      *pgxpool.Pool
	eventSvc  *eventpkg.EventService
	bookSvc   *bookingpkg.BookingService
	fakePay   *fakePaymentClient
	fakeS3    *fakeS3Store
	infra     *testinfra.Infra
	apiURL    string
	srv       *httptest.Server
	cancel    context.CancelFunc
	infraPool *pgxpool.Pool
}

func TestMain(m *testing.M) {
	code := m.Run()
	if testEnv != nil {
		testEnv.cleanup()
	}
	os.Exit(code)
}

func startContainers() *TestEnv {
	infra, err := testinfra.Start(context.Background(), "ticketing")
	if err != nil {
		panic(err)
	}

	logger := log.New("ticketing", "warn")

	eventRepo := eventpostgres.NewEventRepo(infra.Pool)
	seatReader := eventpostgres.NewSeatReader(infra.Pool)
	seatCounter := eventpostgres.NewSeatCounter(infra.Pool)
	bookingRepo := eventpostgres.NewBookingRepo(infra.Pool)
	eventStatusRepo := eventpostgres.NewEventStatusRepo(infra.Pool)
	ticketingOutbox := outbox.NewStore(infra.Pool)
	consumer := eventkafka.NewTicketingConsumer(infra.Brokers, consumerGroup, 1)

	// The payment service is not running: the reserve path writes a
	// payment.initiate outbox row, and the REST dispatcher hands it to this
	// fake instead of the real internal API. The calls are recorded so tests
	// can assert the ticketing side handed off correctly.
	fakePay := &fakePaymentClient{}

	bookSvc := bookingpkg.NewBookingService(
		bookingRepo, seatCounter, consumer, eventStatusRepo,
		ticketingOutbox, logger, 3600,
	)
	// Event photos go to a fake S3 endpoint: pkg/s3 (minio-go, signing
	// included) is exercised for real, but nothing leaves the process.
	fakeS3 := newFakeS3()
	s3Client, err := s3.New(s3.Config{
		Endpoint:  fakeS3.endpoint,
		Secure:    false,
		Region:    "auto",
		AccessKey: "test",
		SecretKey: "test",
		Bucket:    "test-bucket",
		PublicURL: fakeS3.url + "/test-bucket",
	})
	if err != nil {
		panic("fake s3 client: " + err.Error())
	}
	eventSvc := eventpkg.NewEventService(eventRepo, seatReader, seatCounter, ticketingOutbox, s3Client)

	router := chi.NewRouter()
	ticketinghandler.NewEventHandler(eventSvc, s3Client).Routes(router)
	ticketinghandler.NewBookingHandler(bookSvc).Routes(router)
	srv := httptest.NewServer(router)

	ctxBg, cancelBg := context.WithCancel(context.Background())

	// Kafka-bound outbox rows only; payment.initiate goes through the REST
	// dispatcher, which is wired to the fake payment client here.
	go outbox.NewWorker(infra.Pool, infra.Producer, logger, 1, 200,
		[]string{"event.cancelled", "ticket.issued", "payout.requested"}).Run(ctxBg)
	go payment.NewDispatcher(infra.Pool, fakePay, logger, 100).Run(ctxBg)
	consumer.OnPayoutCompleted(ctxBg, func(ctx context.Context, evt sdomain.PayoutCompleted) error {
		return eventSvc.HandlePayoutCompleted(ctx, evt)
	})
	go func() { _ = bookSvc.StartConsumers(ctxBg) }()

	// Allow the outbox worker and Kafka writer to warm up; the first produce
	// to a topic can block for seconds on a fresh broker.
	time.Sleep(8 * time.Second)

	return &TestEnv{
		pool:      infra.Pool,
		eventSvc:  eventSvc,
		bookSvc:   bookSvc,
		fakePay:   fakePay,
		fakeS3:    fakeS3,
		infra:     infra,
		apiURL:    srv.URL,
		srv:       srv,
		cancel:    cancelBg,
		infraPool: infra.Pool,
	}
}

// fakeS3Store stands in for any S3-compatible store: it accepts every PUT and
// keeps the bytes in memory, so pkg/s3 (minio-go, SigV4 signing included) is
// exercised for real without touching Cloudflare.
type fakeS3Store struct {
	url      string
	endpoint string
	srv      *httptest.Server
	mu       sync.Mutex
	objects  map[string][]byte
}

func (f *fakeS3Store) get(key string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.objects[key]
}

func newFakeS3() *fakeS3Store {
	store := &fakeS3Store{objects: map[string][]byte{}}
	store.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// PutObject targets /{bucket}/{key}; any other call (e.g. bucket
		// location) is answered with an empty success so the client proceeds.
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusOK)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/")
		store.mu.Lock()
		store.objects[key] = body
		store.mu.Unlock()
		w.Header().Set("ETag", `"fake"`)
		w.WriteHeader(http.StatusOK)
	}))
	endpoint := strings.TrimPrefix(store.srv.URL, "http://")
	store.url = store.srv.URL
	store.endpoint = endpoint
	return store
}

func getTestEnv() *TestEnv {
	once.Do(func() { testEnv = startContainers() })
	return testEnv
}

func (env *TestEnv) cleanup() {
	env.cancel()
	env.srv.Close()
	env.infra.Close(context.Background())
}
