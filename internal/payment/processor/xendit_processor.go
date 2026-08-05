package processor

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nedo/TicketSaas/internal/payment/domain"
)

// XenditProcessor implements domain.PaymentProcessor against the Xendit
// Payment Sessions API (v2). Customers complete payment on the Xendit hosted
// checkout page (mode PAYMENT_LINK); we receive payment_session.completed and
// payment_session.expired webhooks.
type XenditProcessor struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewXenditProcessor creates a new XenditProcessor.
func NewXenditProcessor(baseURL, apiKey string) *XenditProcessor {
	return &XenditProcessor{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

type xenditSessionRequest struct {
	ReferenceID            string            `json:"reference_id"`
	SessionType            string            `json:"session_type"`
	Mode                   string            `json:"mode"`
	Amount                 string            `json:"amount"`
	Currency               string            `json:"currency"`
	Country                string            `json:"country"`
	Customer               xenditCustomer    `json:"customer"`
	AllowedPaymentChannels []string          `json:"allowed_payment_channels,omitempty"`
	ExpiresAt              string            `json:"expires_at,omitempty"`
	MerchantMetadata       map[string]string `json:"merchant_metadata,omitempty"`
}

type xenditCustomer struct {
	ReferenceID string `json:"reference_id"`
	Type        string `json:"type"`
	Email       string `json:"email,omitempty"`
}

type xenditSessionResponse struct {
	ID             string `json:"id"`
	ReferenceID    string `json:"reference_id"`
	Status         string `json:"status"`
	PaymentLinkURL string `json:"payment_link_url"`
	ExpiresAt      string `json:"expires_at"`
}

// CreateSession creates a hosted payment session at Xendit. The booking ID is
// used as both the reference ID and the idempotency key.
func (p *XenditProcessor) CreateSession(
	ctx context.Context,
	refID string,
	amountCents int,
	currency string,
	expiresAt time.Time,
	allowedChannels []string,
	email string,
) (*domain.SessionResult, error) {
	reqBody := xenditSessionRequest{
		ReferenceID: refID,
		SessionType: "PAY",
		Mode:        "PAYMENT_LINK",
		Amount:      strconv.Itoa(amountCents),
		Currency:    currency,
		Country:     "ID",
		Customer: xenditCustomer{
			ReferenceID: refID,
			Type:        "INDIVIDUAL",
			Email:       email,
		},
		AllowedPaymentChannels: allowedChannels,
		ExpiresAt:              expiresAt.UTC().Format(time.RFC3339),
		MerchantMetadata:       map[string]string{"booking_id": refID},
	}

	var out xenditSessionResponse
	if err := p.do(ctx, http.MethodPost, "/v2/payment_sessions", refID, reqBody, &out); err != nil {
		return nil, err
	}
	return p.toResult(&out, expiresAt), nil
}

// CancelSession expires the payment session at the gateway (idempotent:
// already-expired sessions are ignored).
func (p *XenditProcessor) CancelSession(ctx context.Context, providerRef string) error {
	_, err := p.doRaw(ctx, http.MethodPost, "/v2/payment_sessions/"+providerRef+":cancel", "", nil)
	return err
}

// GetSession fetches an existing payment session.
func (p *XenditProcessor) GetSession(
	ctx context.Context,
	providerRef string,
) (*domain.SessionResult, error) {
	var out xenditSessionResponse
	if err := p.do(
		ctx,
		http.MethodGet,
		"/v2/payment_sessions/"+providerRef,
		"",
		nil,
		&out,
	); err != nil {
		return nil, err
	}
	expiresAt := time.Time{}
	if out.ExpiresAt != "" {
		expiresAt, _ = time.Parse(time.RFC3339, out.ExpiresAt)
	}
	return p.toResult(&out, expiresAt), nil
}

func (p *XenditProcessor) toResult(
	out *xenditSessionResponse,
	fallbackExpiry time.Time,
) *domain.SessionResult {
	expiresAt := fallbackExpiry
	if out.ExpiresAt != "" {
		if parsed, err := time.Parse(time.RFC3339, out.ExpiresAt); err == nil {
			expiresAt = parsed
		}
	}
	return &domain.SessionResult{
		ProviderRef:    out.ID,
		PaymentLinkURL: out.PaymentLinkURL,
		ExpiresAt:      expiresAt,
	}
}

func (p *XenditProcessor) do(
	ctx context.Context,
	method, path, idempotencyKey string,
	body, out interface{},
) error {
	data, err := p.doRaw(ctx, method, path, idempotencyKey, body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("xendit: failed to decode response: %w", err)
	}
	return nil
}

func (p *XenditProcessor) doRaw(
	ctx context.Context,
	method, path, idempotencyKey string,
	body interface{},
) ([]byte, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, fmt.Errorf("xendit: encode request: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, &buf)
	if err != nil {
		return nil, fmt.Errorf("xendit: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set(
		"Authorization",
		"Basic "+base64.StdEncoding.EncodeToString([]byte(p.apiKey+":")),
	)
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xendit: request failed: %w", err)
	}
	defer resp.Body.Close()

	data := make([]byte, 0, 4096)
	buffer := bytes.NewBuffer(data)
	if _, err := buffer.ReadFrom(resp.Body); err != nil {
		return nil, fmt.Errorf("xendit: read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"xendit: %s %s -> %d: %s",
			method,
			path,
			resp.StatusCode,
			buffer.String(),
		)
	}
	return buffer.Bytes(), nil
}
