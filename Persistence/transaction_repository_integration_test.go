//go:build integration

package persistence

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// The database keeps rows between runs, so every test builds its
// transactions with fresh IDs (gen_random_uuid, bet_id_seq) and provider IDs
// derived from them: no test collides with another or with an earlier run.

func TestTransactionRepository_Insert_PendingWithdraw_StoresEveryField(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newTransactionRepository(pool)
	withdraw := newTestWithdraw(t, pool, eurPlayerID)

	err := repository.Insert(testContext(t), withdraw)

	assertStoredTransaction(t, pool, err, withdraw)
}

func TestTransactionRepository_Insert_CompletedWithdraw_StoresTheNewBalance(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newTransactionRepository(pool)
	withdraw := newTestWithdraw(t, pool, eurPlayerID)
	completeTransaction(t, &withdraw, "9000000000.09")

	err := repository.Insert(testContext(t), withdraw)

	assertStoredTransaction(t, pool, err, withdraw)
}

func TestTransactionRepository_Insert_SameProviderIDForSamePlayer_ReturnsTransactionExists(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newTransactionRepository(pool)
	first := newTestWithdraw(t, pool, eurPlayerID)
	insertTransactionRow(t, pool, first)
	retry := newTestWithdraw(t, pool, eurPlayerID)
	retry.ProviderTransactionID = first.ProviderTransactionID

	err := repository.Insert(testContext(t), retry)

	assertErrorIs(t, err, domain.ErrTransactionExists)
}

func TestTransactionRepository_Insert_SameProviderIDForAnotherPlayer_IsStored(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newTransactionRepository(pool)
	first := newTestWithdraw(t, pool, eurPlayerID)
	insertTransactionRow(t, pool, first)
	anotherPlayers := newTestWithdraw(t, pool, usdPlayerID)
	anotherPlayers.ProviderTransactionID = first.ProviderTransactionID

	err := repository.Insert(testContext(t), anotherPlayers)

	assertStoredTransaction(t, pool, err, anotherPlayers)
}

func TestTransactionRepository_Insert_SecondActiveDepositOnSameBet_ReturnsBetAlreadySettled(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newTransactionRepository(pool)
	withdraw := newTestWithdraw(t, pool, eurPlayerID)
	insertTransactionRow(t, pool, newTestDeposit(t, pool, withdraw))
	second := newTestDeposit(t, pool, withdraw)

	err := repository.Insert(testContext(t), second)

	assertErrorIs(t, err, domain.ErrBetAlreadySettled)
}

func TestTransactionRepository_Insert_DepositAfterTheFirstWasCancelled_IsStored(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newTransactionRepository(pool)
	withdraw := newTestWithdraw(t, pool, eurPlayerID)
	cancelledDeposit := newTestDeposit(t, pool, withdraw)
	completeTransaction(t, &cancelledDeposit, "125.00")
	markCancelled(t, &cancelledDeposit)
	insertTransactionRow(t, pool, cancelledDeposit)
	newDeposit := newTestDeposit(t, pool, withdraw)

	err := repository.Insert(testContext(t), newDeposit)

	assertStoredTransaction(t, pool, err, newDeposit)
}

func TestTransactionRepository_Insert_SecondCancelOfSameTransaction_ReturnsAlreadyCancelled(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newTransactionRepository(pool)
	withdraw := newTestWithdraw(t, pool, eurPlayerID)
	insertTransactionRow(t, pool, withdraw)
	insertTransactionRow(t, pool, newTestCancel(t, pool, withdraw))
	second := newTestCancel(t, pool, withdraw)

	err := repository.Insert(testContext(t), second)

	assertErrorIs(t, err, domain.ErrAlreadyCancelled)
}

func TestTransactionRepository_FindByID_PendingWithdraw_ReturnsItWithoutNewBalance(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newTransactionRepository(pool)
	withdraw := newTestWithdraw(t, pool, eurPlayerID)
	insertTransactionRow(t, pool, withdraw)

	result, err := repository.FindByID(testContext(t), eurPlayerID, withdraw.ID)

	assertTransaction(t, result, err, withdraw)
}

func TestTransactionRepository_FindByID_CompletedWithdraw_ReturnsItWithTheNewBalance(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newTransactionRepository(pool)
	withdraw := newTestWithdraw(t, pool, eurPlayerID)
	completeTransaction(t, &withdraw, "9000000000.09")
	insertTransactionRow(t, pool, withdraw)

	result, err := repository.FindByID(testContext(t), eurPlayerID, withdraw.ID)

	assertTransaction(t, result, err, withdraw)
}

