package domain

import (
	"testing"
	"time"
)

// Each test covers one row of the bet status table (assessment §6.1) or one
// transition that the table does not allow.

func TestNewPlacedBet_IsPlaced(t *testing.T) {
	result := givenAPlacedBet(t)

	assertBetStatus(t, result, BetPlaced)
}

func TestNewPlacedBet_SetsBothTimestampsToNow(t *testing.T) {
	result := givenAPlacedBet(t)

	assertTimestamps(t, result, testNow, testNow)
}

// ----- Settle (deposit completed) -----

func TestBet_SettlePositiveAmount_IsWon(t *testing.T) {
	result := givenAPlacedBet(t)

	err := result.Settle(givenAParsedMoney(t, "25.00"), testLater)

	assertTransitionTo(t, result, err, BetWon)
}

func TestBet_SettleZeroAmount_IsLost(t *testing.T) {
	result := givenAPlacedBet(t)

	err := result.Settle(givenAParsedMoney(t, "0"), testLater)

	assertTransitionTo(t, result, err, BetLost)
}

func TestBet_Settle_UpdatesTimestamp(t *testing.T) {
	result := givenAPlacedBet(t)

	_ = result.Settle(givenAParsedMoney(t, "25.00"), testLater)

	assertTimestamps(t, result, testNow, testLater)
}

func TestBet_SettleWonBet_ReturnsBetAlreadySettled(t *testing.T) {
	result := givenAWonBet(t)

	err := result.Settle(givenAParsedMoney(t, "25.00"), testLater)

	assertErrorIs(t, err, ErrBetAlreadySettled)
}

func TestBet_SettleLostBet_ReturnsBetAlreadySettled(t *testing.T) {
	result := givenALostBet(t)

	err := result.Settle(givenAParsedMoney(t, "25.00"), testLater)

	assertErrorIs(t, err, ErrBetAlreadySettled)
}

func TestBet_SettleCancelledBet_ReturnsBetAlreadySettled(t *testing.T) {
	result := givenACancelledBet(t)

	err := result.Settle(givenAParsedMoney(t, "25.00"), testLater)

	assertErrorIs(t, err, ErrBetAlreadySettled)
}

// ----- Cancel (cancel of the withdraw) -----

func TestBet_CancelPlacedBet_IsCancelled(t *testing.T) {
	result := givenAPlacedBet(t)

	err := result.Cancel(testLater)

	assertTransitionTo(t, result, err, BetCancelled)
}

func TestBet_CancelWonBet_ReturnsBetAlreadySettled(t *testing.T) {
	result := givenAWonBet(t)

	err := result.Cancel(testLater)

	assertErrorIs(t, err, ErrBetAlreadySettled)
}

func TestBet_CancelLostBet_ReturnsBetAlreadySettled(t *testing.T) {
	result := givenALostBet(t)

	err := result.Cancel(testLater)

	assertErrorIs(t, err, ErrBetAlreadySettled)
}

func TestBet_CancelCancelledBet_ReturnsInvalidTransition(t *testing.T) {
	result := givenACancelledBet(t)

	err := result.Cancel(testLater)

	assertErrorIs(t, err, ErrInvalidTransition)
}

// ----- RevertSettlement (cancel of the deposit) -----

func TestBet_RevertWonSettlement_IsPlaced(t *testing.T) {
	result := givenAWonBet(t)

	err := result.RevertSettlement(testLater)

	assertTransitionTo(t, result, err, BetPlaced)
}

func TestBet_RevertLostSettlement_IsPlaced(t *testing.T) {
	result := givenALostBet(t)

	err := result.RevertSettlement(testLater)

	assertTransitionTo(t, result, err, BetPlaced)
}

func TestBet_RevertPlacedBet_ReturnsBetNotSettled(t *testing.T) {
	result := givenAPlacedBet(t)

	err := result.RevertSettlement(testLater)

	assertErrorIs(t, err, ErrBetNotSettled)
}

func TestBet_RevertCancelledBet_ReturnsBetNotSettled(t *testing.T) {
	result := givenACancelledBet(t)

	err := result.RevertSettlement(testLater)

	assertErrorIs(t, err, ErrBetNotSettled)
}

// ----- A refused transition changes nothing -----

func TestBet_RefusedTransition_KeepsStatus(t *testing.T) {
	result := givenAWonBet(t)

	_ = result.Cancel(testLater)

	assertBetStatus(t, result, BetWon)
}

func TestBet_RefusedTransition_KeepsTimestamp(t *testing.T) {
	result := givenAPlacedBet(t)

	_ = result.RevertSettlement(testLater)

	assertTimestamps(t, result, testNow, testNow)
}

/////////////////////////////////////////////////////////////////////////////////////

// Fixed times, so tests never depend on the real clock.
var (
	testNow   = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	testLater = testNow.Add(time.Minute)
)

func givenAPlacedBet(t *testing.T) *Bet {
	t.Helper()
	bet := NewPlacedBet(BetID(1), UserID(34633089486), USD, givenAParsedMoney(t, "10.50"), testNow)
	return &bet
}

func givenAWonBet(t *testing.T) *Bet {
	t.Helper()
	bet := givenAPlacedBet(t)
	givenTransitionSucceeds(t, bet.Settle(givenAParsedMoney(t, "25.00"), testNow), BetWon)
	return bet
}

func givenALostBet(t *testing.T) *Bet {
	t.Helper()
	bet := givenAPlacedBet(t)
	givenTransitionSucceeds(t, bet.Settle(givenAParsedMoney(t, "0"), testNow), BetLost)
	return bet
}

func givenACancelledBet(t *testing.T) *Bet {
	t.Helper()
	bet := givenAPlacedBet(t)
	givenTransitionSucceeds(t, bet.Cancel(testNow), BetCancelled)
	return bet
}

// givenTransitionSucceeds stops the test if the Arrange step itself failed:
// that is broken test data, not the behaviour under test.
func givenTransitionSucceeds(t *testing.T, err error, target BetStatus) {
	t.Helper()
	if err != nil {
		t.Fatalf("arrange: could not move bet to %s: %v", target, err)
	}
}

// assertTransitionTo checks that a transition succeeded and left the bet in
// the expected status.
func assertTransitionTo(t *testing.T, result *Bet, err error, expected BetStatus) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertBetStatus(t, result, expected)
}

func assertBetStatus(t *testing.T, result *Bet, expected BetStatus) {
	t.Helper()
	if result.Status != expected {
		t.Errorf("expected bet status %q, got %q", expected, result.Status)
	}
}

func assertTimestamps(t *testing.T, result *Bet, createdAt, updatedAt time.Time) {
	t.Helper()
	if !result.CreatedAt.Equal(createdAt) || !result.UpdatedAt.Equal(updatedAt) {
		t.Errorf("expected CreatedAt=%v UpdatedAt=%v, got CreatedAt=%v UpdatedAt=%v",
			createdAt, updatedAt, result.CreatedAt, result.UpdatedAt)
	}
}
