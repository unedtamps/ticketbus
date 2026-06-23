package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nedo/TicketSaas/internal/event/application"
	"github.com/nedo/TicketSaas/internal/event/domain"
	"github.com/nedo/TicketSaas/internal/event/domain/mocks"
	sdomain "github.com/nedo/TicketSaas/internal/shared/domain"
	"github.com/nedo/TicketSaas/internal/shared/outbox"
	"github.com/nedo/TicketSaas/tests/fixtures"
)

func newEventService(
	t testing.TB,
	repo domain.EventRepository,
	consumer domain.OrganizerConsumer,
	seatReader domain.SeatReader,
) *application.EventService {
	t.Helper()
	return application.NewEventService(repo, consumer, seatReader, outbox.NoopStore{})
}

func TestCreateOrganizer_Success(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()

	repo.EXPECT().FindOrganizerByUserID(ctx, "user-1").Return(nil, pgx.ErrNoRows)
	repo.EXPECT().CreateOrganizer(ctx, mock.AnythingOfType("*domain.Organizer")).Return(nil)

	svc := newEventService(t, repo, consumer, seatReader)
	org, err := svc.CreateOrganizer(ctx, "user-1", "Acme", "d", "url", "e")
	require.NoError(t, err)
	assert.Equal(t, "user-1", org.UserID)
}

func TestCreateOrganizer_Duplicate(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	existing := fixtures.NewTestOrganizer(fixtures.WithOrganizerUserID("user-1"))

	repo.EXPECT().FindOrganizerByUserID(ctx, "user-1").Return(existing, nil)
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.CreateOrganizer(ctx, "user-1", "X", "d", "u", "e")
	assert.ErrorIs(t, err, domain.ErrOrganizerExists)
}

func TestGetOrganizer_Found(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	expected := fixtures.NewTestOrganizer(fixtures.WithOrganizerUserID("user-1"))
	repo.EXPECT().FindOrganizerByUserID(ctx, "user-1").Return(expected, nil)
	svc := newEventService(t, repo, consumer, seatReader)
	org, err := svc.GetOrganizer(ctx, "user-1")
	require.NoError(t, err)
	assert.Equal(t, expected.ID, org.ID)
}

func TestGetOrganizer_NotFound(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	repo.EXPECT().FindOrganizerByUserID(ctx, "nobody").Return(nil, pgx.ErrNoRows)
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.GetOrganizer(ctx, "nobody")
	require.Error(t, err)
}

func TestCreateEvent_Success(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	start := time.Now().Add(24 * time.Hour)
	ticketTypes := []domain.TicketType{
		*fixtures.NewTestTicketType(fixtures.WithTicketTypeName("VIP"), fixtures.WithTicketTypeQuantity(10), fixtures.WithTicketTypePrice(10000)),
		*fixtures.NewTestTicketType(fixtures.WithTicketTypeName("GA"), fixtures.WithTicketTypeQuantity(40), fixtures.WithTicketTypePrice(5000)),
	}
	repo.EXPECT().CreateEvent(ctx, mock.AnythingOfType("*domain.Event")).Return(nil)
	repo.EXPECT().CreateTicketTypes(ctx, mock.Anything).Return(nil)
	svc := newEventService(t, repo, consumer, seatReader)
	event, err := svc.CreateEvent(ctx, "org-1", "Festival", "desc", "Venue", "Addr", 100, start, start.Add(3*time.Hour), ticketTypes)
	require.NoError(t, err)
	assert.Equal(t, sdomain.EventStatusPending, event.Status)
	assert.NotEmpty(t, event.ID)
}

func TestCreateEvent_EmptyTicketTypes(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	start := time.Now().Add(24 * time.Hour)
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.CreateEvent(ctx, "org-1", "E", "d", "V", "A", 100, start, start.Add(time.Hour), nil)
	assert.ErrorIs(t, err, domain.ErrTicketTypesEmpty)
}

func TestCreateEvent_CapacityExceeded(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	start := time.Now().Add(24 * time.Hour)
	types := []domain.TicketType{
		*fixtures.NewTestTicketType(fixtures.WithTicketTypeQuantity(60)),
		*fixtures.NewTestTicketType(fixtures.WithTicketTypeQuantity(50)),
	}
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.CreateEvent(ctx, "org-1", "E", "d", "V", "A", 100, start, start.Add(time.Hour), types)
	assert.ErrorIs(t, err, domain.ErrCapacityExceeded)
}

