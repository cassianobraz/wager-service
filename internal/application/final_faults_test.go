package application_test

import (
	"context"
	"errors"
	"github.com/cassianobraz/wager-service/internal/application"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"testing"
	"time"
	"uuid"
)

func TestExecute_PropagatesTransactionSaveFailureOnSuccess(t *testing.T) {
	h := newFaultyHarness()
	playerID := uuid.NewV7()
	w, err := walletFor(t, playerID, "1000.00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := h.wallets.Create(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h.txs.failSave = true
	cmd := betCommand("provider-a", playerID, w.ID(), "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")

	if _, err := h.useCase.Execute(context.Background(), cmd); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom on the success-path Save, got %v", err)
	}
}

func TestExecute_PropagatesOutboxAppendFailureOnRejection(t *testing.T) {
	h := newFaultyHarness()
	playerID := uuid.NewV7()
	w, err := walletFor(t, playerID, "10.00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := h.wallets.Create(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h.outbox.failAppend = true
	cmd := betCommand("provider-a", playerID, w.ID(), "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")

	if _, err := h.useCase.Execute(context.Background(), cmd); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom to propagate from the rejection path's outbox append, got %v", err)
	}
}

func TestResolvePendingReferences_PropagatesWalletFindForUpdateFailure(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")
	processBetAndAssertOK(t, h, "provider-a", playerID, walletID, "bet-1", "25.00")

	refundCmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "refund-1", IdempotencyKey: "provider-a:refund-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Refund, Amount: mustMoney(t, "25.00"), ReferenceExternalTransactionID: "bet-not-yet", Now: fixedTime,
	}
	if _, err := h.useCase.Execute(context.Background(), refundCmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	processBetAndAssertOK(t, h, "provider-a", playerID, walletID, "bet-not-yet", "25.00")

	faultyWallets := &faultyWallets{fakeWalletRepository: h.wallets, failFindForUpdate: true}
	worker := application.NewResolvePendingReferencesUseCase(h.tx, faultyWallets, h.txs, h.ledgers, h.outbox, application.DefaultPendingReferenceRetryPolicy())

	if _, _, err := worker.RunOnce(context.Background(), fixedTime.Add(time.Minute), 10); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestResolvePendingReferences_PropagatesLedgerAppendFailure(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")
	processBetAndAssertOK(t, h, "provider-a", playerID, walletID, "bet-1", "25.00")

	refundCmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "refund-1", IdempotencyKey: "provider-a:refund-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Refund, Amount: mustMoney(t, "25.00"), ReferenceExternalTransactionID: "bet-not-yet", Now: fixedTime,
	}
	if _, err := h.useCase.Execute(context.Background(), refundCmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	processBetAndAssertOK(t, h, "provider-a", playerID, walletID, "bet-not-yet", "25.00")

	faultyLedgers := &faultyLedgers{fakeLedgerRepository: h.ledgers, failAppend: true}
	worker := application.NewResolvePendingReferencesUseCase(h.tx, h.wallets, h.txs, faultyLedgers, h.outbox, application.DefaultPendingReferenceRetryPolicy())

	if _, _, err := worker.RunOnce(context.Background(), fixedTime.Add(time.Minute), 10); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}
