package processor

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
)

// XenditProcessor implements domain.PaymentProcessor against the Xendit
// Payment Sessions API (v2). Customers complete payment on the Xendit hosted
// checkout page (mode PAYMENT_LINK); we receive payment_session.completed and
// payment_session.expired webhooks.
type XenditProcessor struct {
	baseURL       string
	payoutBaseURL string
	payoutPath    string
	apiKey        string
	httpClient    *http.Client
}

// NewXenditProcessor creates a new XenditProcessor. baseURL points at the
// Payment Sessions API (e.g. https://api.xendit.co/v1); payoutBaseURL is the
// API root used for the Payouts API (https://api.xendit.co).
func NewXenditProcessor(baseURL, payoutBaseURL, payoutPath, apiKey string) *XenditProcessor {
	return &XenditProcessor{
		baseURL:       strings.TrimSuffix(baseURL, "/"),
		payoutBaseURL: strings.TrimSuffix(payoutBaseURL, "/"),
		payoutPath:    payoutPath,
		apiKey:        apiKey,
		httpClient:    &http.Client{Timeout: 15 * time.Second},
	}
}

type xenditSessionRequest struct {
	ReferenceID            string            `json:"reference_id"`
	SessionType            string            `json:"session_type"`
	Mode                   string            `json:"mode"`
	Amount                 int64             `json:"amount"`
	Currency               string            `json:"currency"`
	Country                string            `json:"country"`
	Customer               xenditCustomer    `json:"customer"`
	AllowedPaymentChannels []string          `json:"allowed_payment_channels,omitempty"`
	ExpiresAt              string            `json:"expires_at,omitempty"`
	MerchantMetadata       map[string]string `json:"merchant_metadata,omitempty"`
}

type xenditCustomer struct {
	ReferenceID      string                  `json:"reference_id"`
	Type             string                  `json:"type"`
	Email            string                  `json:"email,omitempty"`
	IndividualDetail *xenditIndividualDetail `json:"individual_detail,omitempty"`
}

type xenditIndividualDetail struct {
	GivenNames string `json:"given_names"`
	Surname    string `json:"surname"`
}

type xenditSessionResponse struct {
	PaymentSessionID string `json:"payment_session_id"`
	ReferenceID      string `json:"reference_id"`
	Status           string `json:"status"`
	CheckoutURL      string `json:"checkout_url"`
	PaymentLinkURL   string `json:"payment_link_url"`
	ExpiresAt        string `json:"expires_at"`
}

// CreateSession creates a hosted payment session at Xendit. The booking ID is
// used as both the reference ID and the idempotency key.
func (p *XenditProcessor) CreateSession(
	ctx context.Context,
	refID string,
	amountRupiah int,
	currency string,
	expiresAt time.Time,
	allowedChannels []string,
	email string,
) (*domain.SessionResult, error) {
	givenNames := strings.SplitN(email, "@", 2)[0]
	if givenNames == "" {
		givenNames = "Customer"
	}
	reqBody := xenditSessionRequest{
		ReferenceID: refID,
		SessionType: "PAY",
		Mode:        "PAYMENT_LINK",
		Amount:      int64(amountRupiah),
		Currency:    currency,
		Country:     "ID",
		Customer: xenditCustomer{
			ReferenceID: refID,
			Type:        "INDIVIDUAL",
			Email:       email,
			IndividualDetail: &xenditIndividualDetail{
				GivenNames: givenNames,
				Surname:    "-",
			},
		},
		// AllowedPaymentChannels is intentionally not sent: Xendit enables all
		// channels activated for the account when the field is omitted.
		ExpiresAt:        expiresAt.UTC().Format(time.RFC3339),
		MerchantMetadata: map[string]string{"booking_id": refID},
	}

	var out xenditSessionResponse
	if err := p.do(ctx, http.MethodPost, "/sessions", refID, reqBody, &out); err != nil {
		return nil, err
	}
	return p.toResult(&out, expiresAt), nil
}

// CancelSession expires the payment session at the gateway (idempotent:
// already-expired sessions are ignored).
func (p *XenditProcessor) CancelSession(ctx context.Context, providerRef string) error {
	_, err := p.doRaw(ctx, http.MethodPost, "/sessions/"+providerRef+"/cancel", "", nil)
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
		"/sessions/"+providerRef,
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
		ProviderRef:    out.PaymentSessionID,
		PaymentLinkURL: firstNonEmpty(out.CheckoutURL, out.PaymentLinkURL),
		ExpiresAt:      expiresAt,
	}
}

