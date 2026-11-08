-- transactions.refund_status was declared before the refund executor existed,
-- with a CHECK that never matched the refund_requests vocabulary ('succeeded',
-- 'failed') and no code writing it past 'pending'. Widen it to the values that
-- are actually settled, then backfill from refund_requests.
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS transactions_refund_status_check;

ALTER TABLE transactions
    ADD CONSTRAINT transactions_refund_status_check
    CHECK (refund_status IN ('none', 'pending', 'processing', 'succeeded', 'failed'));

-- Bring the transaction ledger in line with refund_requests for anything that
-- already settled, so the customer-facing view has one correct source.
UPDATE transactions t
SET refund_status = r.status, updated_at = NOW()
FROM refund_requests r
WHERE r.transaction_id = t.id
  AND r.status IN ('succeeded', 'failed')
  AND t.refund_status IS DISTINCT FROM r.status;

UPDATE transactions t
SET refund_status = 'processing', updated_at = NOW()
FROM refund_requests r
WHERE r.transaction_id = t.id
  AND r.status = 'processing'
  AND t.refund_status NOT IN ('processing', 'succeeded', 'failed');