func TestTransactionRepository_FindByID_Cancel_ReturnsItWithTheCancelledTransactionID(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newTransactionRepository(pool)
	withdraw := newTestWithdraw(t, pool, eurPlayerID)
	insertTransactionRow(t, pool, withdraw)
	cancel := newTestCancel(t, pool, withdraw)
	insertTransactionRow(t, pool, cancel)

	result, err := repository.FindByID(testContext(t), eurPlayerID, cancel.ID)

	assertTransaction(t, result, err, cancel)
}

func TestTransactionRepository_FindByID_AnotherPlayersTransaction_ReturnsTransactionNotFound(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newTransactionRepository(pool)
	withdraw := newTestWithdraw(t, pool, eurPlayerID)
	insertTransactionRow(t, pool, withdraw)

	_, err := repository.FindByID(testContext(t), usdPlayerID, withdraw.ID)

	assertErrorIs(t, err, domain.ErrTransactionNotFound)
}

func TestTransactionRepository_FindByID_UnknownID_ReturnsTransactionNotFound(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newTransactionRepository(pool)
	neverInserted := newTestWithdraw(t, pool, eurPlayerID)

	_, err := repository.FindByID(testContext(t), eurPlayerID, neverInserted.ID)

	assertErrorIs(t, err, domain.ErrTransactionNotFound)
}

/////////////////////////////////////////////////////////////////////////////////////

// newTestWithdraw builds a PENDING withdraw of 10.50 EUR on a fresh bet ID.
func newTestWithdraw(t *testing.T, pool *pgxpool.Pool, userID domain.UserID) domain.Transaction {
	t.Helper()
	id := newTransactionID(t, pool)
	withdraw, err := domain.NewWithdraw(domain.TransactionDetails{
		ID:                    id,
		UserID:                userID,
		BetID:                 newTestBet(t, pool, userID, "10.50").ID,
		ProviderTransactionID: domain.ProviderTransactionID("withdraw-" + id),
		Amount:                parseMoney(t, "10.50"),
		Currency:              domain.EUR,
		OldBalance:            parseMoney(t, "100.00"),
	}, testNow())
	if err != nil {
		t.Fatalf("new withdraw: %v", err)
	}
	return withdraw
}

// newTransactionID asks PostgreSQL for a UUID, so the tests need no UUID library.
func newTransactionID(t *testing.T, pool *pgxpool.Pool) domain.TransactionID {
	t.Helper()
	var id string
	if err := pool.QueryRow(testContext(t), "SELECT gen_random_uuid()::text").Scan(&id); err != nil {
		t.Fatalf("generate transaction id: %v", err)
	}
	return domain.TransactionID(id)
}

// assertStoredTransaction checks that the write succeeded and that the row in
// the database, read with plain SQL, equals expected.
func assertStoredTransaction(t *testing.T, pool *pgxpool.Pool, err error, expected domain.Transaction) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := readTransactionRow(t, pool, expected.ID)
	assertTransaction(t, result, nil, expected)
}

// readTransactionRow reads a transaction with plain SQL, independently of the
// repository under test. Nullable columns are scanned into pointers.
func readTransactionRow(t *testing.T, pool *pgxpool.Pool, id domain.TransactionID) domain.Transaction {
	t.Helper()
	var (
		txType, status, providerID, currency string
		userID, betID                        int64
		cancelledID, newBalanceText          *string
		amountText, oldBalanceText           string
		createdAt, updatedAt                 time.Time
	)
	err := pool.QueryRow(testContext(t), `
		SELECT type, status, wallet_user_id, bet_id, provider_transaction_id,
			cancelled_transaction_id::text, amount::text, currency,
			old_balance::text, new_balance::text, created_at, updated_at
		FROM transactions WHERE id = $1`, string(id)).
		Scan(&txType, &status, &userID, &betID, &providerID,
			&cancelledID, &amountText, &currency,
			&oldBalanceText, &newBalanceText, &createdAt, &updatedAt)
	if err != nil {
		t.Fatalf("read transaction %s: %v", id, err)
	}

	tx := domain.Transaction{
		ID:                    id,
		Type:                  domain.TransactionType(txType),
		Status:                domain.TransactionStatus(status),
		UserID:                domain.UserID(userID),
		BetID:                 domain.BetID(betID),
		ProviderTransactionID: domain.ProviderTransactionID(providerID),
		Amount:                parseMoney(t, amountText),
		Currency:              domain.Currency(currency),
		OldBalance:            parseMoney(t, oldBalanceText),
		CreatedAt:             createdAt,
		UpdatedAt:             updatedAt,
	}
	if cancelledID != nil {
		tx.CancelledTransactionID = domain.TransactionID(*cancelledID)
	}
	if newBalanceText != nil {
		newBalance := parseMoney(t, *newBalanceText)
		tx.NewBalance = &newBalance
	}
	return tx
}

