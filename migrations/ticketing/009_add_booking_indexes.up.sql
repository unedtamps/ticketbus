-- Rollup queries filter bookings by event and status (the organizer event list
-- sums confirmed revenue per event), and booking_items is always joined by
-- booking_id. Neither column had an index: Postgres does not create one for a
-- foreign key, so every per-event aggregate was a sequential scan.
CREATE INDEX IF NOT EXISTS idx_bookings_event_status
    ON bookings (event_id, status);

CREATE INDEX IF NOT EXISTS idx_booking_items_booking
    ON booking_items (booking_id);
