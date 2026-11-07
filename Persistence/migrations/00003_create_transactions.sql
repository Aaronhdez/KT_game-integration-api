-- +goose Up
CREATE TABLE transactions (
    id UUID PRIMARY KEY,
    type TEXT NOT NULL CHECK (type IN ('WITHDRAW', 'DEPOSIT', 'CANCEL')),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'COMPLETED', 'FAILED', 'CANCELLED')),
    wallet_user_id BIGINT NOT NULL REFERENCES players (wallet_user_id),
    bet_id BIGINT NOT NULL,
    provider_transaction_id TEXT NOT NULL,
    cancelled_transaction_id UUID NULL REFERENCES transactions (id),
    amount NUMERIC(20,2) NOT NULL,
    currency TEXT NOT NULL CHECK (currency IN ('USD', 'EUR', 'KES')),
    old_balance NUMERIC(20,2) NOT NULL,
    new_balance NUMERIC(20,2) NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK (amount >= 0),
    CHECK (type <> 'WITHDRAW' OR amount > 0),
    CHECK ((type = 'CANCEL') = (cancelled_transaction_id IS NOT NULL))
);

CREATE UNIQUE INDEX transactions_idempotency_key ON transactions (wallet_user_id, provider_transaction_id) WHERE type IN ('WITHDRAW', 'DEPOSIT');
CREATE UNIQUE INDEX transactions_active_settlement ON transactions (bet_id) WHERE type = 'DEPOSIT' AND status IN ('PENDING', 'COMPLETED');
CREATE UNIQUE INDEX transactions_single_cancel ON transactions (cancelled_transaction_id) WHERE type = 'CANCEL';

-- +goose Down
DROP INDEX IF EXISTS transactions_single_cancel;
DROP INDEX IF EXISTS transactions_active_settlement;
DROP INDEX IF EXISTS transactions_idempotency_key;
DROP TABLE IF EXISTS transactions;
