package wallet_test

import (
	"errors"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"github.com/cassianobraz/wager-service/internal/domain/wallet"
	"testing"
	"time"
	"uuid"
)

var fixedTime = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

func mustMoney(t *testing.T, amount, currency string) money.Money {
	t.Helper()
	m, err := money.Parse(amount, currency)
	if err != nil {
		t.Fatalf("money.Parse(%q, %q) unexpected error: %v", amount, currency, err)
	}
	return m
}

func newTestWallet(t *testing.T, balance string) *wallet.Wallet {
	t.Helper()
	w, err := wallet.New(uuid.NewV7(), uuid.NewV7(), mustMoney(t, balance, "BRL"), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error creating wallet: %v", err)
	}
	return w
}

func TestNew_ValidWallet(t *testing.T) {
	id := uuid.NewV7()
	playerID := uuid.NewV7()
	balance := mustMoney(t, "1000.00", "BRL")

	w, err := wallet.New(id, playerID, balance, fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.ID() != id || w.PlayerID() != playerID {
		t.Fatal("identity fields not preserved")
	}
	if !w.Balance().Equals(balance) {
		t.Fatalf("balance = %s, want %s", w.Balance(), balance)
	}
	if w.Version() != 1 {
		t.Fatalf("initial version = %d, want 1", w.Version())
	}
	if w.CreatedAt() != fixedTime || w.UpdatedAt() != fixedTime {
		t.Fatal("timestamps not set to creation instant")
	}
}

func TestNew_ZeroInitialBalanceStaysAtVersion1(t *testing.T) {
	w, err := wallet.New(uuid.NewV7(), uuid.NewV7(), mustMoney(t, "0.00", "BRL"), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Version() != 1 {
		t.Fatalf("version = %d, want 1", w.Version())
	}
}

func TestNew_RejectsNilIdentities(t *testing.T) {
	balance := mustMoney(t, "0.00", "BRL")
	if _, err := wallet.New(uuid.Nil(), uuid.NewV7(), balance, fixedTime); !errors.Is(err, wallet.ErrInvalidWallet) {
		t.Fatalf("expected ErrInvalidWallet for nil id, got %v", err)
	}
	if _, err := wallet.New(uuid.NewV7(), uuid.Nil(), balance, fixedTime); !errors.Is(err, wallet.ErrInvalidWallet) {
		t.Fatalf("expected ErrInvalidWallet for nil playerID, got %v", err)
	}
}

func TestNew_RejectsNegativeInitialBalance(t *testing.T) {
	balance := mustMoney(t, "-1.00", "BRL")
	if _, err := wallet.New(uuid.NewV7(), uuid.NewV7(), balance, fixedTime); !errors.Is(err, wallet.ErrInvalidWallet) {
		t.Fatalf("expected ErrInvalidWallet, got %v", err)
	}
}

func TestRehydrate_DoesNotReapplyOrChangeState(t *testing.T) {
	id, playerID := uuid.NewV7(), uuid.NewV7()
	balance := mustMoney(t, "500.00", "BRL")
	createdAt := fixedTime
	updatedAt := fixedTime.Add(time.Hour)

	w, err := wallet.Rehydrate(id, playerID, balance, 7, createdAt, updatedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Version() != 7 {
		t.Fatalf("version = %d, want 7", w.Version())
	}
	if !w.Balance().Equals(balance) {
		t.Fatalf("balance = %s, want %s", w.Balance(), balance)
	}
	if w.UpdatedAt() != updatedAt {
		t.Fatal("Rehydrate must preserve the persisted updatedAt exactly")
	}
}

func TestRehydrate_RejectsVersionBelowOne(t *testing.T) {
	balance := mustMoney(t, "0.00", "BRL")
	if _, err := wallet.Rehydrate(uuid.NewV7(), uuid.NewV7(), balance, 0, fixedTime, fixedTime); !errors.Is(err, wallet.ErrInvalidWallet) {
		t.Fatalf("expected ErrInvalidWallet, got %v", err)
	}
}

func TestDebit_ReducesBalanceAndIncrementsVersion(t *testing.T) {
	w := newTestWallet(t, "100.00")
	movement, err := w.Debit(mustMoney(t, "25.00", "BRL"), fixedTime.Add(time.Minute))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Balance().String() != "75.00" {
		t.Fatalf("balance after debit = %s, want 75.00", w.Balance())
	}
	if w.Version() != 2 {
		t.Fatalf("version after debit = %d, want 2", w.Version())
	}
	if movement.BalanceBefore.String() != "100.00" || movement.BalanceAfter.String() != "75.00" {
		t.Fatalf("unexpected movement: %+v", movement)
	}
	if movement.WalletVersion != 2 {
		t.Fatalf("movement.WalletVersion = %d, want 2", movement.WalletVersion)
	}
}

func TestDebit_RejectsInsufficientBalance(t *testing.T) {
	w := newTestWallet(t, "100.00")
	_, err := w.Debit(mustMoney(t, "150.00", "BRL"), fixedTime)
	if !errors.Is(err, wallet.ErrInsufficientBalance) {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
	if w.Balance().String() != "100.00" {
		t.Fatal("a rejected debit must not mutate the balance")
	}
	if w.Version() != 1 {
		t.Fatal("a rejected debit must not mutate the version")
	}
}

func TestDebit_ExactBalanceLeavesZero(t *testing.T) {
	w := newTestWallet(t, "80.00")
	if _, err := w.Debit(mustMoney(t, "80.00", "BRL"), fixedTime); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !w.Balance().IsZero() {
		t.Fatalf("balance = %s, want 0.00", w.Balance())
	}
}

func TestDebit_RejectsCurrencyMismatch(t *testing.T) {
	w := newTestWallet(t, "100.00")
	_, err := w.Debit(mustMoney(t, "10.00", "USD"), fixedTime)
	if !errors.Is(err, wallet.ErrCurrencyMismatch) {
		t.Fatalf("expected ErrCurrencyMismatch, got %v", err)
	}
}

func TestDebit_RejectsNonPositiveAmount(t *testing.T) {
	w := newTestWallet(t, "100.00")
	for _, amount := range []string{"0.00", "-10.00"} {
		if _, err := w.Debit(mustMoney(t, amount, "BRL"), fixedTime); !errors.Is(err, wallet.ErrNonPositiveAmount) {
			t.Errorf("Debit(%s) error = %v, want ErrNonPositiveAmount", amount, err)
		}
	}
}

func TestCredit_IncreasesBalanceAndIncrementsVersion(t *testing.T) {
	w := newTestWallet(t, "100.00")
	movement, err := w.Credit(mustMoney(t, "25.50", "BRL"), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Balance().String() != "125.50" {
		t.Fatalf("balance after credit = %s, want 125.50", w.Balance())
	}
	if w.Version() != 2 {
		t.Fatalf("version after credit = %d, want 2", w.Version())
	}
	if movement.BalanceBefore.String() != "100.00" || movement.BalanceAfter.String() != "125.50" {
		t.Fatalf("unexpected movement: %+v", movement)
	}
}

func TestCredit_RejectsCurrencyMismatch(t *testing.T) {
	w := newTestWallet(t, "100.00")
	if _, err := w.Credit(mustMoney(t, "10.00", "USD"), fixedTime); !errors.Is(err, wallet.ErrCurrencyMismatch) {
		t.Fatalf("expected ErrCurrencyMismatch, got %v", err)
	}
}

func TestCredit_RejectsNonPositiveAmount(t *testing.T) {
	w := newTestWallet(t, "100.00")
	for _, amount := range []string{"0.00", "-10.00"} {
		if _, err := w.Credit(mustMoney(t, amount, "BRL"), fixedTime); !errors.Is(err, wallet.ErrNonPositiveAmount) {
			t.Errorf("Credit(%s) error = %v, want ErrNonPositiveAmount", amount, err)
		}
	}
}

func TestConcurrentBetsOnSameWallet_OnlyOneCanSucceed(t *testing.T) {

	w := newTestWallet(t, "100.00")

	first := mustMoney(t, "80.00", "BRL")
	second := mustMoney(t, "80.00", "BRL")

	_, err1 := w.Debit(first, fixedTime)
	_, err2 := w.Debit(second, fixedTime)

	if err1 != nil {
		t.Fatalf("expected the first debit to succeed, got %v", err1)
	}
	if !errors.Is(err2, wallet.ErrInsufficientBalance) {
		t.Fatalf("expected the second debit to be rejected, got %v", err2)
	}
	if w.Balance().String() != "20.00" {
		t.Fatalf("final balance = %s, want 20.00", w.Balance())
	}
	if w.Version() != 2 {
		t.Fatalf("version = %d, want 2 (only one successful movement)", w.Version())
	}
}

func TestCanDebit(t *testing.T) {
	w := newTestWallet(t, "50.00")
	if !w.CanDebit(mustMoney(t, "50.00", "BRL")) {
		t.Error("expected CanDebit to allow debiting the exact balance")
	}
	if w.CanDebit(mustMoney(t, "50.01", "BRL")) {
		t.Error("expected CanDebit to reject debiting more than the balance")
	}
	if w.CanDebit(mustMoney(t, "10.00", "USD")) {
		t.Error("expected CanDebit to reject a currency mismatch")
	}
	if w.CanDebit(mustMoney(t, "0.00", "BRL")) {
		t.Error("expected CanDebit to reject a non-positive amount")
	}
	if w.Balance().String() != "50.00" || w.Version() != 1 {
		t.Fatal("CanDebit must be a pure query, not mutate the wallet")
	}
}