func TestCreateEvent_EventInsertFails(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	start := time.Now().Add(24 * time.Hour)
	types := []domain.TicketType{*fixtures.NewTestTicketType(fixtures.WithTicketTypeName("GA"), fixtures.WithTicketTypeQuantity(10))}
	repo.EXPECT().CreateEvent(ctx, mock.AnythingOfType("*domain.Event")).Return(errors.New("db error"))
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.CreateEvent(ctx, "org-1", "E", "d", "V", "A", 100, start, start.Add(time.Hour), types)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db error")
}

func TestCreateEvent_TicketTypesInsertFails(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	start := time.Now().Add(24 * time.Hour)
	types := []domain.TicketType{*fixtures.NewTestTicketType(fixtures.WithTicketTypeName("GA"), fixtures.WithTicketTypeQuantity(10))}
	repo.EXPECT().CreateEvent(ctx, mock.AnythingOfType("*domain.Event")).Return(nil)
	repo.EXPECT().CreateTicketTypes(ctx, mock.Anything).Return(errors.New("ticket insert error"))
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.CreateEvent(ctx, "org-1", "E", "d", "V", "A", 100, start, start.Add(time.Hour), types)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ticket insert error")
}

func TestUpdateEvent_Success_Draft(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	event := fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusDraft), fixtures.WithEventOrganizerID("org-1"))
	start := time.Now().Add(24 * time.Hour)
	repo.EXPECT().FindEventByID(ctx, event.ID).Return(event, nil)
	repo.EXPECT().UpdateEvent(ctx, mock.AnythingOfType("*domain.Event")).Return(nil)
	svc := newEventService(t, repo, consumer, seatReader)
	updated, err := svc.UpdateEvent(ctx, event.ID, "org-1", "New", "d", start, start.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, "New", updated.Title)
}

func TestUpdateEvent_Success_Pending(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	event := fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusPending), fixtures.WithEventOrganizerID("org-1"))
	start := time.Now().Add(24 * time.Hour)
	repo.EXPECT().FindEventByID(ctx, event.ID).Return(event, nil)
	repo.EXPECT().UpdateEvent(ctx, mock.AnythingOfType("*domain.Event")).Return(nil)
	svc := newEventService(t, repo, consumer, seatReader)
	updated, err := svc.UpdateEvent(ctx, event.ID, "org-1", "New", "d", start, start.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, "New", updated.Title)
}

func TestUpdateEvent_NotFound(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	start := time.Now().Add(24 * time.Hour)
	repo.EXPECT().FindEventByID(ctx, "bad-id").Return(nil, pgx.ErrNoRows)
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.UpdateEvent(ctx, "bad-id", "org-1", "T", "d", start, start.Add(time.Hour))
	assert.ErrorIs(t, err, domain.ErrEventNotFound)
}

func TestUpdateEvent_NotOwner(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	event := fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusDraft), fixtures.WithEventOrganizerID("org-original"))
	start := time.Now().Add(24 * time.Hour)
	repo.EXPECT().FindEventByID(ctx, event.ID).Return(event, nil)
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.UpdateEvent(ctx, event.ID, "org-imposter", "T", "d", start, start.Add(time.Hour))
	assert.ErrorIs(t, err, domain.ErrNotEventOwner)
}

func TestUpdateEvent_NotEditable_Published(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	event := fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusPublished), fixtures.WithEventOrganizerID("org-1"))
	start := time.Now().Add(24 * time.Hour)
	repo.EXPECT().FindEventByID(ctx, event.ID).Return(event, nil)
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.UpdateEvent(ctx, event.ID, "org-1", "T", "d", start, start.Add(time.Hour))
	assert.ErrorIs(t, err, domain.ErrEventNotEditable)
}

func TestUpdateEvent_UpdateFails(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	event := fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusDraft), fixtures.WithEventOrganizerID("org-1"))
	start := time.Now().Add(24 * time.Hour)
	repo.EXPECT().FindEventByID(ctx, event.ID).Return(event, nil)
	repo.EXPECT().UpdateEvent(ctx, mock.AnythingOfType("*domain.Event")).Return(errors.New("update failed"))
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.UpdateEvent(ctx, event.ID, "org-1", "T", "d", start, start.Add(time.Hour))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update failed")
}

