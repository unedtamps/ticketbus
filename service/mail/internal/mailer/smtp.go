// Package mailer delivers emails over SMTP. MailDev in dev accepts any
// sender and no credentials, so the client stays dependency-free: net/smtp
// plus a hand-built MIME multipart (HTML + plain-text fallback).
package mailer

import (
	"bytes"
	"context"
	"fmt"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
)

// SMTP sends mail through a plain (no-TLS) SMTP server.
type SMTP struct {
	host string
	port string
	from string
}

// NewSMTP creates an SMTP mailer. "from" is the full RFC 5322 sender,
// e.g. "TicketBus <no-reply@ticketbus.local>".
func NewSMTP(host, port, from string) *SMTP {
	return &SMTP{host: host, port: port, from: from}
}

// Send delivers the message as a multipart/alternative email.
func (s *SMTP) Send(_ context.Context, to, subject, htmlBody, textBody string) error {
	addr := s.host + ":" + s.port

	// The envelope MAIL FROM must be the bare address; the display name is
	// header-only. SMTP servers reject "Name <addr>" as a sender (501).
	fromAddr := s.from
	if a, err := mail.ParseAddress(s.from); err == nil {
		fromAddr = a.Address
	}
	toAddr := to
	if a, err := mail.ParseAddress(to); err == nil {
		toAddr = a.Address
	}

	body, boundary, err := buildBody(htmlBody, textBody)
	if err != nil {
		return fmt.Errorf("build mail body: %w", err)
	}

	// Subject is RFC 2047-encoded so non-ASCII event names survive transport.
	encSubject := mime.QEncoding.Encode("utf-8", subject)
	msg := strings.Join([]string{
		"From: " + s.from,
		"To: " + to,
		"Subject: " + encSubject,
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"",
		"",
		body,
	}, "\r\n")

	// MailDev needs no auth; a real server would swap in CRAM-MD5/PLAIN auth here.
	if err := smtp.SendMail(addr, nil, fromAddr, []string{toAddr}, []byte(msg)); err != nil {
		return fmt.Errorf("smtp send to %s: %w", to, err)
	}
	return nil
}

// buildBody renders the two alternatives and reports the boundary used.
func buildBody(htmlBody, textBody string) (string, string, error) {
	boundary := "ticketbus-boundary-42b1c9"
	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	// The writer generates its own boundary; reuse it in the header above.
	boundary = w.Boundary()

	textHdr := textproto.MIMEHeader{}
	textHdr.Set("Content-Type", "text/plain; charset=utf-8")
	part, err := w.CreatePart(textHdr)
	if err != nil {
		return "", "", err
	}
	if _, err := part.Write([]byte(textBody)); err != nil {
		return "", "", err
	}

	htmlHdr := textproto.MIMEHeader{}
	htmlHdr.Set("Content-Type", "text/html; charset=utf-8")
	part, err = w.CreatePart(htmlHdr)
	if err != nil {
		return "", "", err
	}
	if _, err := part.Write([]byte(htmlBody)); err != nil {
		return "", "", err
	}

	if err := w.Close(); err != nil {
		return "", "", err
	}
	return buf.String(), boundary, nil
}
