package application_test

import (
	"context"
	"github.com/cassianobraz/wager-service/internal/application"
	"testing"
	"uuid"
)

func TestReconciliation_ConsistentWallet(t *testing.T) {
	openUC, wallets, txs, ledgers, outbox := newOpenWalletHarness()
	playerID := uuid.NewV7()
	openResult, err := openUC.Execute(context.Background(), application.OpenWalletCommand{
		PlayerID: playerID, InitialBalance: mustMoney(t, "1000.00"), Now: fixedTime,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tx := &fakeTxManager{}
	inbox := newFakeInboxRepository()
	processUC := application.NewProcessWagerTransactionUseCase(tx, wallets, txs, ledgers, inbox, outbox)
	betCmd := betCommand("provider-a", playerID, openResult.WalletID, "bet-1", "")
	betCmd.Amount = mustMoney(t, "25.00")
	if _, err := processUC.Execute(context.Background(), betCmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	uc := application.NewReconciliationUseCase(tx, wallets, ledgers)
	result, err := uc.Execute(context.Background(), openResult.WalletID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Consistent {
		t.Fatalf("expected a consistent reconciliation, got difference %s", result.Difference)
	}
	if result.StoredBalance.String() != "975.00" || result.CalculatedBalance.String() != "975.00" {
		t.Fatalf("unexpected balances: stored=%s calculated=%s", result.StoredBalance, result.CalculatedBalance)
	}
	if result.CheckedEntries != 2 {
		t.Fatalf("checkedEntries = %d, want 2 (the OPENING credit plus the BET debit)", result.CheckedEntries)
	}
}

func TestReconciliation_IncludesOpeningEntry(t *testing.T) {
	openUC, wallets, _, ledgers, _ := newOpenWalletHarness()
	playerID := uuid.NewV7()
	openResult, err := openUC.Execute(context.Background(), application.OpenWalletCommand{
		PlayerID: playerID, InitialBalance: mustMoney(t, "1000.00"), Now: fixedTime,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	uc := application.NewReconciliationUseCase(&fakeTxManager{}, wallets, ledgers)
	result, err := uc.Execute(context.Background(), openResult.WalletID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Consistent || result.CheckedEntries != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}
