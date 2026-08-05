CREATE TABLE IF NOT EXISTS transactions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL,
    booking_id   UUID NOT NULL,
    event_id     UUID NOT NULL,
    amount_cents BIGINT NOT NULL CHECK (amount_cents >= 0),
    currency     TEXT NOT NULL DEFAULT 'USD',
    provider     TEXT NOT NULL DEFAULT 'mock',
    provider_ref TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status       TEXT NOT NULL CHECK (status IN ('success', 'pending', 'expired')),
    refund_status TEXT NOT NULL DEFAULT 'none'
        CHECK (refund_status IN ('none', 'pending', 'success'))
);
