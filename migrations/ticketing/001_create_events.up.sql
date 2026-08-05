CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS events (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organizer_id   UUID NOT NULL,
    title          TEXT NOT NULL,
    description    TEXT DEFAULT '',
    start_at       TIMESTAMPTZ NOT NULL,
    end_at         TIMESTAMPTZ NOT NULL,
    status         TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'pending', 'published', 'rejected', 'cancelled')),
    reviewed_by    UUID,
    reviewed_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    venue_name     TEXT NOT NULL DEFAULT '',
    venue_address  TEXT NOT NULL DEFAULT '',
    venue_capacity INT NOT NULL DEFAULT 0
        CHECK (venue_capacity >= 0)
);
