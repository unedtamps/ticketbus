-- The buyer's email is snapshotted at reserve time so the ticket.issued event
-- can carry it to the mail service without a lookup against the auth service.
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS customer_email TEXT NOT NULL DEFAULT '';
