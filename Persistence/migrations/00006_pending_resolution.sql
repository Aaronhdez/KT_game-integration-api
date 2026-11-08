-- +goose Up
-- Why the Wallet did not apply a FAILED transaction, so a retry of the same
-- request gets the same answer as the first attempt (422 or 502). NULL for
-- every other status, and for transactions that failed before this column.
ALTER TABLE transactions
    ADD COLUMN failure_reason TEXT NULL
        CHECK (failure_reason IN ('INSUFFICIENT_FUNDS', 'WALLET_REJECTED')),
    ADD CONSTRAINT transactions_failure_reason_only_when_failed
        CHECK (failure_reason IS NULL OR status = 'FAILED');

-- The pending resolver looks for PENDING transactions whose last attempt is
-- old enough; this index holds only those few rows.
CREATE INDEX transactions_pending ON transactions (updated_at) WHERE status = 'PENDING';

-- +goose Down
DROP INDEX IF EXISTS transactions_pending;
ALTER TABLE transactions
    DROP CONSTRAINT IF EXISTS transactions_failure_reason_only_when_failed,
    DROP COLUMN IF EXISTS failure_reason;
