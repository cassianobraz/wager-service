package wager

import (
	"errors"
	"github.com/cassianobraz/wager-service/internal/domain/domainerr"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"time"
	"uuid"
)

type Kind string

const (
	Opening  Kind = "OPENING"
	Bet      Kind = "BET"
	Win      Kind = "WIN"
	Loss     Kind = "LOSS"
	Refund   Kind = "REFUND"
	Rollback Kind = "ROLLBACK"
)

func (k Kind) IsExternal() bool {
	switch k {
	case Bet, Win, Loss, Refund, Rollback:
		return true
	default:
		return false
	}
}

func (k Kind) RequiresReference() bool {
	return k == Refund || k == Rollback
}

func (k Kind) MayCarryReference() bool {
	return k == Refund || k == Rollback || k == Win
}

type Status string

const (
	Pending          Status = "PENDING"
	PendingReference Status = "PENDING_REFERENCE"
	Processed        Status = "PROCESSED"
	Rejected         Status = "REJECTED"
	Failed           Status = "FAILED"
)

func (s Status) IsTerminal() bool {
	return s == Processed || s == Rejected || s == Failed
}

var (
	ErrInvalidTransaction      = errors.New("wager: invalid transaction arguments")
	ErrInvalidKindForOperation = errors.New("wager: OPENING may not be submitted as an external operation")
)

type ExternalParams struct {
	ID                             uuid.UUID
	ProviderID                     string
	ExternalTransactionID          string
	IdempotencyKey                 string
	PayloadHash                    string
	WalletID                       uuid.UUID
	PlayerID                       uuid.UUID
	RoundID                        string
	GameID                         string
	Kind                           Kind
	Money                          money.Money
	ReferenceExternalTransactionID string
}

type Transaction struct {
	id                             uuid.UUID
	providerID                     string
	externalTransactionID          string
	idempotencyKey                 string
	payloadHash                    string
	walletID                       uuid.UUID
	playerID                       uuid.UUID
	roundID                        string
	gameID                         string
	kind                           Kind
	money                          money.Money
	referenceExternalTransactionID string
	resolvedReferenceTransactionID *uuid.UUID
	status                         Status
	failureCode                    domainerr.FailureCode
	resultingBalance               *money.Money
	createdAt                      time.Time
	updatedAt                      time.Time
}

func NewExternal(p ExternalParams, at time.Time) (*Transaction, error) {
	if !p.Kind.IsExternal() {
		return nil, ErrInvalidKindForOperation
	}
	if p.ID == uuid.Nil() || p.WalletID == uuid.Nil() || p.PlayerID == uuid.Nil() {
		return nil, ErrInvalidTransaction
	}
	if p.ProviderID == "" || p.ExternalTransactionID == "" || p.IdempotencyKey == "" || p.PayloadHash == "" {
		return nil, ErrInvalidTransaction
	}
	if p.RoundID == "" || p.GameID == "" {
		return nil, ErrInvalidTransaction
	}
	if p.Kind.RequiresReference() && p.ReferenceExternalTransactionID == "" {
		return nil, ErrInvalidTransaction
	}
	if !p.Kind.MayCarryReference() && p.ReferenceExternalTransactionID != "" {
		return nil, ErrInvalidTransaction
	}
	if err := validateAmountForKind(p.Kind, p.Money); err != nil {
		return nil, err
	}

	return &Transaction{
		id:                             p.ID,
		providerID:                     p.ProviderID,
		externalTransactionID:          p.ExternalTransactionID,
		idempotencyKey:                 p.IdempotencyKey,
		payloadHash:                    p.PayloadHash,
		walletID:                       p.WalletID,
		playerID:                       p.PlayerID,
		roundID:                        p.RoundID,
		gameID:                         p.GameID,
		kind:                           p.Kind,
		money:                          p.Money,
		referenceExternalTransactionID: p.ReferenceExternalTransactionID,
		status:                         Pending,
		createdAt:                      at,
		updatedAt:                      at,
	}, nil
}

func validateAmountForKind(kind Kind, m money.Money) error {
	switch kind {
	case Loss:
		if !m.IsZero() {
			return ErrInvalidTransaction
		}
	case Bet, Win, Refund, Rollback, Opening:
		if !m.IsPositive() {
			return ErrInvalidTransaction
		}
	default:
		return ErrInvalidTransaction
	}
	return nil
}

func NewOpening(id, walletID, playerID uuid.UUID, amount money.Money, at time.Time) (*Transaction, error) {
	if id == uuid.Nil() || walletID == uuid.Nil() || playerID == uuid.Nil() {
		return nil, ErrInvalidTransaction
	}
	if err := validateAmountForKind(Opening, amount); err != nil {
		return nil, err
	}
	balance := amount
	return &Transaction{
		id:               id,
		walletID:         walletID,
		playerID:         playerID,
		kind:             Opening,
		money:            amount,
		status:           Processed,
		resultingBalance: &balance,
		createdAt:        at,
		updatedAt:        at,
	}, nil
}

