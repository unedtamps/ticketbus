package event_test

// event_photo_test.go — the UploadEventPhoto business rules: who may upload,
// which event states accept a photo, and what makes an image valid.

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/pkg/outbox"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/application/event"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/domain"
	"github.com/unedtamps/ticketbus/service/ticketing/internal/domain/mocks"
)

type photoHarness struct {
	svc    *event.EventService
	repo   *mocks.MockEventRepository
	images *mocks.MockImageStorage
}

func newPhotoHarness(t *testing.T) *photoHarness {
	t.Helper()
	repo := mocks.NewMockEventRepository(t)
	images := mocks.NewMockImageStorage(t)
	svc := event.NewEventService(repo, mocks.NewMockSeatReader(t), nil, outbox.NoopStore{}, images)
	return &photoHarness{svc: svc, repo: repo, images: images}
}

func TestUploadEventPhoto_Success(t *testing.T) {
	h := newPhotoHarness(t)
	ctx := context.Background()
	ev := &domain.Event{ID: "evt-1", OrganizerID: "org-1", Status: sdomain.EventStatusPublished}

	h.repo.EXPECT().FindEventByID(ctx, "evt-1").Return(ev, nil)
	var uploadedKey, storedKey string
	h.images.EXPECT().Upload(
		ctx,
		mock.MatchedBy(func(key string) bool {
			return strings.HasPrefix(key, "events/evt-1/") && strings.HasSuffix(key, ".png")
		}),
		mock.Anything, int64(4), "image/png",
	).Run(func(_ context.Context, key string, _ io.Reader, _ int64, _ string) {
		uploadedKey = key
	}).Return(nil)
	h.repo.EXPECT().SetEventImage(ctx, "evt-1", mock.Anything).Run(
		func(_ context.Context, _ string, key string) { storedKey = key },
	).Return(nil)
	h.images.EXPECT().PublicURL(mock.Anything).RunAndReturn(
		func(key string) string { return "https://pub-xxx.r2.dev/" + key },
	)

	url, err := h.svc.UploadEventPhoto(
		ctx, "evt-1", "org-1", "image/png", strings.NewReader("data"), 4,
	)
	require.NoError(t, err)
	assert.NotEmpty(t, uploadedKey)
	assert.Equal(t, uploadedKey, storedKey, "the stored key must be the uploaded key")
	assert.Equal(t, "https://pub-xxx.r2.dev/"+uploadedKey, url)
}

// Photos are cosmetic: a published event accepts one even though its fields
// are frozen.
func TestUploadEventPhoto_PublishedAllowed(t *testing.T) {
	h := newPhotoHarness(t)
	ctx := context.Background()
	ev := &domain.Event{ID: "evt-1", OrganizerID: "org-1", Status: sdomain.EventStatusPublished}

	h.repo.EXPECT().FindEventByID(ctx, "evt-1").Return(ev, nil)
	h.images.EXPECT().Upload(ctx, mock.Anything, mock.Anything, int64(4), "image/jpeg").Return(nil)
	h.repo.EXPECT().SetEventImage(ctx, "evt-1", mock.Anything).Return(nil)
	h.images.EXPECT().PublicURL(mock.Anything).Return("https://pub-xxx.r2.dev/key")

	_, err := h.svc.UploadEventPhoto(
		ctx, "evt-1", "org-1", "image/jpeg", strings.NewReader("data"), 4,
	)
	require.NoError(t, err)
}

func TestUploadEventPhoto_CancelledRejected(t *testing.T) {
	h := newPhotoHarness(t)
	ctx := context.Background()
	ev := &domain.Event{ID: "evt-1", OrganizerID: "org-1", Status: sdomain.EventStatusCancelled}

	h.repo.EXPECT().FindEventByID(ctx, "evt-1").Return(ev, nil)

	_, err := h.svc.UploadEventPhoto(
		ctx, "evt-1", "org-1", "image/png", strings.NewReader("data"), 4,
	)
	assert.ErrorIs(t, err, domain.ErrEventNotEditable)
}

func TestUploadEventPhoto_ForeignOwnerRejected(t *testing.T) {
	h := newPhotoHarness(t)
	ctx := context.Background()
	ev := &domain.Event{ID: "evt-1", OrganizerID: "org-1", Status: sdomain.EventStatusDraft}

	h.repo.EXPECT().FindEventByID(ctx, "evt-1").Return(ev, nil)

	_, err := h.svc.UploadEventPhoto(
		ctx, "evt-1", "org-2", "image/png", strings.NewReader("data"), 4,
	)
	assert.ErrorIs(t, err, domain.ErrNotEventOwner)
}

func TestUploadEventPhoto_MissingEvent(t *testing.T) {
	h := newPhotoHarness(t)
	ctx := context.Background()

	h.repo.EXPECT().FindEventByID(ctx, "evt-404").Return(nil, errors.New("no rows"))

	_, err := h.svc.UploadEventPhoto(
		ctx, "evt-404", "org-1", "image/png", strings.NewReader("data"), 4,
	)
	assert.ErrorIs(t, err, domain.ErrEventNotFound)
}

func TestUploadEventPhoto_UnsupportedType(t *testing.T) {
	h := newPhotoHarness(t)
	ctx := context.Background()

	// Rejected before any lookup: nothing is expected on the mocks.
	_, err := h.svc.UploadEventPhoto(
		ctx, "evt-1", "org-1", "image/gif", strings.NewReader("data"), 4,
	)
	assert.ErrorIs(t, err, domain.ErrImageUnsupported)
}

func TestUploadEventPhoto_TooLarge(t *testing.T) {
	h := newPhotoHarness(t)
	ctx := context.Background()

	_, err := h.svc.UploadEventPhoto(
		ctx, "evt-1", "org-1", "image/png",
		strings.NewReader("data"), int64(event.MaxImageBytes)+1,
	)
	assert.ErrorIs(t, err, domain.ErrImageTooLarge)
}
