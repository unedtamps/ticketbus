CREATE TABLE IF NOT EXISTS ticket_types (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id       UUID NOT NULL REFERENCES events(id) ON UPDATE CASCADE,
    name           TEXT NOT NULL,
    price_cents    BIGINT NOT NULL CHECK (price_cents >= 0),
    quantity       INT NOT NULL CHECK (quantity > 0),
    available_seat INT NOT NULL DEFAULT 0
        CHECK (available_seat >= 0 AND available_seat <= quantity),
    max_per_order  INT NOT NULL DEFAULT 5 CHECK (max_per_order > 0)
);
