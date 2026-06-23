CREATE TABLE IF NOT EXISTS transactions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL,
    booking_id   UUID NOT NULL UNIQUE,
    amount_cents INT NOT NULL CHECK (amount_cents >= 0),
    currency     TEXT NOT NULL DEFAULT 'USD',
    status       TEXT NOT NULL CHECK (status IN ('initiated', 'processing', 'completed', 'failed')),
    provider     TEXT NOT NULL DEFAULT 'mock',
    provider_ref TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
