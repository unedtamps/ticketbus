-- Restore the original vocabulary. Rows carrying a widened value fall back to
-- the closest original state so the constraint can be re-added.
UPDATE transactions SET refund_status = 'pending' WHERE refund_status = 'processing';
UPDATE transactions SET refund_status = 'success' WHERE refund_status = 'succeeded';
UPDATE transactions SET refund_status = 'none'    WHERE refund_status = 'failed';

ALTER TABLE transactions DROP CONSTRAINT IF EXISTS transactions_refund_status_check;

ALTER TABLE transactions
    ADD CONSTRAINT transactions_refund_status_check
    CHECK (refund_status IN ('none', 'pending', 'success'));
