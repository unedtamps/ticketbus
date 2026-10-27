package processor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unedtamps/ticketbus/service/payment/internal/domain"
)

func TestXenditCreatePayout_RequestShape(t *testing.T) {
	var gotPath, gotAuth, gotIdem string
	var gotBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotIdem = r.Header.Get("Idempotency-Key")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"po-1","reference_id":"payout-1","status":"ACCEPTED"}`))
	}))
	defer srv.Close()

	p := NewXenditProcessor("http://unused/v1", srv.URL, "/v2/payouts", "secret-key")
	result, err := p.CreatePayout(context.Background(), domain.PayoutRequest{
		ReferenceID:       "payout-1",
		IdempotencyKey:    "payout-payout-1-1",
		AmountRupiah:      15000,
		Currency:          "IDR",
		BankCode:          "bca",
		AccountNumber:     "1234567890",
		AccountHolderName: "Budi",
		Description:       "Event payout evt-1",
	})
	require.NoError(t, err)

	assert.Equal(t, "/v2/payouts", gotPath)
	assert.Equal(t,
		"Basic "+base64.StdEncoding.EncodeToString([]byte("secret-key:")),
		gotAuth)
	assert.Equal(t, "payout-payout-1-1", gotIdem)

	assert.Equal(t, "payout-1", gotBody["reference_id"])
	assert.Equal(t, "IDR", gotBody["currency"])
	assert.Equal(t, "ID_BCA", gotBody["channel_code"])
	assert.EqualValues(t, 15000, gotBody["amount"])
	assert.Equal(t, "DIRECT_DISBURSEMENT", gotBody["type"])

	props, ok := gotBody["channel_properties"].(map[string]interface{})
	require.True(t, ok, "channel_properties missing: %v", gotBody)
	assert.Equal(t, "Budi", props["account_holder_name"])
	assert.Equal(t, "1234567890", props["account_number"])

	assert.Equal(t, "po-1", result.ProviderRef)
	assert.Equal(t, "ACCEPTED", result.Status)
}

func TestXenditCreatePayout_ErrorClassification(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		permanent bool
	}{
		{"bad request is permanent", http.StatusBadRequest, true},
		{"unsupported channel is permanent", http.StatusUnprocessableEntity, true},
		{"rate limit is retryable", http.StatusTooManyRequests, false},
		{"server error is retryable", http.StatusInternalServerError, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error_code":"API_VALIDATION_ERROR","message":"nope"}`))
			}))
			defer srv.Close()

			p := NewXenditProcessor("http://unused/v1", srv.URL, "/v2/payouts", "key")
			_, err := p.CreatePayout(context.Background(), domain.PayoutRequest{
				ReferenceID: "payout-1", IdempotencyKey: "k", AmountRupiah: 10000, Currency: "IDR",
			})
			require.Error(t, err)
			assert.Equal(t, tc.permanent, domain.IsPermanentProcessorError(err))
		})
	}
}
