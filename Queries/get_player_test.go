package queries

import (
	"context"
	"errors"
	"slices"
	"testing"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
	"github.com/aaronhdez/kt_game-integration-api/Domain/domaintest"
)

const testUserID domain.UserID = 34616761765 // player_kes

func TestGetPlayer_KnownPlayer_ReturnsTheWalletBalance(t *testing.T) {
	fixture := givenAGetPlayer(t)

	result, err := fixture.getPlayer.Execute(context.Background(), GetPlayerQuery{UserID: testUserID})

	assertPlayerResult(t, result, err, PlayerResult{
		UserID:   testUserID,
		Balance:  givenAParsedMoney(t, "750.50"),
		Currency: domain.KES,
	})
}

func TestGetPlayer_KnownPlayer_AsksTheWalletForThatPlayer(t *testing.T) {
	fixture := givenAGetPlayer(t)

	fixture.getPlayer.Execute(context.Background(), GetPlayerQuery{UserID: testUserID})

	assertBalanceReadsFor(t, fixture.wallet, []domain.UserID{testUserID})
}

// §8.2: the balance is read at request time, never served from a cache.
func TestGetPlayer_CalledTwice_ReadsTheWalletTwice(t *testing.T) {
	fixture := givenAGetPlayer(t)

	fixture.getPlayer.Execute(context.Background(), GetPlayerQuery{UserID: testUserID})
	fixture.getPlayer.Execute(context.Background(), GetPlayerQuery{UserID: testUserID})

	assertBalanceReadsFor(t, fixture.wallet, []domain.UserID{testUserID, testUserID})
}

func TestGetPlayer_UnknownPlayer_IsPlayerNotFound(t *testing.T) {
	fixture := givenAGetPlayer(t)

	_, err := fixture.getPlayer.Execute(context.Background(), GetPlayerQuery{UserID: 1})

	assertErrorIs(t, err, domain.ErrPlayerNotFound)
}

func TestGetPlayer_UnknownPlayer_DoesNotCallTheWallet(t *testing.T) {
	fixture := givenAGetPlayer(t)

	fixture.getPlayer.Execute(context.Background(), GetPlayerQuery{UserID: 1})

	assertBalanceReadsFor(t, fixture.wallet, []domain.UserID{})
}

// A database failure keeps its cause (and becomes a 500); it is never
// reported as "player not found".
func TestGetPlayer_RepositoryFails_ReturnsItsError(t *testing.T) {
	fixture := givenAGetPlayer(t)
	fixture.players.Err = errors.New("database down")

	_, err := fixture.getPlayer.Execute(context.Background(), GetPlayerQuery{UserID: testUserID})

	assertErrorIs(t, err, fixture.players.Err)
}

func TestGetPlayer_RepositoryFails_DoesNotCallTheWallet(t *testing.T) {
	fixture := givenAGetPlayer(t)
	fixture.players.Err = errors.New("database down")

	fixture.getPlayer.Execute(context.Background(), GetPlayerQuery{UserID: testUserID})

	assertBalanceReadsFor(t, fixture.wallet, []domain.UserID{})
}

func TestGetPlayer_WalletFails_IsWalletFailure(t *testing.T) {
	fixture := givenAGetPlayer(t)
	fixture.wallet.Next(domaintest.Reject)

	_, err := fixture.getPlayer.Execute(context.Background(), GetPlayerQuery{UserID: testUserID})

	assertErrorIs(t, err, domain.ErrWalletFailure)
}

func TestGetPlayer_WalletTimesOut_IsWalletTimeout(t *testing.T) {
	fixture := givenAGetPlayer(t)
	fixture.wallet.Next(domaintest.TimeOut)

	_, err := fixture.getPlayer.Execute(context.Background(), GetPlayerQuery{UserID: testUserID})

	assertErrorIs(t, err, domain.ErrWalletTimeout)
}

// Our account says KES but the Wallet says USD: showing that balance under
// either currency would be wrong, so it is a Wallet error (502).
func TestGetPlayer_WalletCurrencyDiffersFromThePlayers_IsWalletFailure(t *testing.T) {
	fixture := givenAGetPlayerWithWalletCurrency(t, domain.USD)

	_, err := fixture.getPlayer.Execute(context.Background(), GetPlayerQuery{UserID: testUserID})

	assertErrorIs(t, err, domain.ErrWalletFailure)
}

/////////////////////////////////////////////////////////////////////////////////////

// getPlayerFixture keeps the fakes reachable, so a test can make one of them
// fail or inspect what it received.
type getPlayerFixture struct {
	getPlayer *GetPlayerQueryHandler
	players   *domaintest.FakePlayerRepository
	wallet    *domaintest.FakeWallet
}

// givenAGetPlayer builds the query with player_kes in the database and a
// KES 750.50 balance in the Wallet.
func givenAGetPlayer(t *testing.T) getPlayerFixture {
	t.Helper()
	return givenAGetPlayerWithWalletCurrency(t, domain.KES)
}

// givenAGetPlayerWithWalletCurrency is givenAGetPlayer with the currency the
// Wallet reports; the player's account currency stays KES.
func givenAGetPlayerWithWalletCurrency(t *testing.T, walletCurrency domain.Currency) getPlayerFixture {
	t.Helper()
	players := domaintest.NewFakePlayerRepository(domain.Player{
		UserID:   testUserID,
		Username: "player_kes",
		Currency: domain.KES,
	})
	wallet := domaintest.NewFakeWallet(map[domain.UserID]domain.WalletBalance{
		testUserID: {Amount: givenAParsedMoney(t, "750.50"), Currency: walletCurrency},
	})

	return getPlayerFixture{
		getPlayer: NewGetPlayerQueryHandler(players, wallet),
		players:   players,
		wallet:    wallet,
	}
}

func givenAParsedMoney(t *testing.T, value string) domain.Money {
	t.Helper()
	money, err := domain.ParseMoney(value)
	if err != nil {
		t.Fatalf("invalid test money %q: %v", value, err)
	}
	return money
}

// assertPlayerResult compares field by field: Money is compared with Equal,
// not ==.
func assertPlayerResult(t *testing.T, result PlayerResult, err error, want PlayerResult) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.UserID != want.UserID || !result.Balance.Equal(want.Balance) || result.Currency != want.Currency {
		t.Errorf("expected %+v, got %+v", want, result)
	}
}

// assertBalanceReadsFor checks which players the Wallet was asked about, in
// order, and that nothing but balance reads happened.
func assertBalanceReadsFor(t *testing.T, wallet *domaintest.FakeWallet, want []domain.UserID) {
	t.Helper()
	result := []domain.UserID{}
	for _, call := range wallet.Calls() {
		if call.Method != "Balance" {
			t.Fatalf("unexpected wallet call %s", call.Method)
		}
		result = append(result, call.UserID)
	}
	if !slices.Equal(result, want) {
		t.Errorf("expected balance reads for %v, got %v", want, result)
	}
}

func assertErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("expected %v to be %q", err, target)
	}
}