type RehydrateParams struct {
	ID                             uuid.UUID
	ProviderID                     string
	ExternalTransactionID          string
	IdempotencyKey                 string
	PayloadHash                    string
	WalletID                       uuid.UUID
	PlayerID                       uuid.UUID
	RoundID                        string
	GameID                         string
	Kind                           Kind
	Money                          money.Money
	ReferenceExternalTransactionID string
	ResolvedReferenceTransactionID *uuid.UUID
	Status                         Status
	FailureCode                    domainerr.FailureCode
	ResultingBalance               *money.Money
	CreatedAt                      time.Time
	UpdatedAt                      time.Time
}

func Rehydrate(p RehydrateParams) (*Transaction, error) {
	if p.ID == uuid.Nil() || p.WalletID == uuid.Nil() || p.PlayerID == uuid.Nil() {
		return nil, ErrInvalidTransaction
	}
	return &Transaction{
		id:                             p.ID,
		providerID:                     p.ProviderID,
		externalTransactionID:          p.ExternalTransactionID,
		idempotencyKey:                 p.IdempotencyKey,
		payloadHash:                    p.PayloadHash,
		walletID:                       p.WalletID,
		playerID:                       p.PlayerID,
		roundID:                        p.RoundID,
		gameID:                         p.GameID,
		kind:                           p.Kind,
		money:                          p.Money,
		referenceExternalTransactionID: p.ReferenceExternalTransactionID,
		resolvedReferenceTransactionID: p.ResolvedReferenceTransactionID,
		status:                         p.Status,
		failureCode:                    p.FailureCode,
		resultingBalance:               p.ResultingBalance,
		createdAt:                      p.CreatedAt,
		updatedAt:                      p.UpdatedAt,
	}, nil
}

func (t *Transaction) ID() uuid.UUID                 { return t.id }
func (t *Transaction) ProviderID() string            { return t.providerID }
func (t *Transaction) ExternalTransactionID() string { return t.externalTransactionID }
func (t *Transaction) IdempotencyKey() string        { return t.idempotencyKey }
func (t *Transaction) PayloadHash() string           { return t.payloadHash }
func (t *Transaction) WalletID() uuid.UUID           { return t.walletID }
func (t *Transaction) PlayerID() uuid.UUID           { return t.playerID }
func (t *Transaction) RoundID() string               { return t.roundID }
func (t *Transaction) GameID() string                { return t.gameID }
func (t *Transaction) Kind() Kind                    { return t.kind }
func (t *Transaction) Money() money.Money            { return t.money }
func (t *Transaction) ReferenceExternalTransactionID() string {
	return t.referenceExternalTransactionID
}
func (t *Transaction) ResolvedReferenceTransactionID() *uuid.UUID {
	return t.resolvedReferenceTransactionID
}
func (t *Transaction) Status() Status                     { return t.status }
func (t *Transaction) FailureCode() domainerr.FailureCode { return t.failureCode }
func (t *Transaction) ResultingBalance() *money.Money     { return t.resultingBalance }
func (t *Transaction) CreatedAt() time.Time               { return t.createdAt }
func (t *Transaction) UpdatedAt() time.Time               { return t.updatedAt }

func (t *Transaction) AttachResolvedReference(id uuid.UUID) error {
	if t.status.IsTerminal() {
		return domainerr.ErrInvalidTransition
	}
	if id == uuid.Nil() {
		return ErrInvalidTransaction
	}
	t.resolvedReferenceTransactionID = &id
	return nil
}

func (t *Transaction) MarkPendingReference(at time.Time) error {
	if t.status != Pending {
		return domainerr.ErrInvalidTransition
	}
	t.status = PendingReference
	t.updatedAt = at
	return nil
}

func (t *Transaction) MarkProcessed(resultingBalance money.Money, at time.Time) error {
	if t.status.IsTerminal() {
		return domainerr.ErrInvalidTransition
	}
	t.status = Processed
	t.resultingBalance = &resultingBalance
	t.updatedAt = at
	return nil
}

func (t *Transaction) MarkRejected(code domainerr.FailureCode, at time.Time) error {
	if t.status.IsTerminal() {
		return domainerr.ErrInvalidTransition
	}
	if code == "" {
		return ErrInvalidTransaction
	}
	t.status = Rejected
	t.failureCode = code
	t.updatedAt = at
	return nil
}

func (t *Transaction) MarkFailed(code domainerr.FailureCode, at time.Time) error {
	if t.status.IsTerminal() {
		return domainerr.ErrInvalidTransition
	}
	if code == "" {
		return ErrInvalidTransaction
	}
	t.status = Failed
	t.failureCode = code
	t.updatedAt = at
	return nil
}
