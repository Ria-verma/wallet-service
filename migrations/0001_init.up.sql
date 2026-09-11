CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE wallets (
    user_id       UUID PRIMARY KEY REFERENCES users (id),
    balance_paise BIGINT NOT NULL CHECK (balance_paise >= 0),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sender_id            UUID NOT NULL REFERENCES users (id),
    recipient_id         UUID NOT NULL REFERENCES users (id),
    amount_paise         BIGINT NOT NULL CHECK (amount_paise > 0),
    idempotency_key      TEXT NOT NULL,
    body_hash            TEXT NOT NULL,
    sender_balance_after BIGINT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (sender_id, idempotency_key)
);

CREATE INDEX transfers_recipient_idx ON transfers (recipient_id);
