-- One row per payout attempt. UNIQUE (event_id, attempt) makes the Kafka
-- delivery of a single attempt idempotent; several attempts per event are
-- allowed (a retry after failure, or a top-up that collects transactions which
-- completed after the first payout).
CREATE TABLE IF NOT EXISTS payouts (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id       UUID NOT NULL,
    attempt        INTEGER NOT NULL,
    organizer_id   UUID NOT NULL,
    amount_rupiah  BIGINT NOT NULL DEFAULT 0 CHECK (amount_rupiah >= 0),
    currency       TEXT NOT NULL DEFAULT 'IDR',
    status         TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'paid_out', 'failed')),
    provider       TEXT NOT NULL DEFAULT 'mock',
    provider_ref   TEXT,
    failure_reason TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (event_id, attempt)
);
