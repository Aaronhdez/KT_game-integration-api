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

-- +goose Down
-- TODO (developer): drop everything created above, in reverse order.
DROP TABLE IF EXISTS bets;
DROP SEQUENCE IF EXISTS bet_id_seq;
DROP TABLE IF EXISTS players;
