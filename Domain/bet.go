package domain

import (
	"fmt"
	"time"
)

type BetStatus string

const (
	BetPlaced    BetStatus = "PLACED"
	BetWon       BetStatus = "WON"
	BetLost      BetStatus = "LOST"
	BetCancelled BetStatus = "CANCELLED"
)

// Bet represents a wager placed by a user, including its status and timestamps.
type Bet struct {
	ID        BetID
	UserID    UserID
	Currency  Currency
	Stake     Money // lo apostado (el importe del retiro)
	Status    BetStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewPlacedBet(id BetID, userID UserID, currency Currency, stake Money, now time.Time) Bet {
	return Bet{
		ID:        id,
		UserID:    userID,
		Currency:  currency,
		Stake:     stake,
		Status:    BetPlaced,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Settle records the deposit that settles the bet: an amount greater
// than zero makes it WON, zero makes it LOST. Only a PLACED bet can be settled;
// a bet that is already settled or cancelled has its single settlement.
func (b *Bet) Settle(amount Money, now time.Time) error {
	if b.Status != BetPlaced {
		return fmt.Errorf("%w: bet %d is %s", ErrBetAlreadySettled, b.ID, b.Status)
	}
	if amount.IsPositive() {
		b.Status = BetWon
	} else {
		b.Status = BetLost
	}
	b.UpdatedAt = now
	return nil
}

// Cancel records the cancel of the bet's withdraw: a PLACED bet becomes
// CANCELLED. A settled bet cannot be cancelled until its deposit is cancelled.
func (b *Bet) Cancel(now time.Time) error {
	switch b.Status {
	case BetPlaced:
		b.Status = BetCancelled
		b.UpdatedAt = now
		return nil
	case BetWon, BetLost:
		return fmt.Errorf("%w: cancel the deposit of bet %d first", ErrBetAlreadySettled, b.ID)
	default:
		return fmt.Errorf("%w: bet %d is %s", ErrInvalidTransition, b.ID, b.Status)
	}
}

// RevertSettlement records the cancel of the bet's deposit: a WON or
// LOST bet returns to PLACED.
func (b *Bet) RevertSettlement(now time.Time) error {
	if b.Status != BetWon && b.Status != BetLost {
		return fmt.Errorf("%w: bet %d is %s", ErrBetNotSettled, b.ID, b.Status)
	}
	b.Status = BetPlaced
	b.UpdatedAt = now
	return nil
}
