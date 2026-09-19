package wallet

import (
	"errors"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"time"
	"uuid"
)

var (
	ErrInvalidWallet       = errors.New("wallet: invalid wallet arguments")
	ErrCurrencyMismatch    = errors.New("wallet: movement currency does not match wallet currency")
	ErrNonPositiveAmount   = errors.New("wallet: movement amount must be strictly positive")
	ErrInsufficientBalance = errors.New("wallet: insufficient balance for debit")
)

type Wallet struct {
	id        uuid.UUID
	playerID  uuid.UUID
	balance   money.Money
	version   int64
	createdAt time.Time
	updatedAt time.Time
}

func New(id, playerID uuid.UUID, initialBalance money.Money, at time.Time) (*Wallet, error) {
	if id == uuid.Nil() || playerID == uuid.Nil() {
		return nil, ErrInvalidWallet
	}
	if initialBalance.IsNegative() {
		return nil, ErrInvalidWallet
	}
	return &Wallet{
		id:        id,
		playerID:  playerID,
		balance:   initialBalance,
		version:   1,
		createdAt: at,
		updatedAt: at,
	}, nil
}

func Rehydrate(id, playerID uuid.UUID, balance money.Money, version int64, createdAt, updatedAt time.Time) (*Wallet, error) {
	if id == uuid.Nil() || playerID == uuid.Nil() {
		return nil, ErrInvalidWallet
	}
	if version < 1 {
		return nil, ErrInvalidWallet
	}
	return &Wallet{
		id:        id,
		playerID:  playerID,
		balance:   balance,
		version:   version,
		createdAt: createdAt,
		updatedAt: updatedAt,
	}, nil
}

func (w *Wallet) ID() uuid.UUID { return w.id }

func (w *Wallet) PlayerID() uuid.UUID { return w.playerID }

func (w *Wallet) Currency() string { return w.balance.Currency() }

func (w *Wallet) Balance() money.Money { return w.balance }

func (w *Wallet) Version() int64 { return w.version }

func (w *Wallet) CreatedAt() time.Time { return w.createdAt }

func (w *Wallet) UpdatedAt() time.Time { return w.updatedAt }

type Movement struct {
	BalanceBefore money.Money
	BalanceAfter  money.Money
	WalletVersion int64
}

func (w *Wallet) Debit(amount money.Money, at time.Time) (Movement, error) {
	if amount.Currency() != w.Currency() {
		return Movement{}, ErrCurrencyMismatch
	}
	if !amount.IsPositive() {
		return Movement{}, ErrNonPositiveAmount
	}
	before := w.balance
	after, err := w.balance.Subtract(amount)
	if err != nil {
		return Movement{}, err
	}
	if after.IsNegative() {
		return Movement{}, ErrInsufficientBalance
	}
	w.balance = after
	w.version++
	w.updatedAt = at
	return Movement{BalanceBefore: before, BalanceAfter: after, WalletVersion: w.version}, nil
}

func (w *Wallet) Credit(amount money.Money, at time.Time) (Movement, error) {
	if amount.Currency() != w.Currency() {
		return Movement{}, ErrCurrencyMismatch
	}
	if !amount.IsPositive() {
		return Movement{}, ErrNonPositiveAmount
	}
	before := w.balance
	after, err := w.balance.Add(amount)
	if err != nil {
		return Movement{}, err
	}
	w.balance = after
	w.version++
	w.updatedAt = at
	return Movement{BalanceBefore: before, BalanceAfter: after, WalletVersion: w.version}, nil
}

func (w *Wallet) CanDebit(amount money.Money) bool {
	if amount.Currency() != w.Currency() || !amount.IsPositive() {
		return false
	}
	after, err := w.balance.Subtract(amount)
	if err != nil {
		return false
	}
	return !after.IsNegative()
}
