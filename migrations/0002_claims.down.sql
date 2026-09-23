ALTER TABLE transfers
    DROP COLUMN status,
    DROP COLUMN reversed_at,
    DROP COLUMN sender_balance_after_reversal;

DROP INDEX transfers_recipient_created_idx;
CREATE INDEX transfers_recipient_idx ON transfers (recipient_id);
