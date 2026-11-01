package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unedtamps/ticketbus/service/ticketing/internal/domain"
)

func TestCursor_RoundTrip(t *testing.T) {
	// Nanosecond precision matters: created_at carries sub-second values and
	// truncating them would silently shift the keyset boundary.
	ts := time.Date(2026, 11, 1, 4, 53, 12, 531071000, time.UTC)
	in := domain.Cursor{CreatedAt: ts, ID: "b9aa4f76-3c67-4fbd-89a2-cb6588e3f9e5"}

	out, err := domain.DecodeCursor(in.Encode())
	require.NoError(t, err)
	assert.True(t, ts.Equal(out.CreatedAt))
	assert.Equal(t, in.ID, out.ID)
}

func TestCursor_IsOpaque(t *testing.T) {
	// The encoded form must not leak the raw timestamp, so clients cannot
	// hand-build positions or depend on the internal layout.
	c := domain.Cursor{CreatedAt: time.Now(), ID: "evt-1"}
	assert.NotContains(t, c.Encode(), "|")
	assert.NotContains(t, c.Encode(), "evt-1")
}

func TestDecodeCursor_Invalid(t *testing.T) {
	cases := map[string]string{
		"not base64":    "!!!!",
		"missing id":    "MjAyNi0xMS0wMVQwNDo1MzoxMlp8", // "2026-11-01T04:53:12Z|"
		"missing pipe":  "MjAyNi0xMS0wMVQwNDo1MzoxMlo=", // "2026-11-01T04:53:12Z"
		"bad timestamp": "bm90LWEtdGltZXwxMjM=",         // "not-a-time|123"
		"empty payload": "",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := domain.DecodeCursor(raw)
			require.Error(t, err)
			assert.ErrorIs(t, err, domain.ErrInvalidCursor)
		})
	}
}
