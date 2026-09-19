package application_test

import (
	"context"
	"github.com/cassianobraz/wager-service/internal/application"
	"github.com/cassianobraz/wager-service/internal/domain/events"
	"testing"
	"uuid"
)

func newOpenWalletHarness() (*application.OpenWalletUseCase, *fakeWalletRepository, *fakeWagerTransactionRepository, *fakeLedgerRepository, *fakeOutboxRepository) {
	tx := &fakeTxManager{}
	wallets := newFakeWalletRepository()
	txs := newFakeWagerTransactionRepository()
	ledgers := newFakeLedgerRepository()
	outbox := newFakeOutboxRepository()
	uc := application.NewOpenWalletUseCase(tx, wallets, txs, ledgers, outbox)
	return uc, wallets, txs, ledgers, outbox
}

func TestOpenWallet_WithPositiveBalance_CreatesOpeningTransactionAndLedger(t *testing.T) {
	uc, wallets, _, ledgers, outbox := newOpenWalletHarness()
	playerID := uuid.NewV7()

	result, err := uc.Execute(context.Background(), application.OpenWalletCommand{
		PlayerID: playerID, InitialBalance: mustMoney(t, "1000.00"), Now: fixedTime,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Balance.String() != "1000.00" || result.Version != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}

	w, err := wallets.FindByID(context.Background(), result.WalletID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Version() != 1 {
		t.Fatal("initial balance must not increment the version past 1")
	}

	entries, _, err := ledgers.ListByWallet(context.Background(), result.WalletID, "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one ledger entry for the opening credit, got %d", len(entries))
	}

	if len(outbox.byType(events.TypeWagerTransactionProcessed)) != 1 {
		t.Fatal("expected exactly one WagerTransactionProcessed event for the opening")
	}
	if len(outbox.byType(events.TypeWalletBalanceChanged)) != 1 {
		t.Fatal("expected exactly one WalletBalanceChanged event for the opening")
	}
}

func TestOpenWallet_WithZeroBalance_CreatesOnlyTheWallet(t *testing.T) {
	uc, wallets, _, ledgers, outbox := newOpenWalletHarness()
	playerID := uuid.NewV7()

	result, err := uc.Execute(context.Background(), application.OpenWalletCommand{
		PlayerID: playerID, InitialBalance: mustMoney(t, "0.00"), Now: fixedTime,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w, err := wallets.FindByID(context.Background(), result.WalletID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !w.Balance().IsZero() {
		t.Fatal("expected a zero balance")
	}

	entries, _, err := ledgers.ListByWallet(context.Background(), result.WalletID, "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatal("a zero-balance opening must not create a ledger entry")
	}
	if len(outbox.byType(events.TypeWagerTransactionProcessed)) != 0 {
		t.Fatal("a zero-balance opening must not publish any financial events")
	}
}

func TestOpenWallet_DuplicateForSamePlayerAndCurrency_Conflicts(t *testing.T) {
	uc, _, _, _, _ := newOpenWalletHarness()
	playerID := uuid.NewV7()

	if _, err := uc.Execute(context.Background(), application.OpenWalletCommand{
		PlayerID: playerID, InitialBalance: mustMoney(t, "100.00"), Now: fixedTime,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := uc.Execute(context.Background(), application.OpenWalletCommand{
		PlayerID: playerID, InitialBalance: mustMoney(t, "50.00"), Now: fixedTime,
	})
	if err == nil {
		t.Fatal("expected an error opening a second wallet for the same player and currency")
	}
}
