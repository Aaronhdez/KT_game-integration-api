-- +goose Up
-- Provider transaction IDs of WITHDRAW and DEPOSIT transactions are unique
-- across the whole system (assessment §6.1), not per player. Same index name,
-- so the repository keeps mapping its violation to ErrTransactionExists.
DROP INDEX transactions_idempotency_key;
CREATE UNIQUE INDEX transactions_idempotency_key ON transactions (provider_transaction_id) WHERE type IN ('WITHDRAW', 'DEPOSIT');

-- +goose Down
DROP INDEX transactions_idempotency_key;
CREATE UNIQUE INDEX transactions_idempotency_key ON transactions (wallet_user_id, provider_transaction_id) WHERE type IN ('WITHDRAW', 'DEPOSIT');
