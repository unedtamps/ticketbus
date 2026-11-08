DROP TABLE IF EXISTS customer_refund_destinations;
ALTER TABLE refund_requests DROP COLUMN IF EXISTS attempt;
