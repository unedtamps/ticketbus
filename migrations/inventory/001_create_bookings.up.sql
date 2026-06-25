CREATE TABLE IF NOT EXISTS bookings (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL,
    event_id    UUID NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('confirmed', 'cancelled')),
    total_cents INT NOT NULL CHECK (total_cents >= 0),
    payment_id  UUID,
    refund_status TEXT NOT NULL DEFAULT 'none' CHECK (refund_status IN ('none', 'pending', 'refunded')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS booking_items (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id      UUID NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
    ticket_type_id  UUID NOT NULL,
    quantity        INT NOT NULL CHECK (quantity > 0),
    unit_price_cents INT NOT NULL CHECK (unit_price_cents >= 0)
);

CREATE TABLE IF NOT EXISTS event_status_cache (
    event_id UUID PRIMARY KEY,
    status   TEXT NOT NULL
);
