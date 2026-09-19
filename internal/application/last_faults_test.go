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

func TestExecute_Loss_PropagatesTransactionSaveFailure(t *testing.T) {
	h := newFaultyHarness()
	playerID := uuid.NewV7()
	w, err := walletFor(t, playerID, "100.00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := h.wallets.Create(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h.txs.failSave = true
	cmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "tx-1", IdempotencyKey: "provider-a:tx-1",
		PlayerID: playerID, WalletID: w.ID(), RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Loss, Amount: mustMoney(t, "0.00"), Now: fixedTime,
	}
	if _, err := h.useCase.Execute(context.Background(), cmd); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestExecute_Loss_PropagatesOutboxAppendFailure(t *testing.T) {
	h := newFaultyHarness()
	playerID := uuid.NewV7()
	w, err := walletFor(t, playerID, "100.00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := h.wallets.Create(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h.outbox.failAppend = true
	cmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "tx-1", IdempotencyKey: "provider-a:tx-1",
		PlayerID: playerID, WalletID: w.ID(), RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Loss, Amount: mustMoney(t, "0.00"), Now: fixedTime,
	}
	if _, err := h.useCase.Execute(context.Background(), cmd); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func setupResolvableRefund(t *testing.T, h *harness) uuid.UUID {
	t.Helper()
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
	return walletID
}

func TestResolvePendingReferences_PropagatesWalletSaveFailure(t *testing.T) {
	h := newHarness()
	setupResolvableRefund(t, h)

	faultyWallets := &faultyWallets{fakeWalletRepository: h.wallets, failSave: true}
	worker := application.NewResolvePendingReferencesUseCase(h.tx, faultyWallets, h.txs, h.ledgers, h.outbox, application.DefaultPendingReferenceRetryPolicy())
	if _, _, err := worker.RunOnce(context.Background(), fixedTime.Add(time.Minute), 10); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestResolvePendingReferences_PropagatesTransactionSaveFailure(t *testing.T) {
	h := newHarness()
	setupResolvableRefund(t, h)

	faultyTxs := &faultyTxs{fakeWagerTransactionRepository: h.txs, failSave: true}
	worker := application.NewResolvePendingReferencesUseCase(h.tx, h.wallets, faultyTxs, h.ledgers, h.outbox, application.DefaultPendingReferenceRetryPolicy())
	if _, _, err := worker.RunOnce(context.Background(), fixedTime.Add(time.Minute), 10); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestResolvePendingReferences_PropagatesOutboxAppendFailure(t *testing.T) {
	h := newHarness()
	setupResolvableRefund(t, h)

	faultyOutbox := &faultyOutbox{fakeOutboxRepository: h.outbox, failAppend: true}
	worker := application.NewResolvePendingReferencesUseCase(h.tx, h.wallets, h.txs, h.ledgers, faultyOutbox, application.DefaultPendingReferenceRetryPolicy())
	if _, _, err := worker.RunOnce(context.Background(), fixedTime.Add(time.Minute), 10); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestResolvePendingReferences_RejectPending_PropagatesOutboxAppendFailure(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")

	refundCmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "refund-1", IdempotencyKey: "provider-a:refund-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Refund, Amount: mustMoney(t, "25.00"), ReferenceExternalTransactionID: "bet-never-arrives", Now: fixedTime,
	}
	if _, err := h.useCase.Execute(context.Background(), refundCmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	faultyOutbox := &faultyOutbox{fakeOutboxRepository: h.outbox, failAppend: true}
	policy := application.PendingReferenceRetryPolicy{MaxAttempts: 3, MaxAge: time.Hour, BaseBackoff: time.Second}
	worker := application.NewResolvePendingReferencesUseCase(h.tx, h.wallets, h.txs, h.ledgers, faultyOutbox, policy)

	if _, _, err := worker.RunOnce(context.Background(), fixedTime.Add(2*time.Hour), 10); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestResolvePendingReferences_RejectPending_PropagatesTransactionSaveFailure(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")

	refundCmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "refund-1", IdempotencyKey: "provider-a:refund-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Refund, Amount: mustMoney(t, "25.00"), ReferenceExternalTransactionID: "bet-never-arrives", Now: fixedTime,
	}
	if _, err := h.useCase.Execute(context.Background(), refundCmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	faultyTxs := &faultyTxs{fakeWagerTransactionRepository: h.txs, failSave: true}
	policy := application.PendingReferenceRetryPolicy{MaxAttempts: 3, MaxAge: time.Hour, BaseBackoff: time.Second}
	worker := application.NewResolvePendingReferencesUseCase(h.tx, h.wallets, faultyTxs, h.ledgers, h.outbox, policy)

	if _, _, err := worker.RunOnce(context.Background(), fixedTime.Add(2*time.Hour), 10); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}
