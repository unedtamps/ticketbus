//go:build integration

// Package integration is the payment service's integration suite. It boots
// Postgres and Kafka via pkg/testinfra, wires the payment handler against them
// in-process, and exercises the public HTTP API.
//
// The suite imports nothing from the other services. Where a booking would
// normally be created by the ticketing service, tests seed a transaction
// through the internal endpoint that ticketing itself calls, and where a
// cancelled event would normally arrive from ticketing, tests publish
// event.cancelled straight to Kafka.
//
// Running the suite:
//
//	go test -tags=integration ./service/payment/tests/integration/
//
// Per-area filtering — test names follow Test<Area>_<Scenario>[_<Variant>]
// with areas: Payment, Webhook, Mock, Expiry, Refund:
//
//	go test -tags=integration -run 'TestPayment|TestWebhook' ./service/payment/tests/integration/
package integration

import (
	"context"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nedo/TicketSaas/pkg/log"
	"github.com/nedo/TicketSaas/pkg/outbox"
	"github.com/nedo/TicketSaas/pkg/testinfra"
	payapp "github.com/nedo/TicketSaas/service/payment/internal/application"
	payhandler "github.com/nedo/TicketSaas/service/payment/internal/handler"
	paykafka "github.com/nedo/TicketSaas/service/payment/internal/kafka"
	paypostgres "github.com/nedo/TicketSaas/service/payment/internal/postgres"
	"github.com/nedo/TicketSaas/service/payment/internal/processor"
)

const (
	internalKey   = "test-internal-key"
	webhookToken  = "test-webhook-token"
	consumerGroup = "payment-service-test"

	// defaultAmount is the price of the single ticket type the transaction
	// tests pretend to have reserved.
	defaultAmount = 10000
)

var (
	testEnv *TestEnv
	once    sync.Once
)

// TestEnv holds the container handles, the wired payment service, and the
// base URL the tests drive.
type TestEnv struct {
	payPool *pgxpool.Pool
	svc     *payapp.PaymentService
	mock    *processor.MockProcessor
	payURL  string
	srv     *httptest.Server
	infra   *testinfra.Infra
	cancel  context.CancelFunc
}

func TestMain(m *testing.M) {
	code := m.Run()
	if testEnv != nil {
		testEnv.cleanup()
	}
	os.Exit(code)
}

func startContainers() *TestEnv {
	infra, err := testinfra.Start(context.Background(), "payment")
	if err != nil {
		panic(err)
	}

	logger := log.New("payment", "warn")

	txnRepo := paypostgres.NewTransactionRepo(infra.Pool)
	refundRepo := paypostgres.NewRefundRepo(infra.Pool)
	mockProcessor := processor.NewMockProcessor()
	payConsumer := paykafka.NewPaymentConsumer(infra.Brokers, consumerGroup, 1)
	payOutbox := outbox.NewStore(infra.Pool)

	svc := payapp.NewPaymentService(
		txnRepo, refundRepo, mockProcessor, payConsumer, payOutbox,
		logger, "mock", 5, []string{"ID_QRIS", "ID_BCA_VA"},
	)

	handler := payhandler.NewPaymentHandler(svc, internalKey, webhookToken)
	srv := httptest.NewServer(handler.Routes())

	ctxBg, cancelBg := context.WithCancel(context.Background())

	go outbox.NewWorker(infra.Pool, infra.Producer, logger, 1, 200).Run(ctxBg)
	go func() { _ = svc.StartConsumer(ctxBg) }()
	svc.StartExpiryPoller(ctxBg, 1)

	// Allow the outbox worker and Kafka writer to warm up; the first produce
	// to a topic can block for seconds on a fresh broker.
	time.Sleep(8 * time.Second)

	return &TestEnv{
		payPool: infra.Pool,
		svc:     svc,
		mock:    mockProcessor,
		payURL:  srv.URL,
		srv:     srv,
		infra:   infra,
		cancel:  cancelBg,
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
