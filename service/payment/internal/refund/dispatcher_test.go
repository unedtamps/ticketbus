package refund

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	dto "github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
	"github.com/unedtamps/ticketbus/service/payment/internal/domain/mocks"
)

func newTestDispatcher(
	t *testing.T,
	refunds *mocks.MockRefundRepository,
	processor *mocks.MockPaymentProcessor,
) *Dispatcher {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewDispatcher(nil, refunds, processor, logger, 60, 0)
}

func refundJob(refundID string) dto.RefundDisbursementRequested {
	return dto.RefundDisbursementRequested{
		RefundID:          refundID,
		AmountRupiah:      150000,
		Currency:          "IDR",
		ChannelCode:       "BCA",
		AccountNumber:     "1234567890",
		AccountHolderName: "Cust QA",
		Description:       "TicketBus refund",
	}
}

func refundRow(job dto.RefundDisbursementRequested) disbursementRow {
	payload := []byte(`{
		"refund_id": "` + job.RefundID + `",
		"attempt": 0,
		"amount_rupiah": 150000,
		"currency": "IDR",
		"channel_code": "BCA",
		"account_number": "1234567890",
		"account_holder_name": "Cust QA",
		"description": "TicketBus refund"
	}`)
	return disbursementRow{ID: 1, Payload: payload}
}

// TestDeliver_Success covers a synchronously settled refund payout: the
// gateway reports SUCCEEDED and the refund lands as succeeded.
func TestDeliver_Success(t *testing.T) {
	ctx := context.Background()
	refunds := mocks.NewMockRefundRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	d := newTestDispatcher(t, refunds, processor)

	job := refundJob("rfd-test-1")
	refunds.EXPECT().Complete(ctx, "rfd-test-1", domain.RefundSucceeded, "dis-mock-rfd-test-1", "").
		Return(nil)
	processor.EXPECT().CreatePayout(mock.Anything, mock.MatchedBy(func(req domain.PayoutRequest) bool {
		return req.ReferenceID == "rfd-test-1" &&
			req.IdempotencyKey == "refund-rfd-test-1-0" &&
			req.AmountRupiah == 150000 &&
			req.BankCode == "BCA"
	})).Return(&domain.PayoutResult{ProviderRef: "dis-mock-rfd-test-1", Status: "SUCCEEDED"}, nil)

	require.NoError(t, d.deliver(context.Background(), refundRow(job)))
}

// TestDeliver_RetryUsesFreshIdempotencyKey is the retry guarantee: attempt 2
// must not replay attempt 0's gateway payout.
func TestDeliver_RetryUsesFreshIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	refunds := mocks.NewMockRefundRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	d := newTestDispatcher(t, refunds, processor)

	job := refundJob("rfd-retry")
	job.Attempt = 2
	row := disbursementRow{ID: 1, Payload: []byte(`{
		"refund_id": "rfd-retry",
		"attempt": 2,
		"amount_rupiah": 150000,
		"currency": "IDR",
		"channel_code": "BCA",
		"account_number": "1234567890",
		"account_holder_name": "Cust QA",
		"description": "TicketBus refund"
	}`)}

	refunds.EXPECT().Complete(ctx, "rfd-retry", domain.RefundSucceeded, "dis-mock-rfd-retry", "").
		Return(nil)
	processor.EXPECT().CreatePayout(mock.Anything, mock.MatchedBy(func(req domain.PayoutRequest) bool {
		return req.IdempotencyKey == "refund-rfd-retry-2"
	})).Return(&domain.PayoutResult{ProviderRef: "dis-mock-rfd-retry", Status: "SUCCEEDED"}, nil)

	require.NoError(t, d.deliver(context.Background(), row))
}

// TestDeliver_NonTerminal keeps the refund processing and stores the gateway
// reference, waiting for the webhook or the reconciliation poll.
func TestDeliver_NonTerminal(t *testing.T) {
	ctx := context.Background()
	refunds := mocks.NewMockRefundRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	d := newTestDispatcher(t, refunds, processor)

	job := refundJob("rfd-test-2")
	refunds.EXPECT().AttachProviderRef(ctx, "rfd-test-2", "dis-mock-rfd-test-2").Return(nil)
	processor.EXPECT().CreatePayout(mock.Anything, mock.Anything).
		Return(&domain.PayoutResult{ProviderRef: "dis-mock-rfd-test-2", Status: "PENDING"}, nil)

	require.NoError(t, d.deliver(context.Background(), refundRow(job)))
}

// TestDeliver_PermanentFailure records the failure instead of retrying: a
// gateway rejection of the destination cannot fix itself.
func TestDeliver_PermanentFailure(t *testing.T) {
	ctx := context.Background()
	refunds := mocks.NewMockRefundRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	d := newTestDispatcher(t, refunds, processor)

	job := refundJob("rfd-test-3")
	refunds.EXPECT().Complete(ctx, "rfd-test-3", domain.RefundFailed, "", mock.Anything).
		Return(nil)
	processor.EXPECT().CreatePayout(mock.Anything, mock.Anything).
		Return(nil, &domain.PermanentProcessorError{Err: errors.New("REJECTED_BY_CHANNEL")})

	require.NoError(t, d.deliver(context.Background(), refundRow(job)))
}

// TestSweep_ClaimsPending covers the claim: every pending refund with a
// registered destination moves to processing and gets its payout job enqueued.
func TestSweep_ClaimsPending(t *testing.T) {
	ctx := context.Background()
	refunds := mocks.NewMockRefundRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	d := newTestDispatcher(t, refunds, processor)

	refunds.EXPECT().ListPendingWithDestination(mock.Anything, 50).Return(
		[]domain.RefundSweep{{
			RefundID:          "rfd-sweep-1",
			AmountRupiah:      100000,
			Currency:          "IDR",
			ChannelCode:       "GOPAY",
			AccountNumber:     "081234567890",
			AccountHolderName: "Cust QA",
		}}, nil)
	refunds.EXPECT().ClaimForDisbursement(ctx, "rfd-sweep-1", mock.MatchedBy(func(j dto.RefundDisbursementRequested) bool {
		return j.RefundID == "rfd-sweep-1" && j.ChannelCode == "GOPAY"
	})).Return(true, nil)

	require.NoError(t, d.sweepPending(context.Background()))
}

// TestSweep_NothingPending confirms an empty claim queue is a no-op.
func TestSweep_NothingPending(t *testing.T) {
	refunds := mocks.NewMockRefundRepository(t)
	processor := mocks.NewMockPaymentProcessor(t)
	d := newTestDispatcher(t, refunds, processor)

	refunds.EXPECT().ListPendingWithDestination(mock.Anything, 50).Return(nil, nil)

	require.NoError(t, d.sweepPending(context.Background()))
}
