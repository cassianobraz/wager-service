package ledger

import (
	"errors"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"time"
	"uuid"
)

type Direction string

const (
	Debit  Direction = "DEBIT"
	Credit Direction = "CREDIT"
)

func (d Direction) IsValid() bool {
	return d == Debit || d == Credit
}

var (
	ErrInvalidEntry    = errors.New("ledger: invalid entry arguments")
	ErrBalanceMismatch = errors.New("ledger: balanceAfter does not equal balanceBefore adjusted by money in the given direction")
)

type Entry struct {
	id            uuid.UUID
	walletID      uuid.UUID
	transactionID uuid.UUID
	direction     Direction
	money         money.Money
	balanceBefore money.Money
	balanceAfter  money.Money
	createdAt     time.Time
}

func New(id, walletID, transactionID uuid.UUID, direction Direction, amount, balanceBefore, balanceAfter money.Money, at time.Time) (*Entry, error) {
	if id == uuid.Nil() || walletID == uuid.Nil() || transactionID == uuid.Nil() {
		return nil, ErrInvalidEntry
	}
	if !direction.IsValid() {
		return nil, ErrInvalidEntry
	}
	if !amount.IsPositive() {
		return nil, ErrInvalidEntry
	}

	var expected money.Money
	var err error
	switch direction {
	case Debit:
		expected, err = balanceBefore.Subtract(amount)
	case Credit:
		expected, err = balanceBefore.Add(amount)
	}
	if err != nil {
		return nil, err
	}
	if !expected.Equals(balanceAfter) {
		return nil, ErrBalanceMismatch
	}

	return &Entry{
		id:            id,
		walletID:      walletID,
		transactionID: transactionID,
		direction:     direction,
		money:         amount,
		balanceBefore: balanceBefore,
		balanceAfter:  balanceAfter,
		createdAt:     at,
	}, nil
}

func Rehydrate(id, walletID, transactionID uuid.UUID, direction Direction, amount, balanceBefore, balanceAfter money.Money, at time.Time) (*Entry, error) {
	if id == uuid.Nil() || walletID == uuid.Nil() || transactionID == uuid.Nil() || !direction.IsValid() {
		return nil, ErrInvalidEntry
	}
	return &Entry{
		id:            id,
		walletID:      walletID,
		transactionID: transactionID,
		direction:     direction,
		money:         amount,
		balanceBefore: balanceBefore,
		balanceAfter:  balanceAfter,
		createdAt:     at,
	}, nil
}

func (e *Entry) ID() uuid.UUID              { return e.id }
func (e *Entry) WalletID() uuid.UUID        { return e.walletID }
func (e *Entry) TransactionID() uuid.UUID   { return e.transactionID }
func (e *Entry) Direction() Direction       { return e.direction }
func (e *Entry) Money() money.Money         { return e.money }
func (e *Entry) BalanceBefore() money.Money { return e.balanceBefore }
func (e *Entry) BalanceAfter() money.Money  { return e.balanceAfter }
func (e *Entry) CreatedAt() time.Time       { return e.createdAt }

func (e *Entry) SignedAmount() (money.Money, error) {
	if e.direction == Debit {
		return e.money.Negate()
	}
	return e.money, nil
}
