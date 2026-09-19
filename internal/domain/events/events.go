package events

import (
	"encoding/json"
	"errors"
	"github.com/cassianobraz/wager-service/internal/domain/domainerr"
	"github.com/cassianobraz/wager-service/internal/domain/ledger"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"time"
	"uuid"
)

const (
	TypeWagerTransactionProcessed        = "WagerTransactionProcessed"
	TypeWagerTransactionRejected         = "WagerTransactionRejected"
	TypeWalletBalanceChanged             = "WalletBalanceChanged"
	TypeWagerTransactionPendingReference = "WagerTransactionPendingReference"
)

var ErrInvalidEnvelope = errors.New("events: invalid envelope arguments")

type Envelope struct {
	EventID       uuid.UUID       `json:"eventId"`
	EventType     string          `json:"eventType"`
	AggregateID   uuid.UUID       `json:"aggregateId"`
	CorrelationID uuid.UUID       `json:"correlationId"`
	CausationID   *uuid.UUID      `json:"causationId,omitempty"`
	OccurredAt    time.Time       `json:"occurredAt"`
	Version       int             `json:"version"`
	Data          json.RawMessage `json:"data"`
}

func newEnvelope(eventType string, version int, aggregateID, correlationID uuid.UUID, causationID *uuid.UUID, occurredAt time.Time, data any) (Envelope, error) {
	if aggregateID == uuid.Nil() || correlationID == uuid.Nil() {
		return Envelope{}, ErrInvalidEnvelope
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		EventID:       uuid.NewV7(),
		EventType:     eventType,
		AggregateID:   aggregateID,
		CorrelationID: correlationID,
		CausationID:   causationID,
		OccurredAt:    occurredAt.UTC(),
		Version:       version,
		Data:          raw,
	}, nil
}

type WagerTransactionProcessedData struct {
	TransactionID         uuid.UUID   `json:"transactionId"`
	ProviderID            string      `json:"providerId,omitempty"`
	ExternalTransactionID string      `json:"externalTransactionId,omitempty"`
	WalletID              uuid.UUID   `json:"walletId"`
	PlayerID              uuid.UUID   `json:"playerId"`
	Kind                  wager.Kind  `json:"kind"`
	Money                 money.Money `json:"money"`
	ResultingBalance      money.Money `json:"resultingBalance"`
	ProcessedAt           time.Time   `json:"processedAt"`
}

func NewWagerTransactionProcessed(correlationID uuid.UUID, causationID *uuid.UUID, data WagerTransactionProcessedData, occurredAt time.Time) (Envelope, error) {
	data.ProcessedAt = data.ProcessedAt.UTC()
	return newEnvelope(TypeWagerTransactionProcessed, 1, data.TransactionID, correlationID, causationID, occurredAt, data)
}

type WagerTransactionRejectedData struct {
	TransactionID         uuid.UUID             `json:"transactionId"`
	ProviderID            string                `json:"providerId,omitempty"`
	ExternalTransactionID string                `json:"externalTransactionId,omitempty"`
	WalletID              uuid.UUID             `json:"walletId"`
	PlayerID              uuid.UUID             `json:"playerId"`
	Kind                  wager.Kind            `json:"kind"`
	FailureCode           domainerr.FailureCode `json:"failureCode"`
	RejectedAt            time.Time             `json:"rejectedAt"`
}

func NewWagerTransactionRejected(correlationID uuid.UUID, causationID *uuid.UUID, data WagerTransactionRejectedData, occurredAt time.Time) (Envelope, error) {
	data.RejectedAt = data.RejectedAt.UTC()
	return newEnvelope(TypeWagerTransactionRejected, 1, data.TransactionID, correlationID, causationID, occurredAt, data)
}

type WalletBalanceChangedData struct {
	WalletID      uuid.UUID        `json:"walletId"`
	TransactionID uuid.UUID        `json:"transactionId"`
	Direction     ledger.Direction `json:"direction"`
	Money         money.Money      `json:"money"`
	BalanceBefore money.Money      `json:"balanceBefore"`
	BalanceAfter  money.Money      `json:"balanceAfter"`
	WalletVersion int64            `json:"walletVersion"`
}

func NewWalletBalanceChanged(correlationID uuid.UUID, causationID *uuid.UUID, data WalletBalanceChangedData, occurredAt time.Time) (Envelope, error) {
	return newEnvelope(TypeWalletBalanceChanged, 1, data.WalletID, correlationID, causationID, occurredAt, data)
}

type WagerTransactionPendingReferenceData struct {
	TransactionID                  uuid.UUID `json:"transactionId"`
	ProviderID                     string    `json:"providerId"`
	ExternalTransactionID          string    `json:"externalTransactionId"`
	WalletID                       uuid.UUID `json:"walletId"`
	ReferenceExternalTransactionID string    `json:"referenceExternalTransactionId"`
	RegisteredAt                   time.Time `json:"registeredAt"`
}

func NewWagerTransactionPendingReference(correlationID uuid.UUID, causationID *uuid.UUID, data WagerTransactionPendingReferenceData, occurredAt time.Time) (Envelope, error) {
	data.RegisteredAt = data.RegisteredAt.UTC()
	return newEnvelope(TypeWagerTransactionPendingReference, 1, data.TransactionID, correlationID, causationID, occurredAt, data)
}
