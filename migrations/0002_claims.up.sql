ALTER TABLE transfers
    ADD COLUMN status TEXT NOT NULL DEFAULT 'completed'
        CHECK (status IN ('completed', 'reversed')),
    ADD COLUMN reversed_at TIMESTAMPTZ,
    ADD COLUMN sender_balance_after_reversal BIGINT;

-- The hold-sum query filters on recipient_id AND created_at, so the plain
-- recipient index is replaced by a composite one (its prefix still serves
-- recipient-only lookups).
DROP INDEX transfers_recipient_idx;
CREATE INDEX transfers_recipient_created_idx
    ON transfers (recipient_id, created_at);
