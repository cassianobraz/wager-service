package ledger_test

import (
	"errors"
	"github.com/cassianobraz/wager-service/internal/domain/ledger"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"testing"
	"time"
	"uuid"
)

var fixedTime = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

type Direction = ledger.Direction

func mustMoney(t *testing.T, amount string) money.Money {
	t.Helper()
	m, err := money.Parse(amount, "BRL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return m
}

func TestNew_ValidDebitEntry(t *testing.T) {
	id, walletID, txID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	e, err := ledger.New(id, walletID, txID, ledger.Debit, mustMoney(t, "25.00"), mustMoney(t, "100.00"), mustMoney(t, "75.00"), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Direction() != ledger.Debit {
		t.Fatal("direction not preserved")
	}
	if e.BalanceAfter().String() != "75.00" {
		t.Fatalf("balanceAfter = %s, want 75.00", e.BalanceAfter())
	}
}

func TestNew_ValidCreditEntry(t *testing.T) {
	e, err := ledger.New(uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), ledger.Credit, mustMoney(t, "25.00"), mustMoney(t, "100.00"), mustMoney(t, "125.00"), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.BalanceAfter().String() != "125.00" {
		t.Fatalf("balanceAfter = %s, want 125.00", e.BalanceAfter())
	}
}

func TestNew_RejectsBalanceMismatch(t *testing.T) {
	_, err := ledger.New(uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), ledger.Debit, mustMoney(t, "25.00"), mustMoney(t, "100.00"), mustMoney(t, "80.00"), fixedTime)
	if !errors.Is(err, ledger.ErrBalanceMismatch) {
		t.Fatalf("expected ErrBalanceMismatch, got %v", err)
	}
}

func TestNew_RejectsInvalidDirection(t *testing.T) {
	_, err := ledger.New(uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), Direction("SIDEWAYS"), mustMoney(t, "25.00"), mustMoney(t, "100.00"), mustMoney(t, "75.00"), fixedTime)
	if !errors.Is(err, ledger.ErrInvalidEntry) {
		t.Fatalf("expected ErrInvalidEntry, got %v", err)
	}
}

func TestNew_RejectsNonPositiveAmount(t *testing.T) {
	_, err := ledger.New(uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), ledger.Credit, mustMoney(t, "0.00"), mustMoney(t, "100.00"), mustMoney(t, "100.00"), fixedTime)
	if !errors.Is(err, ledger.ErrInvalidEntry) {
		t.Fatalf("expected ErrInvalidEntry, got %v", err)
	}
}

func TestNew_RejectsNilIdentities(t *testing.T) {
	amount, before, after := mustMoney(t, "25.00"), mustMoney(t, "100.00"), mustMoney(t, "75.00")
	if _, err := ledger.New(uuid.Nil(), uuid.NewV7(), uuid.NewV7(), ledger.Debit, amount, before, after, fixedTime); !errors.Is(err, ledger.ErrInvalidEntry) {
		t.Errorf("expected ErrInvalidEntry for nil id, got %v", err)
	}
	if _, err := ledger.New(uuid.NewV7(), uuid.Nil(), uuid.NewV7(), ledger.Debit, amount, before, after, fixedTime); !errors.Is(err, ledger.ErrInvalidEntry) {
		t.Errorf("expected ErrInvalidEntry for nil walletID, got %v", err)
	}
	if _, err := ledger.New(uuid.NewV7(), uuid.NewV7(), uuid.Nil(), ledger.Debit, amount, before, after, fixedTime); !errors.Is(err, ledger.ErrInvalidEntry) {
		t.Errorf("expected ErrInvalidEntry for nil transactionID, got %v", err)
	}
}

func TestRehydrate_DoesNotReValidateArithmetic(t *testing.T) {

	e, err := ledger.Rehydrate(uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), ledger.Debit, mustMoney(t, "25.00"), mustMoney(t, "100.00"), mustMoney(t, "999.00"), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.BalanceAfter().String() != "999.00" {
		t.Fatal("Rehydrate must preserve exactly what was persisted")
	}
}

func TestRehydrate_RejectsInvalidDirection(t *testing.T) {
	_, err := ledger.Rehydrate(uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), Direction("X"), mustMoney(t, "1.00"), mustMoney(t, "1.00"), mustMoney(t, "2.00"), fixedTime)
	if !errors.Is(err, ledger.ErrInvalidEntry) {
		t.Fatalf("expected ErrInvalidEntry, got %v", err)
	}
}

func TestSignedAmount(t *testing.T) {
	debit, err := ledger.New(uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), ledger.Debit, mustMoney(t, "25.00"), mustMoney(t, "100.00"), mustMoney(t, "75.00"), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	signed, err := debit.SignedAmount()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signed.String() != "-25.00" {
		t.Fatalf("debit SignedAmount = %s, want -25.00", signed)
	}

	credit, err := ledger.New(uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), ledger.Credit, mustMoney(t, "25.00"), mustMoney(t, "100.00"), mustMoney(t, "125.00"), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	signed, err = credit.SignedAmount()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signed.String() != "25.00" {
		t.Fatalf("credit SignedAmount = %s, want 25.00", signed)
	}
}

func TestGetters(t *testing.T) {
	id, walletID, txID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	amount, before, after := mustMoney(t, "25.00"), mustMoney(t, "100.00"), mustMoney(t, "75.00")
	e, err := ledger.New(id, walletID, txID, ledger.Debit, amount, before, after, fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.ID() != id || e.WalletID() != walletID || e.TransactionID() != txID {
		t.Fatal("identity getters mismatch")
	}
	if !e.Money().Equals(amount) || !e.BalanceBefore().Equals(before) || !e.BalanceAfter().Equals(after) {
		t.Fatal("money getters mismatch")
	}
	if e.CreatedAt() != fixedTime {
		t.Fatal("CreatedAt mismatch")
	}
}

func TestDirection_IsValid(t *testing.T) {
	if !ledger.Debit.IsValid() || !ledger.Credit.IsValid() {
		t.Fatal("Debit and Credit must be valid directions")
	}
	if Direction("SIDEWAYS").IsValid() {
		t.Fatal("an unknown direction must not be valid")
	}
}