type xenditPayoutRequest struct {
	ReferenceID       string                        `json:"reference_id"`
	Currency          string                        `json:"currency"`
	ChannelCode       string                        `json:"channel_code"`
	ChannelProperties xenditPayoutChannelProperties `json:"channel_properties"`
	Amount            int64                         `json:"amount"`
	Description       string                        `json:"description,omitempty"`
	Type              string                        `json:"type"`
}

type xenditPayoutChannelProperties struct {
	AccountHolderName string `json:"account_holder_name"`
	AccountNumber     string `json:"account_number"`
}

type xenditPayoutResponse struct {
	ID          string `json:"id"`
	ReferenceID string `json:"reference_id"`
	Status      string `json:"status"`
	FailureCode string `json:"failure_code"`
}

// CreatePayout sends funds to the organizer's bank account through the Xendit
// Payouts API. A 4xx (except 429) is classified as a permanent rejection so the
// dispatcher fails the payout instead of retrying forever.
func (p *XenditProcessor) CreatePayout(
	ctx context.Context,
	req domain.PayoutRequest,
) (*domain.PayoutResult, error) {
	body := xenditPayoutRequest{
		ReferenceID: req.ReferenceID,
		Currency:    req.Currency,
		ChannelCode: "ID_" + strings.ToUpper(req.BankCode),
		ChannelProperties: xenditPayoutChannelProperties{
			AccountHolderName: req.AccountHolderName,
			AccountNumber:     req.AccountNumber,
		},
		Amount:      req.AmountRupiah,
		Description: req.Description,
		Type:        "DIRECT_DISBURSEMENT",
	}

	var out xenditPayoutResponse
	if err := p.doURL(ctx, http.MethodPost, p.payoutBaseURL+p.payoutPath,
		req.IdempotencyKey, body, &out); err != nil {
		return nil, err
	}
	return &domain.PayoutResult{
		ProviderRef: firstNonEmpty(out.ID, out.ReferenceID, req.ReferenceID),
		Status:      out.Status,
		FailureCode: out.FailureCode,
	}, nil
}

// GetPayout fetches the gateway's current state of a previously created
// payout. The dispatcher's reconciliation poll uses it as the source of truth
// for payouts whose webhook never arrived.
func (p *XenditProcessor) GetPayout(
	ctx context.Context,
	payoutID string,
) (*domain.PayoutResult, error) {
	var out xenditPayoutResponse
	if err := p.doURL(
		ctx,
		http.MethodGet,
		p.payoutBaseURL+p.payoutPath+"/"+url.PathEscape(payoutID),
		"",
		nil,
		&out,
	); err != nil {
		return nil, err
	}
	return &domain.PayoutResult{
		ProviderRef: firstNonEmpty(out.ID, payoutID),
		Status:      out.Status,
		FailureCode: out.FailureCode,
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (p *XenditProcessor) do(
	ctx context.Context,
	method, path, idempotencyKey string,
	body, out interface{},
) error {
	return p.doURL(ctx, method, p.baseURL+path, idempotencyKey, body, out)
}

func (p *XenditProcessor) doRaw(
	ctx context.Context,
	method, path, idempotencyKey string,
	body interface{},
) ([]byte, error) {
	return p.doURLRaw(ctx, method, p.baseURL+path, idempotencyKey, body)
}

func (p *XenditProcessor) doURL(
	ctx context.Context,
	method, url, idempotencyKey string,
	body, out interface{},
) error {
	data, err := p.doURLRaw(ctx, method, url, idempotencyKey, body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("xendit: failed to decode response: %w", err)
	}
	return nil
}

func (p *XenditProcessor) doURLRaw(
	ctx context.Context,
	method, url, idempotencyKey string,
	body interface{},
) ([]byte, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, fmt.Errorf("xendit: encode request: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, url, &buf)
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
		httpErr := fmt.Errorf(
			"xendit: %s %s -> %d: %s",
			method,
			url,
			resp.StatusCode,
			buffer.String(),
		)
		// 401/403 are credential problems (a missing API key scope, for
		// example): the payout is retried until the configuration is fixed.
		// 429 is a rate limit and is retryable too. The other 4xx are
		// permanent rejections (invalid account, unsupported channel, ...).
		switch {
		case resp.StatusCode == http.StatusUnauthorized,
			resp.StatusCode == http.StatusForbidden:
			return nil, &domain.AuthProcessorError{Err: httpErr}
		case resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests:
			return nil, &domain.PermanentProcessorError{Err: httpErr}
		}
		return nil, httpErr
	}
	return buffer.Bytes(), nil
}
