-- See migrations/payment/004_add_outbox_trace_context.up.sql for why the
-- outbox has to carry trace context across the publish boundary.
ALTER TABLE outbox ADD COLUMN IF NOT EXISTS trace_context TEXT;