// assertTransaction compares field by field: Money and time.Time need Equal,
// and NewBalance must be nil on both sides or equal amounts.
func assertTransaction(t *testing.T, result domain.Transaction, err error, expected domain.Transaction) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != expected.ID || result.Type != expected.Type || result.Status != expected.Status ||
		result.UserID != expected.UserID || result.BetID != expected.BetID ||
		result.ProviderTransactionID != expected.ProviderTransactionID ||
		result.CancelledTransactionID != expected.CancelledTransactionID ||
		result.Currency != expected.Currency {
		t.Errorf("expected %+v, got %+v", expected, result)
	}
	if !result.Amount.Equal(expected.Amount) || !result.OldBalance.Equal(expected.OldBalance) {
		t.Errorf("expected amount/old balance %s/%s, got %s/%s",
			expected.Amount, expected.OldBalance, result.Amount, result.OldBalance)
	}
	if !equalOptionalMoney(result.NewBalance, expected.NewBalance) {
		t.Errorf("expected new balance %v, got %v", expected.NewBalance, result.NewBalance)
	}
	if !result.CreatedAt.Equal(expected.CreatedAt) || !result.UpdatedAt.Equal(expected.UpdatedAt) {
		t.Errorf("expected times %v/%v, got %v/%v",
			expected.CreatedAt, expected.UpdatedAt, result.CreatedAt, result.UpdatedAt)
	}
}

func equalOptionalMoney(a, b *domain.Money) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// completeTransaction completes the transaction in memory, one second after
// its creation, as a use case would after the Wallet applied it.
func completeTransaction(t *testing.T, tx *domain.Transaction, newBalance string) {
	t.Helper()
	if err := tx.Complete(parseMoney(t, newBalance), tx.CreatedAt.Add(time.Second)); err != nil {
		t.Fatalf("complete transaction %s: %v", tx.ID, err)
	}
}

// insertTransactionRow writes a transaction with plain SQL, independently of
// the repository under test.
func insertTransactionRow(t *testing.T, pool *pgxpool.Pool, tx domain.Transaction) {
	t.Helper()
	var cancelledID, newBalance any
	if tx.CancelledTransactionID != "" {
		cancelledID = string(tx.CancelledTransactionID)
	}
	if tx.NewBalance != nil {
		newBalance = tx.NewBalance.String()
	}
	execSQL(t, pool, `
		INSERT INTO transactions (id, type, status, wallet_user_id, bet_id,
			provider_transaction_id, cancelled_transaction_id, amount, currency,
			old_balance, new_balance, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		string(tx.ID), string(tx.Type), string(tx.Status), int64(tx.UserID), int64(tx.BetID),
		string(tx.ProviderTransactionID), cancelledID, tx.Amount.String(), string(tx.Currency),
		tx.OldBalance.String(), newBalance, tx.CreatedAt, tx.UpdatedAt)
}

// newTestDeposit builds a PENDING deposit of 25.00 that settles withdraw's bet.
func newTestDeposit(t *testing.T, pool *pgxpool.Pool, withdraw domain.Transaction) domain.Transaction {
	t.Helper()
	id := newTransactionID(t, pool)
	return domain.NewDeposit(domain.TransactionDetails{
		ID:                    id,
		UserID:                withdraw.UserID,
		BetID:                 withdraw.BetID,
		ProviderTransactionID: domain.ProviderTransactionID("deposit-" + id),
		Amount:                parseMoney(t, "25.00"),
		Currency:              withdraw.Currency,
		OldBalance:            parseMoney(t, "89.50"),
	}, testNow())
}

// markCancelled marks a completed transaction as cancelled in memory.
func markCancelled(t *testing.T, tx *domain.Transaction) {
	t.Helper()
	if err := tx.MarkCancelled(tx.UpdatedAt.Add(time.Second)); err != nil {
		t.Fatalf("mark transaction %s cancelled: %v", tx.ID, err)
	}
}

// newTestCancel builds a PENDING cancel of original.
func newTestCancel(t *testing.T, pool *pgxpool.Pool, original domain.Transaction) domain.Transaction {
	t.Helper()
	return domain.NewCancel(newTransactionID(t, pool), original, parseMoney(t, "100.00"), testNow())
}
