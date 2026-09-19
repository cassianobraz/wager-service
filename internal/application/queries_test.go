package application_test

import (
	"context"
	"github.com/cassianobraz/wager-service/internal/application"
	"testing"
	"uuid"
)

func TestQueryService_GetWalletAndTransaction(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")
	processBetAndAssertOK(t, h, "provider-a", playerID, walletID, "bet-1", "25.00")

	qs := application.NewQueryService(h.wallets, h.txs, h.ledgers)

	w, err := qs.GetWallet(context.Background(), walletID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Balance().String() != "975.00" {
		t.Fatalf("balance = %s, want 975.00", w.Balance())
	}

	tx, err := qs.GetTransactionByExternalID(context.Background(), "provider-a", "bet-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	txByID, err := qs.GetTransaction(context.Background(), tx.ID())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if txByID.ID() != tx.ID() {
		t.Fatal("GetTransaction and GetTransactionByExternalID must agree")
	}

	entries, _, err := qs.GetLedgerPage(context.Background(), walletID, "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 ledger entry, got %d", len(entries))
	}
}
