-- +goose Up
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
DROP TABLE IF EXISTS bets;
DROP SEQUENCE IF EXISTS bet_id_seq;
