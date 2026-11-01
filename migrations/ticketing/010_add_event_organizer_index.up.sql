-- The organizer event list is keyset-paginated on (created_at, id) DESC.
-- events previously had no index beyond the primary key, so every page was a
-- sequential scan plus a sort. The trailing id makes the index usable for the
-- row-value seek and gives a deterministic tiebreak for events that share a
-- created_at (NOW() is the transaction start time, so same-transaction inserts
-- collide).
CREATE INDEX IF NOT EXISTS idx_events_organizer_created
    ON events (organizer_id, created_at DESC, id DESC);
