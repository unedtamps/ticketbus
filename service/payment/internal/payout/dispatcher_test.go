package payout

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
)

type completeCall struct {
	eventID     string
	attempt     int
	status      string
	amount      int64
	providerRef string
	reason      string
}

type fakeRepo struct {
	completed []completeCall
	err       error
}

func (f *fakeRepo) Prepare(context.Context, dto.PayoutRequested) (*domain.PreparedPayout, error) {
	return nil, nil
}

func (f *fakeRepo) Complete(_ context.Context, eventID string, attempt int, status string, amount int64, providerRef, reason string) error {
	f.completed = append(f.completed, completeCall{eventID, attempt, status, amount, providerRef, reason})
	return f.err
}

func (f *fakeRepo) FindByEvent(context.Context, string, int) (*domain.Payout, error) {
	return nil, nil
}

type fakeProcessor struct {
	result *domain.PayoutResult
	err    error
	last   domain.PayoutRequest
	calls  int
}

func (f *fakeProcessor) CreateSession(context.Context, string, int, string, time.Time, []string, string) (*domain.SessionResult, error) {
	return nil, nil
}

func (f *fakeProcessor) CancelSession(context.Context, string) error { return nil }

func (f *fakeProcessor) GetSession(context.Context, string) (*domain.SessionResult, error) {
	return nil, nil
}

func (f *fakeProcessor) CreatePayout(_ context.Context, req domain.PayoutRequest) (*domain.PayoutResult, error) {
	f.calls++
	f.last = req
	return f.result, f.err
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func jobRow(t *testing.T, job dto.PaymentDisbursementRequested) disbursementRow {
	t.Helper()
	payload, err := json.Marshal(job)
	require.NoError(t, err)
	return disbursementRow{ID: 1, Payload: payload}
}

func TestDispatcher_DeliverPaidOut(t *testing.T) {
	repo := &fakeRepo{}
	proc := &fakeProcessor{result: &domain.PayoutResult{ProviderRef: "po-1", Status: "ACCEPTED"}}
	d := NewDispatcher(nil, repo, proc, discardLogger(), 10)

	err := d.deliver(context.Background(), jobRow(t, dto.PaymentDisbursementRequested{
		PayoutID:              "payout-1",
		EventID:               "event-1",
		Attempt:               2,
		AmountRupiah:          15000,
		Currency:              "IDR",
		BankCode:              "BCA",
		BankAccountNumber:     "1234567890",
		BankAccountHolderName: "Budi",
		Description:           "Event payout",
	}))
	require.NoError(t, err)

	assert.Equal(t, "payout-1", proc.last.ReferenceID)
	assert.Equal(t, "payout-payout-1-2", proc.last.IdempotencyKey)
	assert.EqualValues(t, 15000, proc.last.AmountRupiah)
	assert.Equal(t, "BCA", proc.last.BankCode)
	assert.Equal(t, "1234567890", proc.last.AccountNumber)

	require.Len(t, repo.completed, 1)
	assert.Equal(t, "event-1", repo.completed[0].eventID)
	assert.Equal(t, 2, repo.completed[0].attempt)
	assert.Equal(t, domain.PayoutStatusPaidOut, repo.completed[0].status)
	assert.EqualValues(t, 15000, repo.completed[0].amount)
	assert.Equal(t, "po-1", repo.completed[0].providerRef)
}

func TestDispatcher_PermanentErrorMarksFailed(t *testing.T) {
	repo := &fakeRepo{}
	proc := &fakeProcessor{err: &domain.PermanentProcessorError{Err: errors.New("invalid account")}}
	d := NewDispatcher(nil, repo, proc, discardLogger(), 10)

	err := d.deliver(context.Background(), jobRow(t, dto.PaymentDisbursementRequested{
		PayoutID: "payout-1", EventID: "event-1", Attempt: 1,
		AmountRupiah: 10000, Currency: "IDR", BankCode: "BCA",
	}))
	require.NoError(t, err, "a permanent rejection is recorded, not retried")

	require.Len(t, repo.completed, 1)
	assert.Equal(t, domain.PayoutStatusFailed, repo.completed[0].status)
	assert.Contains(t, repo.completed[0].reason, "invalid account")
}

func TestDispatcher_AuthErrorIsRetried(t *testing.T) {
	repo := &fakeRepo{}
	proc := &fakeProcessor{err: &domain.AuthProcessorError{Err: errors.New("403 forbidden")}}
	d := NewDispatcher(nil, repo, proc, discardLogger(), 10)

	err := d.deliver(context.Background(), jobRow(t, dto.PaymentDisbursementRequested{
		PayoutID: "payout-1", EventID: "event-1", Attempt: 1,
		AmountRupiah: 10000, Currency: "IDR", BankCode: "BCA",
	}))
	require.Error(t, err, "a credentials problem must not fail the payout")
	assert.Empty(t, repo.completed)
}

func TestDispatcher_RetryableErrorLeavesJobUndelivered(t *testing.T) {
	repo := &fakeRepo{}
	proc := &fakeProcessor{err: errors.New("gateway timeout")}
	d := NewDispatcher(nil, repo, proc, discardLogger(), 10)

	err := d.deliver(context.Background(), jobRow(t, dto.PaymentDisbursementRequested{
		PayoutID: "payout-1", EventID: "event-1", Attempt: 1,
		AmountRupiah: 10000, Currency: "IDR", BankCode: "BCA",
	}))
	require.Error(t, err)
	assert.Empty(t, repo.completed)
}