func TestApproveEvent_Success(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	event := fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusPending))
	ticketTypes := []domain.TicketType{*fixtures.NewTestTicketType(fixtures.WithTicketTypeEventID(event.ID))}
	repo.EXPECT().FindEventByID(ctx, event.ID).Return(event, nil)
	repo.EXPECT().UpdateEvent(ctx, mock.AnythingOfType("*domain.Event")).Return(nil)
	repo.EXPECT().ListTicketTypesByEvent(ctx, event.ID).Return(ticketTypes, nil)
	svc := newEventService(t, repo, consumer, seatReader)
	approved, err := svc.ApproveEvent(ctx, event.ID, "admin-1")
	require.NoError(t, err)
	assert.Equal(t, sdomain.EventStatusPublished, approved.Status)
	assert.Equal(t, "admin-1", *approved.ReviewedBy)
}

func TestApproveEvent_NotFound(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	repo.EXPECT().FindEventByID(ctx, "bad-id").Return(nil, pgx.ErrNoRows)
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.ApproveEvent(ctx, "bad-id", "admin-1")
	assert.ErrorIs(t, err, domain.ErrEventNotFound)
}

func TestApproveEvent_NotPending(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	event := fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusPublished))
	repo.EXPECT().FindEventByID(ctx, event.ID).Return(event, nil)
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.ApproveEvent(ctx, event.ID, "admin-1")
	assert.ErrorIs(t, err, domain.ErrEventNotApprovable)
}

func TestApproveEvent_UpdateFails(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	event := fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusPending))
	repo.EXPECT().FindEventByID(ctx, event.ID).Return(event, nil)
	repo.EXPECT().UpdateEvent(ctx, mock.AnythingOfType("*domain.Event")).Return(errors.New("update failed"))
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.ApproveEvent(ctx, event.ID, "admin-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update failed")
}

func TestApproveEvent_TicketTypesLoadFails(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	event := fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusPending))
	repo.EXPECT().FindEventByID(ctx, event.ID).Return(event, nil)
	repo.EXPECT().UpdateEvent(ctx, mock.AnythingOfType("*domain.Event")).Return(nil)
	repo.EXPECT().ListTicketTypesByEvent(ctx, event.ID).Return(nil, errors.New("load error"))
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.ApproveEvent(ctx, event.ID, "admin-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load error")
}

func TestRejectEvent_Success(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	event := fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusPending))
	repo.EXPECT().FindEventByID(ctx, event.ID).Return(event, nil)
	repo.EXPECT().UpdateEvent(ctx, mock.AnythingOfType("*domain.Event")).Return(nil)
	svc := newEventService(t, repo, consumer, seatReader)
	rejected, err := svc.RejectEvent(ctx, event.ID, "admin-1", "reason")
	require.NoError(t, err)
	assert.Equal(t, sdomain.EventStatusRejected, rejected.Status)
}

func TestRejectEvent_NotFound(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	repo.EXPECT().FindEventByID(ctx, "bad-id").Return(nil, pgx.ErrNoRows)
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.RejectEvent(ctx, "bad-id", "admin-1", "r")
	assert.ErrorIs(t, err, domain.ErrEventNotFound)
}

func TestRejectEvent_NotPending(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	event := fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusPublished))
	repo.EXPECT().FindEventByID(ctx, event.ID).Return(event, nil)
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.RejectEvent(ctx, event.ID, "admin-1", "r")
	assert.ErrorIs(t, err, domain.ErrEventNotApprovable)
}

func TestCancelEvent_Success(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	event := fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusPublished), fixtures.WithEventOrganizerID("org-1"))
	repo.EXPECT().FindEventByID(ctx, event.ID).Return(event, nil)
	repo.EXPECT().UpdateEvent(ctx, mock.AnythingOfType("*domain.Event")).Return(nil)
	svc := newEventService(t, repo, consumer, seatReader)
	cancelled, err := svc.CancelEvent(ctx, event.ID, "org-1")
	require.NoError(t, err)
	assert.Equal(t, sdomain.EventStatusCancelled, cancelled.Status)
}

func TestCancelEvent_NotFound(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	repo.EXPECT().FindEventByID(ctx, "bad-id").Return(nil, pgx.ErrNoRows)
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.CancelEvent(ctx, "bad-id", "org-1")
	assert.ErrorIs(t, err, domain.ErrEventNotFound)
}

