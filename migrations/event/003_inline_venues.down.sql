CREATE TABLE IF NOT EXISTS venues (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    address    TEXT NOT NULL,
    capacity   INT NOT NULL CHECK (capacity > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE events ADD COLUMN venue_id UUID REFERENCES venues(id);
ALTER TABLE events DROP COLUMN venue_name;
ALTER TABLE events DROP COLUMN venue_address;
ALTER TABLE events DROP COLUMN venue_capacity;
