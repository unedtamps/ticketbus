CREATE TABLE IF NOT EXISTS refund_requests (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id         UUID NOT NULL,
    booking_id       UUID NOT NULL,
    transaction_id   UUID NOT NULL REFERENCES transactions(id) ON UPDATE CASCADE,
    customer_email   TEXT,
    amount_rupiah    BIGINT NOT NULL CHECK (amount_rupiah >= 0),
    currency         TEXT NOT NULL DEFAULT 'IDR',
    status           TEXT NOT NULL
        CHECK (status IN ('pending', 'processing', 'succeeded', 'failed')),
    reason           TEXT NOT NULL DEFAULT 'event_cancelled',
    provider_ref     TEXT,
    idempotency_key  TEXT NOT NULL UNIQUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_refund_requests_event ON refund_requests(event_id);
CREATE INDEX IF NOT EXISTS idx_refund_requests_booking ON refund_requests(booking_id);
CREATE INDEX IF NOT EXISTS idx_refund_requests_transaction ON refund_requests(transaction_id);
CREATE INDEX IF NOT EXISTS idx_refund_requests_status ON refund_requests(status);
