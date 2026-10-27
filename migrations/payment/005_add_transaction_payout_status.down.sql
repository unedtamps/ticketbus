DROP INDEX IF EXISTS idx_transactions_event_payoutable;
ALTER TABLE transactions DROP COLUMN IF EXISTS payout_status;
