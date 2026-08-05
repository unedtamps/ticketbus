CREATE TABLE IF NOT EXISTS booking_items (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id     UUID NOT NULL REFERENCES bookings(id) ON UPDATE CASCADE,
    ticket_type_id UUID NOT NULL,
    quantity       INT NOT NULL CHECK (quantity > 0),
    total_price_cents    BIGINT NOT NULL CHECK (total_price_cents >= 0)
);
