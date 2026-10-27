-- Per-transaction payout state: none → pending → paid_out | failed.
-- 'failed' transactions are included again by the next payout attempt.
ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS payout_status TEXT NOT NULL DEFAULT 'none'
        CHECK (payout_status IN ('none', 'pending', 'paid_out', 'failed'));

-- Payout aggregation scans an event's not-yet-paid transactions.
CREATE INDEX IF NOT EXISTS idx_transactions_event_payoutable
    ON transactions (event_id)
    WHERE payout_status <> 'paid_out';
