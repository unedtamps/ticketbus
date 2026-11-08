package application

import (
	"bytes"
	"context"
	"embed"
	"html/template"
	"log/slog"
	"strconv"
	"time"

	sdomain "github.com/unedtamps/ticketbus/pkg/dto"
	"github.com/unedtamps/ticketbus/service/mail/internal/kafka"
	"github.com/unedtamps/ticketbus/service/mail/internal/mailer"
)

//go:embed templates/*.html
var templateFS embed.FS

// MailService consumes ticket and event lifecycle events and emails buyers
// accordingly: an e-ticket per issued ticket, and a cancellation notice per
// affected booking. Delivery is at-most-once like every consumer in this
// system: a failed send is logged and the offset moves on.
type MailService struct {
	consumer *kafka.MailConsumer
	mailer   mailer.Mailer
	logger   *slog.Logger
	tmpl     *template.Template
}

// NewMailService wires the consumer to the mailer.
func NewMailService(
	consumer *kafka.MailConsumer,
	mailer mailer.Mailer,
	logger *slog.Logger,
) (*MailService, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &MailService{
		consumer: consumer,
		mailer:   mailer,
		logger:   logger,
		tmpl:     tmpl,
	}, nil
}

// StartConsumer registers the event handlers and starts consuming. It blocks
// until ctx is cancelled.
func (s *MailService) StartConsumer(ctx context.Context) {
	s.consumer.OnTicketIssued(ctx, s.onTicketIssued)
	s.consumer.OnEventCancelled(ctx, s.onEventCancelled)
	_ = s.consumer.Start(ctx)
}

// onTicketIssued renders and delivers one e-ticket email per ticket item.
func (s *MailService) onTicketIssued(ctx context.Context, event sdomain.TicketIssued) error {
	if event.Email == "" {
		// Bookings created before the email snapshot existed carry no address;
		// there is nowhere to deliver to, and the offset must move on.
		s.logger.Warn("ticket issued without email, skipping",
			"booking_id", event.BookingID, "ticket_id", event.TicketID)
		return nil
	}

	subject := "E-Tiket " + event.EventTitle + " — " + event.TicketTypeName
	d := ticketEmailData{
		EventTitle:     event.EventTitle,
		EventVenue:     event.EventVenue,
		EventStartAt:   formatWhen(event.EventStartAt),
		TicketTypeName: event.TicketTypeName,
		Quantity:       event.Quantity,
		BookingID:      event.BookingID,
		TicketID:       event.TicketID,
	}
	return s.deliver(event.Email, subject, "ticket_email.html", d,
		plainTicketText(d), "e-ticket emailed",
		slog.String("booking_id", event.BookingID),
		slog.String("ticket_id", event.TicketID),
	)
}

// onEventCancelled delivers one cancellation notice per affected booking.
func (s *MailService) onEventCancelled(ctx context.Context, event sdomain.EventCancelled) error {
	if len(event.Bookers) == 0 {
		// No snapshotted bookers (every booking predates the email column, or
		// the event had no bookings) — nothing to deliver, offset moves on.
		s.logger.Info("event cancelled without bookers, skipping",
			"event_id", event.EventID)
		return nil
	}

	subject := "Event Dibatalkan — " + event.EventTitle
	sent := 0
	for _, booker := range event.Bookers {
		if booker.Email == "" {
			continue
		}
		d := cancelledEmailData{
			EventTitle:   event.EventTitle,
			EventVenue:   event.EventVenue,
			EventStartAt: formatWhen(event.EventStartAt),
			BookingID:    booker.BookingID,
		}
		if err := s.deliver(booker.Email, subject, "event_cancelled_email.html", d,
			plainCancelledText(d), "cancellation emailed",
			slog.String("event_id", event.EventID),
			slog.String("booking_id", booker.BookingID),
		); err != nil {
			return err
		}
		sent++
	}
	s.logger.Info("cancellation notices sent",
		"event_id", event.EventID, "bookers", len(event.Bookers), "sent", sent)
	return nil
}

// deliver renders the named template, sends the mail, and logs the outcome
// with the caller's context fields. The error is returned so the consumer's
// at-most-once logging sees it; the offset moves on either way.
func (s *MailService) deliver(
	to, subject, templateName string,
	data any,
	textBody string,
	logMsg string,
	logFields ...any,
) error {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, templateName, data); err != nil {
		return err
	}
	if err := s.mailer.Send(context.Background(), to, subject, buf.String(), textBody); err != nil {
		return err
	}
	s.logger.Info(logMsg, append(logFields, "to", to)...)
	return nil
}

// formatWhen renders an event timestamp in the server's zone. It is
// pre-formatted because Go's locale default renders English month names.
func formatWhen(t time.Time) string {
	return t.Format("Monday, 2 January 2006 · 15:04")
}

// ticketEmailData is the e-ticket template view.
type ticketEmailData struct {
	EventTitle     string
	EventVenue     string
	EventStartAt   string
	TicketTypeName string
	Quantity       int
	BookingID      string
	TicketID       string
}

// cancelledEmailData is the cancellation template view.
type cancelledEmailData struct {
	EventTitle   string
	EventVenue   string
	EventStartAt string
	BookingID    string
}

// plainTicketText builds the plain-text e-ticket fallback.
func plainTicketText(d ticketEmailData) string {
	var b bytes.Buffer
	b.WriteString("Pembayaran berhasil — e-tiket Anda\n\n")
	b.WriteString("Event: " + d.EventTitle + "\n")
	b.WriteString("Waktu: " + d.EventStartAt + "\n")
	b.WriteString("Lokasi: " + d.EventVenue + "\n")
	b.WriteString("Tiket: " + d.TicketTypeName + " x" + strconv.Itoa(d.Quantity) + "\n")
	b.WriteString("Booking ID: " + d.BookingID + "\n")
	b.WriteString("Ticket ID: " + d.TicketID + "\n\n")
	b.WriteString("Simpan email ini dan tunjukkan saat masuk venue.")
	return b.String()
}

// plainCancelledText builds the plain-text cancellation fallback.
func plainCancelledText(d cancelledEmailData) string {
	var b bytes.Buffer
	b.WriteString("Mohon maaf — event dibatalkan\n\n")
	b.WriteString("Event: " + d.EventTitle + "\n")
	b.WriteString("Waktu semula: " + d.EventStartAt + "\n")
	b.WriteString("Lokasi: " + d.EventVenue + "\n")
	b.WriteString("Booking ID: " + d.BookingID + "\n\n")
	b.WriteString("Jika kamu sudah melakukan pembayaran, danamu akan dikembalikan ke metode pembayaran yang dipakai.")
	return b.String()
}
