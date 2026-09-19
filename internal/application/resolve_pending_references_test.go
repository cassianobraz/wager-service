package application_test

import (
	"context"
	"github.com/cassianobraz/wager-service/internal/application"
	"github.com/cassianobraz/wager-service/internal/domain/domainerr"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"testing"
	"time"
	"uuid"
)

func TestResolvePendingReferences_ResolvesOnceReferenceArrives(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")

	refundCmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "refund-1", IdempotencyKey: "provider-a:refund-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Refund, Amount: mustMoney(t, "25.00"), ReferenceExternalTransactionID: "bet-1", Now: fixedTime,
	}
	result, err := h.useCase.Execute(context.Background(), refundCmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != application.ResultPendingReference {
		t.Fatalf("expected PENDING_REFERENCE, got %+v", result)
	}

	processBetAndAssertOK(t, h, "provider-a", playerID, walletID, "bet-1", "25.00")

	policy := application.DefaultPendingReferenceRetryPolicy()
	worker := application.NewResolvePendingReferencesUseCase(h.tx, h.wallets, h.txs, h.ledgers, h.outbox, policy)

	resolved, rescheduled, err := worker.RunOnce(context.Background(), fixedTime.Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != 1 || rescheduled != 0 {
		t.Fatalf("resolved=%d rescheduled=%d, want 1 and 0", resolved, rescheduled)
	}

	refundTx, err := h.txs.FindByProviderAndExternalID(context.Background(), "provider-a", "refund-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if refundTx.Status() != wager.Processed {
		t.Fatalf("refund status = %s, want PROCESSED", refundTx.Status())
	}

	w, _ := h.wallets.FindByID(context.Background(), walletID)
	if w.Balance().String() != "1000.00" {
		t.Fatalf("final balance = %s, want 1000.00 (bet then full refund)", w.Balance())
	}
}

func TestResolvePendingReferences_StillMissingIsRescheduled(t *testing.T) {
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

	policy := application.PendingReferenceRetryPolicy{MaxAttempts: 10, MaxAge: 24 * time.Hour, BaseBackoff: time.Second}
	worker := application.NewResolvePendingReferencesUseCase(h.tx, h.wallets, h.txs, h.ledgers, h.outbox, policy)

	resolved, rescheduled, err := worker.RunOnce(context.Background(), fixedTime.Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != 0 || rescheduled != 1 {
		t.Fatalf("resolved=%d rescheduled=%d, want 0 and 1", resolved, rescheduled)
	}

	tx, err := h.txs.FindByProviderAndExternalID(context.Background(), "provider-a", "refund-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Status() != wager.PendingReference {
		t.Fatalf("status = %s, want still PENDING_REFERENCE", tx.Status())
	}
}

func TestResolvePendingReferences_ExpiresAfterMaxAge(t *testing.T) {
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

	policy := application.PendingReferenceRetryPolicy{MaxAttempts: 3, MaxAge: time.Hour, BaseBackoff: time.Second}
	worker := application.NewResolvePendingReferencesUseCase(h.tx, h.wallets, h.txs, h.ledgers, h.outbox, policy)

	resolved, _, err := worker.RunOnce(context.Background(), fixedTime.Add(2*time.Hour), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != 1 {
		t.Fatalf("resolved = %d, want 1", resolved)
	}

	tx, err := h.txs.FindByProviderAndExternalID(context.Background(), "provider-a", "refund-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Status() != wager.Rejected {
		t.Fatalf("status = %s, want REJECTED", tx.Status())
	}
	if tx.FailureCode() != domainerr.FailureReferenceNotFound {
		t.Fatalf("failureCode = %s, want %s", tx.FailureCode(), domainerr.FailureReferenceNotFound)
	}
}
