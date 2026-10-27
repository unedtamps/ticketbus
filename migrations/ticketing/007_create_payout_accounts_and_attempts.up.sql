-- Organizer payout destination (settings, editable at any time).
CREATE TABLE IF NOT EXISTS organizer_payout_accounts (
    organizer_id             UUID PRIMARY KEY,
    bank_code                TEXT NOT NULL,
    bank_account_number      TEXT NOT NULL,
    bank_account_holder_name TEXT NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One row per payout attempt for an event. An event may have several
-- attempts: a retry after a failure, or a top-up that collects transactions
-- which completed after the first payout. UNIQUE (event_id, attempt) makes the
-- delivery of one attempt idempotent.
CREATE TABLE IF NOT EXISTS event_payout_attempts (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id                 UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    attempt                  INTEGER NOT NULL,
    organizer_id             UUID NOT NULL,
    bank_code                TEXT NOT NULL,
    bank_account_number      TEXT NOT NULL,
    bank_account_holder_name TEXT NOT NULL,
    status                   TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'paid_out', 'failed')),
    amount_rupiah            BIGINT,
    provider                 TEXT NOT NULL DEFAULT 'mock',
    provider_ref             TEXT,
    failure_reason           TEXT,
    requested_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at             TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (event_id, attempt)
);

CREATE INDEX IF NOT EXISTS idx_event_payout_attempts_event
    ON event_payout_attempts (event_id, attempt DESC);
