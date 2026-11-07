package domain

import (
	"fmt"
	"time"
)

type TransactionType string

const (
	TransactionWithdraw TransactionType = "WITHDRAW"
	TransactionDeposit  TransactionType = "DEPOSIT"
	TransactionCancel   TransactionType = "CANCEL"
)

type TransactionStatus string

const (
	TransactionPending   TransactionStatus = "PENDING"
	TransactionCompleted TransactionStatus = "COMPLETED"
	TransactionFailed    TransactionStatus = "FAILED"
	TransactionCancelled TransactionStatus = "CANCELLED"
)

type Transaction struct {
	ID                     TransactionID
	Type                   TransactionType
	Status                 TransactionStatus
	UserID                 UserID
	BetID                  BetID
	ProviderTransactionID  ProviderTransactionID
	CancelledTransactionID TransactionID
	Amount                 Money
	Currency               Currency
	OldBalance             Money
	NewBalance             *Money
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type TransactionDetails struct {
	ID                    TransactionID
	UserID                UserID
	BetID                 BetID
	ProviderTransactionID ProviderTransactionID
	Amount                Money
	Currency              Currency
	OldBalance            Money
}

func NewWithdraw(
	details TransactionDetails,
	now time.Time,
) (Transaction, error) {
	if !details.Amount.IsPositive() {
		return Transaction{}, fmt.Errorf("%w: withdraw amount must be greater than zero", ErrInvalidAmount)
	}
	return newTransaction(TransactionWithdraw, details, now), nil
}

func NewDeposit(
	details TransactionDetails,
	now time.Time,
) Transaction {
	return newTransaction(TransactionDeposit, details, now)
}

func NewCancel(id TransactionID, original Transaction, oldBalance Money, now time.Time) Transaction {
	details := TransactionDetails{
		ID:                    id,
		UserID:                original.UserID,
		BetID:                 original.BetID,
		ProviderTransactionID: original.ProviderTransactionID,
		Amount:                original.Amount,
		Currency:              original.Currency,
		OldBalance:            oldBalance,
	}
	cancel := newTransaction(TransactionCancel, details, now)
	cancel.CancelledTransactionID = original.ID
	return cancel
}

func newTransaction(txType TransactionType, details TransactionDetails, now time.Time) Transaction {
	return Transaction{
		ID:                    details.ID,
		Type:                  txType,
		Status:                TransactionPending,
		UserID:                details.UserID,
		BetID:                 details.BetID,
		ProviderTransactionID: details.ProviderTransactionID,
		Amount:                details.Amount,
		Currency:              details.Currency,
		OldBalance:            details.OldBalance,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
}

// Complete records that the Wallet confirmed the operation
// and stores the balance it reported. Only a PENDING transaction can complete.
func (tx *Transaction) Complete(newBalance Money, now time.Time) error {
	if tx.Status != TransactionPending {
		return fmt.Errorf("%w: transaction %s is %s", ErrInvalidTransition, tx.ID, tx.Status)
	}
	tx.Status = TransactionCompleted
	tx.NewBalance = &newBalance
	tx.UpdatedAt = now
	return nil
}

// Fail records that the service established the Wallet did not apply the
// operation. Only a PENDING transaction can fail.
func (tx *Transaction) Fail(now time.Time) error {
	if tx.Status != TransactionPending {
		return fmt.Errorf("%w: transaction %s is %s", ErrInvalidTransition, tx.ID, tx.Status)
	}
	tx.Status = TransactionFailed
	tx.UpdatedAt = now
	return nil
}

// MarkCancelled records that a completed withdraw or deposit has been reverted
// by a Cancel. A CANCEL transaction cannot itself be cancelled.
func (tx *Transaction) MarkCancelled(now time.Time) error {
	if tx.Status != TransactionCompleted || tx.Type == TransactionCancel {
		return fmt.Errorf("%w: %s transaction %s is %s", ErrInvalidTransition, tx.Type, tx.ID, tx.Status)
	}
	tx.Status = TransactionCancelled
	tx.UpdatedAt = now
	return nil
}
