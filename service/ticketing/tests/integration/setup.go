//go:build integration

// Package integration is the ticketing service's integration suite. It boots
// Postgres and Kafka via pkg/testinfra, wires the event and booking handlers
// against them in-process, and exercises the public HTTP API.
//
// The suite imports nothing from the other services. Caller identity comes from
// synthesized X-Authenticated-* headers, which WithUserContext trusts without
// signature verification; the payment service is replaced by a fake
// domain.PaymentClient; and upstream payment events are published straight to
// Kafka, standing in for the payment service.
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
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nedo/TicketSaas/pkg/log"
	"github.com/nedo/TicketSaas/pkg/outbox"
	"github.com/nedo/TicketSaas/pkg/testinfra"
	bookingpkg "github.com/nedo/TicketSaas/service/ticketing/internal/application/booking"
	eventpkg "github.com/nedo/TicketSaas/service/ticketing/internal/application/event"
	ticketinghandler "github.com/nedo/TicketSaas/service/ticketing/internal/handler"
	eventkafka "github.com/nedo/TicketSaas/service/ticketing/internal/kafka"
	eventpostgres "github.com/nedo/TicketSaas/service/ticketing/internal/postgres"
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

	// The payment service is not running: the reserve path only needs a
	// transaction to be created, and a fake records the calls instead.
	fakePay := &fakePaymentClient{}

	bookSvc := bookingpkg.NewBookingService(
		bookingRepo, fakePay, seatCounter, consumer, eventStatusRepo,
		ticketingOutbox, logger, 3600,
	)
	eventSvc := eventpkg.NewEventService(eventRepo, seatReader, seatCounter, ticketingOutbox)

	router := chi.NewRouter()
	ticketinghandler.NewEventHandler(eventSvc).Routes(router)
	ticketinghandler.NewBookingHandler(bookSvc).Routes(router)
	srv := httptest.NewServer(router)

	ctxBg, cancelBg := context.WithCancel(context.Background())

	go outbox.NewWorker(infra.Pool, infra.Producer, logger, 1, 200).Run(ctxBg)
	go func() { _ = bookSvc.StartConsumers(ctxBg) }()

	// Allow the outbox worker and Kafka writer to warm up; the first produce
	// to a topic can block for seconds on a fresh broker.
	time.Sleep(8 * time.Second)

	return &TestEnv{
		pool:      infra.Pool,
		eventSvc:  eventSvc,
		bookSvc:   bookSvc,
		fakePay:   fakePay,
		infra:     infra,
		apiURL:    srv.URL,
		srv:       srv,
		cancel:    cancelBg,
		infraPool: infra.Pool,
	}
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
