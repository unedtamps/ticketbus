-- Stores the W3C traceparent of the request that produced this event.
--
-- The outbox worker publishes from its own background context, seconds after the
-- originating request finished, so without this column the producer span would
-- become an unrelated root trace and the consumer on the other side would break
-- the chain at exactly the point where the trace is most useful.
ALTER TABLE outbox ADD COLUMN IF NOT EXISTS trace_context TEXT;
