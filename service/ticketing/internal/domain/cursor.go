package domain

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidCursor is returned when a client sends a cursor that does not
// decode. Callers should surface it as a 400 rather than falling back to the
// first page, so a broken cursor does not silently look like "no more data".
var ErrInvalidCursor = errors.New("invalid cursor")

// Cursor is a keyset position: the (created_at, id) pair of the last row the
// caller has already seen. Ordering is DESC, so the next page selects rows
// strictly "smaller" than this pair.
//
// The ID is not decoration. created_at defaults to NOW(), which in Postgres is
// the transaction start time, so rows inserted in one transaction share an
// identical timestamp. A timestamp-only cursor would then skip (with <) or
// duplicate (with <=) those rows; the pair comparison resolves them.
type Cursor struct {
	CreatedAt time.Time
	ID        string
}

// Encode serialises the cursor as an opaque base64url string. Opacity keeps
// clients from hand-building positions and lets the scheme change later.
func (c Cursor) Encode() string {
	raw := c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses a cursor produced by Encode.
func DecodeCursor(s string) (*Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: not base64url", ErrInvalidCursor)
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	if !ok || id == "" {
		return nil, fmt.Errorf("%w: expected timestamp|id", ErrInvalidCursor)
	}
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return nil, fmt.Errorf("%w: bad timestamp", ErrInvalidCursor)
	}
	return &Cursor{CreatedAt: parsed, ID: id}, nil
}
