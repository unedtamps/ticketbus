-- Event photos live in object storage (S3-compatible); the event row only
-- holds the object key. Empty key = no photo uploaded yet.
ALTER TABLE events ADD COLUMN IF NOT EXISTS image_key TEXT NOT NULL DEFAULT '';
