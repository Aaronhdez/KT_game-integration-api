//go:build integration

package persistence

import (
	"testing"
)

func TestMigrate_EmptyDatabase_AppliesTheSchemaMigration(t *testing.T) {
	pool := connectTestDatabase(t)
	dropAllTables(t, pool)

	result, err := Migrate(testContext(t), pool)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) == 0 || result[0] != 1 {
		t.Errorf("expected the applied versions to start with 1, got %v", result)
	}
}

func TestMigrate_UpToDateDatabase_AppliesNothing(t *testing.T) {
	pool := migratedTestDatabase(t)

	result, err := Migrate(testContext(t), pool)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected no migrations to be applied, got %v", result)
	}
}

func TestSchema_WithdrawWithZeroAmount_IsRejected(t *testing.T) {
	pool := migratedTestDatabase(t)
	insertTestPlayer(t, pool)

	_, err := pool.Exec(testContext(t), `
		INSERT INTO transactions (id, type, status, wallet_user_id, bet_id,
			provider_transaction_id, amount, currency, old_balance, created_at, updated_at)
		VALUES (gen_random_uuid(), 'WITHDRAW', 'PENDING', $1, 1,
			'zero-withdraw', 0, 'USD', 100, now(), now())`, testPlayerID)

	assertCheckViolation(t, err)
}

func TestSchema_CancelWithoutCancelledTransaction_IsRejected(t *testing.T) {
	pool := migratedTestDatabase(t)
	insertTestPlayer(t, pool)

	_, err := pool.Exec(testContext(t), `
		INSERT INTO transactions (id, type, status, wallet_user_id, bet_id,
			provider_transaction_id, amount, currency, old_balance, created_at, updated_at)
		VALUES (gen_random_uuid(), 'CANCEL', 'PENDING', $1, 1,
			'cancel-without-original', 10, 'USD', 100, now(), now())`, testPlayerID)

	assertCheckViolation(t, err)
}

func TestSchema_UnknownCurrency_IsRejected(t *testing.T) {
	pool := migratedTestDatabase(t)

	_, err := pool.Exec(testContext(t), `
		INSERT INTO players (wallet_user_id, username, password_hash, currency)
		VALUES (2, 'gbp_player', 'not-a-real-hash', 'GBP')`)

	assertCheckViolation(t, err)
}
