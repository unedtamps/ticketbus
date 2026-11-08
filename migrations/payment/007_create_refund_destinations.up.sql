-- Customer refund destination: where the platform sends a refund when the
-- original payment channel cannot refund to source (VA, retail outlet).
-- Reusable across events; one row per user.
CREATE TABLE IF NOT EXISTS customer_refund_destinations (
    user_id              UUID PRIMARY KEY,
    channel_code         TEXT NOT NULL,
    account_number       TEXT NOT NULL,
    account_holder_name  TEXT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Which retry of this refund the executor is on: the Payouts idempotency key
-- includes it, so an admin retry gets a fresh gateway payout instead of
-- replaying the failed one.
ALTER TABLE refund_requests ADD COLUMN IF NOT EXISTS attempt INTEGER NOT NULL DEFAULT 0;

-- Why a payout failed, for the admin refund table and the retry decision.
ALTER TABLE refund_requests ADD COLUMN IF NOT EXISTS failure_reason TEXT;

-- provider_ref used to be seeded with the customer's payment-session id at
-- create time, which the reconciliation poll mistook for a gateway payout id.
-- Only a real payout id may live here.
UPDATE refund_requests r
SET provider_ref = NULL
FROM transactions t
WHERE t.id = r.transaction_id
  AND r.provider_ref = t.provider_ref
  AND r.status IN ('pending', 'processing');

ALTER TABLE refund_requests
    ALTER COLUMN provider_ref DROP DEFAULT;
