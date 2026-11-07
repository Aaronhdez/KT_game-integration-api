-- +goose Up
-- TODO (developer): tables, sequence, checks and indexes. See the instructions in the chat.
CREATE TABLE players (
    wallet_user_id BIGINT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    currency TEXT NOT NULL CHECK (currency IN ('USD', 'EUR', 'KES')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
-- TODO (developer): drop everything created above, in reverse order.
DROP TABLE IF EXISTS players;
