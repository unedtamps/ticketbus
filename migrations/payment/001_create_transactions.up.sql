CREATE TABLE IF NOT EXISTS transactions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL,
    booking_id      UUID NOT NULL UNIQUE,
    event_id        UUID NOT NULL,
    amount_cents    BIGINT NOT NULL CHECK (amount_cents >= 0),
    currency        TEXT NOT NULL DEFAULT 'IDR',
    provider        TEXT NOT NULL DEFAULT 'mock',
    provider_ref    TEXT,
    payment_link_url TEXT,
    customer_email  TEXT,
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status          TEXT NOT NULL CHECK (status IN ('initiated', 'pending', 'completed', 'expired')),
    refund_status   TEXT NOT NULL DEFAULT 'none'
        CHECK (refund_status IN ('none', 'pending', 'success'))
);
