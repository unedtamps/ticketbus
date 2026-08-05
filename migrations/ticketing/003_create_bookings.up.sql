CREATE TABLE IF NOT EXISTS bookings (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL,
    event_id   UUID NOT NULL REFERENCES events(id) ON UPDATE CASCADE,
    status     TEXT NOT NULL
        CHECK (status IN ('confirmed', 'pending', 'expired', 'cancelled')),
    expires_at TIMESTAMPTZ,
    CHECK (status <> 'pending' OR expires_at IS NOT NULL),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
