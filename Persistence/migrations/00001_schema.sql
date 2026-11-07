-- +goose Up
-- TODO (developer): tables, sequence, checks and indexes. See the instructions in the chat.
CREATE TABLE players (
    wallet_user_id BIGINT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    currency TEXT NOT NULL CHECK (currency IN ('USD', 'EUR', 'KES')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE SEQUENCE bet_id_seq START 1;

CREATE TABLE bets (
    id BIGINT PRIMARY KEY,
    wallet_user_id BIGINT NOT NULL REFERENCES players (wallet_user_id),
    currency TEXT NOT NULL CHECK (currency IN ('USD', 'EUR', 'KES')),
    stake NUMERIC(20,2) NOT NULL CHECK (stake > 0),
    status TEXT NOT NULL CHECK (status IN ('PLACED', 'WON', 'LOST', 'CANCELLED')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

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
    updated_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
-- TODO (developer): drop everything created above, in reverse order.
DROP TABLE IF EXISTS transactions;
DROP TABLE IF EXISTS bets;
DROP SEQUENCE IF EXISTS bet_id_seq;
DROP TABLE IF EXISTS players;
