-- available_seat used to default to 0 and was only filled in at approval, so
-- events that had never been published read as "0 of N available" — rendered
-- as 100% sold / sold out. Published events are left alone: there the counter
-- is the authoritative, actively decremented value.
UPDATE ticket_types tt
SET available_seat = tt.quantity
FROM events e
WHERE tt.event_id = e.id
  AND e.status <> 'published'
  AND tt.available_seat = 0;
