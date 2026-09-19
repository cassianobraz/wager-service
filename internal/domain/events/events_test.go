package events_test

import (
	"encoding/json"
	"errors"
	"github.com/cassianobraz/wager-service/internal/domain/domainerr"
	"github.com/cassianobraz/wager-service/internal/domain/events"
	"github.com/cassianobraz/wager-service/internal/domain/ledger"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"testing"
	"time"
	"uuid"
)

var fixedTime = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

func mustMoney(t *testing.T, amount string) money.Money {
	t.Helper()
	m, err := money.Parse(amount, "BRL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return m
}

func TestNewWagerTransactionProcessed(t *testing.T) {
	txID, walletID, playerID, correlationID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	env, err := events.NewWagerTransactionProcessed(correlationID, nil, events.WagerTransactionProcessedData{
		TransactionID:    txID,
		WalletID:         walletID,
		PlayerID:         playerID,
		Kind:             wager.Bet,
		Money:            mustMoney(t, "25.00"),
		ResultingBalance: mustMoney(t, "975.00"),
	}, fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.EventType != events.TypeWagerTransactionProcessed {
		t.Fatalf("EventType = %s, want %s", env.EventType, events.TypeWagerTransactionProcessed)
	}
	if env.AggregateID != txID {
		t.Fatal("AggregateID must be the transaction's own ID")
	}
	if env.Version != 1 {
		t.Fatalf("Version = %d, want 1", env.Version)
	}
	if env.EventID == uuid.Nil() {
		t.Fatal("EventID must be generated, not nil")
	}
	if env.OccurredAt.Location() != time.UTC {
		t.Fatal("OccurredAt must be normalized to UTC")
	}

	var decoded events.WagerTransactionProcessedData
	if err := json.Unmarshal(env.Data, &decoded); err != nil {
		t.Fatalf("unexpected error unmarshaling Data: %v", err)
	}
	if decoded.Money.String() != "25.00" || decoded.ResultingBalance.String() != "975.00" {
		t.Fatal("payload money values not preserved")
	}
}

func TestNewWagerTransactionProcessed_RejectsNilIDs(t *testing.T) {
	data := events.WagerTransactionProcessedData{
		TransactionID: uuid.Nil(),
		WalletID:      uuid.NewV7(),
		PlayerID:      uuid.NewV7(),
		Kind:          wager.Bet,
		Money:         mustMoney(t, "25.00"),
	}
	if _, err := events.NewWagerTransactionProcessed(uuid.NewV7(), nil, data, fixedTime); !errors.Is(err, events.ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope, got %v", err)
	}

	data.TransactionID = uuid.NewV7()
	if _, err := events.NewWagerTransactionProcessed(uuid.Nil(), nil, data, fixedTime); !errors.Is(err, events.ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope for nil correlationID, got %v", err)
	}
}

func TestNewWagerTransactionRejected(t *testing.T) {
	txID, walletID, playerID, correlationID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	env, err := events.NewWagerTransactionRejected(correlationID, nil, events.WagerTransactionRejectedData{
		TransactionID: txID,
		WalletID:      walletID,
		PlayerID:      playerID,
		Kind:          wager.Bet,
		FailureCode:   domainerr.FailureInsufficientFunds,
	}, fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.EventType != events.TypeWagerTransactionRejected {
		t.Fatalf("EventType = %s, want %s", env.EventType, events.TypeWagerTransactionRejected)
	}

	var decoded events.WagerTransactionRejectedData
	if err := json.Unmarshal(env.Data, &decoded); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decoded.FailureCode != domainerr.FailureInsufficientFunds {
		t.Fatal("failureCode not preserved")
	}
}

func TestNewWalletBalanceChanged(t *testing.T) {
	walletID, txID, correlationID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	causationID := uuid.NewV7()
	env, err := events.NewWalletBalanceChanged(correlationID, &causationID, events.WalletBalanceChangedData{
		WalletID:      walletID,
		TransactionID: txID,
		Direction:     ledger.Debit,
		Money:         mustMoney(t, "25.00"),
		BalanceBefore: mustMoney(t, "1000.00"),
		BalanceAfter:  mustMoney(t, "975.00"),
		WalletVersion: 2,
	}, fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.AggregateID != walletID {
		t.Fatal("AggregateID must be the wallet's own ID")
	}
	if env.CausationID == nil || *env.CausationID != causationID {
		t.Fatal("CausationID not preserved")
	}

	var decoded events.WalletBalanceChangedData
	if err := json.Unmarshal(env.Data, &decoded); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decoded.WalletVersion != 2 || decoded.Direction != ledger.Debit {
		t.Fatal("payload fields not preserved")
	}
}

func TestNewWagerTransactionPendingReference(t *testing.T) {
	txID, walletID, correlationID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	env, err := events.NewWagerTransactionPendingReference(correlationID, nil, events.WagerTransactionPendingReferenceData{
		TransactionID:                  txID,
		ProviderID:                     "provider-a",
		ExternalTransactionID:          "transaction-123",
		WalletID:                       walletID,
		ReferenceExternalTransactionID: "transaction-100",
	}, fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.EventType != events.TypeWagerTransactionPendingReference {
		t.Fatalf("EventType = %s, want %s", env.EventType, events.TypeWagerTransactionPendingReference)
	}

	var decoded events.WagerTransactionPendingReferenceData
	if err := json.Unmarshal(env.Data, &decoded); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decoded.ReferenceExternalTransactionID != "transaction-100" {
		t.Fatal("referenceExternalTransactionId not preserved")
	}
}

func TestEnvelope_JSONRoundTrip(t *testing.T) {
	txID, walletID, playerID, correlationID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	env, err := events.NewWagerTransactionProcessed(correlationID, nil, events.WagerTransactionProcessedData{
		TransactionID:    txID,
		WalletID:         walletID,
		PlayerID:         playerID,
		Kind:             wager.Bet,
		Money:            mustMoney(t, "25.00"),
		ResultingBalance: mustMoney(t, "975.00"),
	}, fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var decoded events.Envelope
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.EventID != env.EventID || decoded.EventType != env.EventType {
		t.Fatal("envelope round trip mismatch")
	}
}
