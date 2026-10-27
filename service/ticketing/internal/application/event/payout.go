package event

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/domain"
)

// supportedBankCodes is the set of Indonesian banks Xendit supports for local
// IDR payouts. Stored without the ID_ channel prefix; the payment service maps
// it to the gateway channel code.
var supportedBankCodes = map[string]struct{}{
	"BCA":     {},
	"BNI":     {},
	"BRI":     {},
	"MANDIRI": {},
	"PERMATA": {},
	"CIMB":    {},
	"MAYBANK": {},
}

func normalizeBankCode(code string) (string, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if _, ok := supportedBankCodes[code]; !ok {
		return "", false
	}
	return code, true
}

func validAccountNumber(number string) bool {
	if len(number) < 5 || len(number) > 20 {
		return false
	}
	for _, r := range number {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// GetPayoutAccount returns the organizer's saved payout destination.
func (s *EventService) GetPayoutAccount(
	ctx context.Context,
	organizerID string,
) (*domain.OrganizerPayoutAccount, error) {
	acct, err := s.repo.FindPayoutAccount(ctx, organizerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPayoutAccountAbsent
	}
	if err != nil {
		return nil, err
	}
	return acct, nil
}

// SetPayoutAccount creates or updates the organizer's payout destination.
func (s *EventService) SetPayoutAccount(
	ctx context.Context,
	organizerID, bankCode, accountNumber, holderName string,
) (*domain.OrganizerPayoutAccount, error) {
	code, ok := normalizeBankCode(bankCode)
	number := strings.TrimSpace(accountNumber)
	holder := strings.TrimSpace(holderName)
	if !ok || !validAccountNumber(number) || holder == "" {
		return nil, domain.ErrInvalidPayoutBank
	}

	acct := &domain.OrganizerPayoutAccount{
		OrganizerID:           organizerID,
		BankCode:              code,
		BankAccountNumber:     number,
		BankAccountHolderName: holder,
	}
	if err := s.repo.UpsertPayoutAccount(ctx, acct); err != nil {
		return nil, err
	}
	return acct, nil
}

// RequestPayout validates the event eligibility and asks the repository to
// insert the next payout attempt and its outbox event atomically. The bank
// destination is snapshotted from the organizer's saved payout account.
//
// An event may have several attempts: a retry after a failure, or a top-up
// that collects transactions which completed after an earlier payout. Only one
// attempt may be in flight at a time.
func (s *EventService) RequestPayout(
	ctx context.Context,
	eventID, organizerID string,
) (*domain.EventPayoutAttempt, error) {
	event, err := s.repo.FindEventByID(ctx, eventID)
	if err != nil {
		return nil, domain.ErrEventNotFound
	}
	if event.OrganizerID != organizerID {
		return nil, domain.ErrNotEventOwner
	}
	if event.Status != sdomain.EventStatusPublished {
		return nil, domain.ErrEventNotPayoutable
	}
	if !time.Now().After(event.EndAt) {
		return nil, domain.ErrEventNotEnded
	}

	acct, err := s.repo.FindPayoutAccount(ctx, organizerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPayoutAccountAbsent
	}
	if err != nil {
		return nil, err
	}

	attempt := &domain.EventPayoutAttempt{
		ID:                    uuid.NewString(),
		EventID:               event.ID,
		OrganizerID:           organizerID,
		BankCode:              acct.BankCode,
		BankAccountNumber:     acct.BankAccountNumber,
		BankAccountHolderName: acct.BankAccountHolderName,
		Status:                sdomain.PayoutStatusPending,
		RequestedAt:           time.Now(),
	}

	// The attempt number is allocated by the repository inside the same
	// transaction as the insert, so the payload is built once it is known.
	applied, err := s.repo.RequestPayout(ctx, attempt, func(attemptNo int) any {
		return sdomain.PayoutRequested{
			EventID:               event.ID,
			OrganizerID:           organizerID,
			PayoutID:              attempt.ID,
			Attempt:               attemptNo,
			BankCode:              attempt.BankCode,
			BankAccountNumber:     attempt.BankAccountNumber,
			BankAccountHolderName: attempt.BankAccountHolderName,
			At:                    attempt.RequestedAt,
		}
	})
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, domain.ErrPayoutInFlight
	}
	return attempt, nil
}

// GetEventPayout returns the aggregated payout state for an event owned by
// the organizer, including every attempt.
func (s *EventService) GetEventPayout(
	ctx context.Context,
	eventID, organizerID string,
) (*domain.EventPayoutSummary, error) {
	event, err := s.repo.FindEventByID(ctx, eventID)
	if err != nil {
		return nil, domain.ErrEventNotFound
	}
	if event.OrganizerID != organizerID {
		return nil, domain.ErrNotEventOwner
	}
	return s.repo.PayoutSummary(ctx, eventID)
}

// HandlePayoutCompleted applies the gateway result to one payout attempt. It
// is idempotent: only a pending attempt is updated.
func (s *EventService) HandlePayoutCompleted(
	ctx context.Context,
	evt sdomain.PayoutCompleted,
) error {
	status := sdomain.PayoutStatus(evt.Status)
	if status != sdomain.PayoutStatusPaidOut && status != sdomain.PayoutStatusFailed {
		return fmt.Errorf("invalid payout status %q", evt.Status)
	}
	_, err := s.repo.SetPayoutResult(
		ctx, evt.EventID, evt.Attempt, status, evt.AmountRupiah, evt.ProviderRef, evt.Reason,
	)
	return err
}
