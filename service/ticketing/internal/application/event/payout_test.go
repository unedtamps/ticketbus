package event_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/domain"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/domain/mocks"
)

func endedEvent() *domain.Event {
	return &domain.Event{
		ID:          "evt-1",
		OrganizerID: "org-1",
		Status:      sdomain.EventStatusPublished,
		EndAt:       time.Now().Add(-time.Hour),
	}
}

func payoutAccount() *domain.OrganizerPayoutAccount {
	return &domain.OrganizerPayoutAccount{
		OrganizerID:           "org-1",
		BankCode:              "BCA",
		BankAccountNumber:     "1234567890",
		BankAccountHolderName: "Budi",
	}
}

func TestRequestPayout_Success(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()

	repo.On("FindEventByID", ctx, "evt-1").Return(endedEvent(), nil)
	repo.On("FindPayoutAccount", ctx, "org-1").Return(payoutAccount(), nil)
	var captured *domain.EventPayoutAttempt
	repo.On("RequestPayout", ctx, mock.AnythingOfType("*domain.EventPayoutAttempt"),
		mock.AnythingOfType("func(int) interface {}")).
		Run(func(args mock.Arguments) {
			captured = args.Get(1).(*domain.EventPayoutAttempt)
			// The repository allocates the attempt number; the payload builder
			// receives it.
			payload := args.Get(2).(func(int) any)(3)
			req, ok := payload.(sdomain.PayoutRequested)
			require.True(t, ok)
			assert.Equal(t, 3, req.Attempt)
			assert.Equal(t, "evt-1", req.EventID)
			assert.Equal(t, "BCA", req.BankCode)
			assert.Equal(t, "1234567890", req.BankAccountNumber)
		}).
		Return(true, nil)

	svc := newEventService(t, repo, seatReader)
	payout, err := svc.RequestPayout(ctx, "evt-1", "org-1")
	require.NoError(t, err)

	assert.Equal(t, sdomain.PayoutStatusPending, payout.Status)
	assert.Equal(t, "BCA", payout.BankCode)
	assert.Equal(t, "1234567890", payout.BankAccountNumber)
	assert.Equal(t, "Budi", payout.BankAccountHolderName)
	assert.NotEmpty(t, payout.ID)
	require.NotNil(t, captured)
	assert.Equal(t, payout.ID, captured.ID)
}

func TestRequestPayout_NotOwner(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	repo.On("FindEventByID", ctx, "evt-1").Return(endedEvent(), nil)

	svc := newEventService(t, repo, seatReader)
	_, err := svc.RequestPayout(ctx, "evt-1", "someone-else")
	assert.ErrorIs(t, err, domain.ErrNotEventOwner)
}

func TestRequestPayout_NotEnded(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	ev := endedEvent()
	ev.EndAt = time.Now().Add(time.Hour)
	repo.On("FindEventByID", ctx, "evt-1").Return(ev, nil)

	svc := newEventService(t, repo, seatReader)
	_, err := svc.RequestPayout(ctx, "evt-1", "org-1")
	assert.ErrorIs(t, err, domain.ErrEventNotEnded)
}

func TestRequestPayout_NotPublished(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	ev := endedEvent()
	ev.Status = sdomain.EventStatusCancelled
	repo.On("FindEventByID", ctx, "evt-1").Return(ev, nil)

	svc := newEventService(t, repo, seatReader)
	_, err := svc.RequestPayout(ctx, "evt-1", "org-1")
	assert.ErrorIs(t, err, domain.ErrEventNotPayoutable)
}

func TestRequestPayout_NoAccount(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	repo.On("FindEventByID", ctx, "evt-1").Return(endedEvent(), nil)
	repo.On("FindPayoutAccount", ctx, "org-1").Return(nil, pgx.ErrNoRows)

	svc := newEventService(t, repo, seatReader)
	_, err := svc.RequestPayout(ctx, "evt-1", "org-1")
	assert.ErrorIs(t, err, domain.ErrPayoutAccountAbsent)
}

func TestRequestPayout_AlreadyInFlight(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	repo.On("FindEventByID", ctx, "evt-1").Return(endedEvent(), nil)
	repo.On("FindPayoutAccount", ctx, "org-1").Return(payoutAccount(), nil)
	repo.On("RequestPayout", ctx, mock.Anything, mock.Anything).Return(false, nil)

	svc := newEventService(t, repo, seatReader)
	_, err := svc.RequestPayout(ctx, "evt-1", "org-1")
	assert.ErrorIs(t, err, domain.ErrPayoutInFlight)
}

func TestSetPayoutAccount_Validates(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	svc := newEventService(t, repo, seatReader)

	cases := []struct {
		name, bank, number, holder string
	}{
		{"unknown bank", "XXX", "1234567890", "Budi"},
		{"short number", "BCA", "123", "Budi"},
		{"non numeric", "BCA", "12345abc", "Budi"},
		{"empty holder", "BCA", "1234567890", " "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.SetPayoutAccount(ctx, "org-1", tc.bank, tc.number, tc.holder)
			assert.ErrorIs(t, err, domain.ErrInvalidPayoutBank)
		})
	}
}

func TestSetPayoutAccount_NormalizesBank(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	repo.On("UpsertPayoutAccount", ctx, mock.MatchedBy(func(a *domain.OrganizerPayoutAccount) bool {
		return a.OrganizerID == "org-1" && a.BankCode == "BCA" &&
			a.BankAccountNumber == "1234567890" && a.BankAccountHolderName == "Budi"
	})).Return(nil)

	svc := newEventService(t, repo, seatReader)
	acct, err := svc.SetPayoutAccount(ctx, "org-1", " bca ", "1234567890", " Budi ")
	require.NoError(t, err)
	assert.Equal(t, "BCA", acct.BankCode)
}

func TestHandlePayoutCompleted_Applied(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	repo.On("SetPayoutResult", ctx, "evt-1", 2,
		sdomain.PayoutStatusPaidOut, int64(25000), "dis-1", "").Return(true, nil)

	svc := newEventService(t, repo, seatReader)
	err := svc.HandlePayoutCompleted(ctx, sdomain.PayoutCompleted{
		EventID: "evt-1", Attempt: 2, Status: "paid_out",
		AmountRupiah: 25000, ProviderRef: "dis-1",
	})
	assert.NoError(t, err)
}

func TestHandlePayoutCompleted_RejectsUnknownStatus(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	seatReader := mocks.NewMockSeatReader(t)
	svc := newEventService(t, repo, seatReader)

	err := svc.HandlePayoutCompleted(context.Background(), sdomain.PayoutCompleted{
		EventID: "evt-1", Attempt: 1, Status: "processing",
	})
	assert.Error(t, err)
}
