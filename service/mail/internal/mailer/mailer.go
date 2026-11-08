package mailer

import (
	"context"
)

// Mailer sends one rendered email.
type Mailer interface {
	// Send delivers an HTML (with a plain-text fallback) email to "to".
	Send(ctx context.Context, to, subject, htmlBody, textBody string) error
}