func TestCancelEvent_NotPublished(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	event := fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusDraft))
	repo.EXPECT().FindEventByID(ctx, event.ID).Return(event, nil)
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.CancelEvent(ctx, event.ID, "org-1")
	assert.ErrorIs(t, err, domain.ErrEventNotCancellable)
}

func TestCancelEvent_NotOwner(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	event := fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusPublished), fixtures.WithEventOrganizerID("org-original"))
	repo.EXPECT().FindEventByID(ctx, event.ID).Return(event, nil)
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.CancelEvent(ctx, event.ID, "org-imposter")
	assert.ErrorIs(t, err, domain.ErrNotEventOwner)
}

func TestCancelEvent_UpdateFails(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	event := fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusPublished), fixtures.WithEventOrganizerID("org-1"))
	repo.EXPECT().FindEventByID(ctx, event.ID).Return(event, nil)
	repo.EXPECT().UpdateEvent(ctx, mock.AnythingOfType("*domain.Event")).Return(errors.New("db error"))
	svc := newEventService(t, repo, consumer, seatReader)
	_, err := svc.CancelEvent(ctx, event.ID, "org-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db error")
}

func TestGetEvent_WithAvailability(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	event := fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusPublished))
	types := []domain.TicketType{
		*fixtures.NewTestTicketType(fixtures.WithTicketTypeEventID(event.ID), fixtures.WithTicketTypeID("tt-vip"), fixtures.WithTicketTypeQuantity(50)),
		*fixtures.NewTestTicketType(fixtures.WithTicketTypeEventID(event.ID), fixtures.WithTicketTypeID("tt-ga"), fixtures.WithTicketTypeQuantity(100)),
	}
	repo.EXPECT().FindEventByID(ctx, event.ID).Return(event, nil)
	repo.EXPECT().ListTicketTypesByEvent(ctx, event.ID).Return(types, nil)
	seatReader.EXPECT().Available(ctx, event.ID, "tt-vip").Return(42)
	seatReader.EXPECT().Available(ctx, event.ID, "tt-ga").Return(95)
	svc := newEventService(t, repo, consumer, seatReader)
	got, gotTypes, err := svc.GetEvent(ctx, event.ID)
	require.NoError(t, err)
	assert.Equal(t, event.ID, got.ID)
	assert.Equal(t, 42, gotTypes[0].Available)
	assert.Equal(t, 95, gotTypes[1].Available)
}

func TestGetEvent_NotFound(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	repo.EXPECT().FindEventByID(ctx, "bad-id").Return(nil, pgx.ErrNoRows)
	svc := newEventService(t, repo, consumer, seatReader)
	_, _, err := svc.GetEvent(ctx, "bad-id")
	assert.ErrorIs(t, err, domain.ErrEventNotFound)
}

func TestListPublished(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	expected := []domain.Event{*fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusPublished))}
	repo.EXPECT().ListPublished(ctx, 10, 0).Return(expected, 1, nil)
	svc := newEventService(t, repo, consumer, seatReader)
	events, total, err := svc.ListPublished(ctx, 10, 0)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, 1, total)
}

func TestListByOrganizer(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	expected := []domain.Event{*fixtures.NewTestEvent(fixtures.WithEventOrganizerID("org-1"))}
	repo.EXPECT().ListByOrganizer(ctx, "org-1").Return(expected, nil)
	svc := newEventService(t, repo, consumer, seatReader)
	events, err := svc.ListByOrganizer(ctx, "org-1")
	require.NoError(t, err)
	assert.Len(t, events, 1)
}

func TestListByOrganizer_Empty(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	repo.EXPECT().ListByOrganizer(ctx, "new-org").Return(nil, nil)
	svc := newEventService(t, repo, consumer, seatReader)
	events, err := svc.ListByOrganizer(ctx, "new-org")
	require.NoError(t, err)
	assert.Empty(t, events)
}

func TestListPending(t *testing.T) {
	repo := mocks.NewMockEventRepository(t)
	consumer := mocks.NewMockOrganizerConsumer(t)
	seatReader := mocks.NewMockSeatReader(t)
	ctx := context.Background()
	expected := []domain.Event{*fixtures.NewTestEvent(fixtures.WithEventStatus(sdomain.EventStatusPending))}
	repo.EXPECT().ListPending(ctx, 20, 40).Return(expected, 5, nil)
	svc := newEventService(t, repo, consumer, seatReader)
	events, total, err := svc.ListPending(ctx, 20, 40)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, 5, total)
}
