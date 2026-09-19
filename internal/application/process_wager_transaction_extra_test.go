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

func TestProcessRefund_RejectsWhenReferenceIsNotABet(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")

	winCmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "win-1", IdempotencyKey: "provider-a:win-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Win, Amount: mustMoney(t, "50.00"), Now: fixedTime,
	}
	if _, err := h.useCase.Execute(context.Background(), winCmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	refundCmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "refund-1", IdempotencyKey: "provider-a:refund-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Refund, Amount: mustMoney(t, "50.00"), ReferenceExternalTransactionID: "win-1", Now: fixedTime,
	}
	result, err := h.useCase.Execute(context.Background(), refundCmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != application.ResultRejected || result.FailureCode != domainerr.FailureReferenceMismatch {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestProcessReversal_RejectsWhenReferenceStillPending(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")

	pendingRefund := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "refund-1", IdempotencyKey: "provider-a:refund-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Refund, Amount: mustMoney(t, "25.00"), ReferenceExternalTransactionID: "bet-missing", Now: fixedTime,
	}
	pendingResult, err := h.useCase.Execute(context.Background(), pendingRefund)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pendingResult.Status != application.ResultPendingReference {
		t.Fatalf("setup failed: %+v", pendingResult)
	}

	rollbackOfPendingRefund := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "rollback-1", IdempotencyKey: "provider-a:rollback-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Rollback, Amount: mustMoney(t, "25.00"), ReferenceExternalTransactionID: "refund-1", Now: fixedTime,
	}
	result, err := h.useCase.Execute(context.Background(), rollbackOfPendingRefund)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != application.ResultRejected || result.FailureCode != domainerr.FailureReferenceNotProcessed {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestProcessReversal_RejectsOnRoundMismatch(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")
	processBetAndAssertOK(t, h, "provider-a", playerID, walletID, "bet-1", "25.00")

	refundDifferentRound := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "refund-1", IdempotencyKey: "provider-a:refund-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-DIFFERENT", GameID: "fortune-chimp",
		Kind: wager.Refund, Amount: mustMoney(t, "25.00"), ReferenceExternalTransactionID: "bet-1", Now: fixedTime,
	}
	result, err := h.useCase.Execute(context.Background(), refundDifferentRound)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != application.ResultRejected || result.FailureCode != domainerr.FailureReferenceMismatch {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestProcessReversal_RejectsOnAmountMismatch(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")
	processBetAndAssertOK(t, h, "provider-a", playerID, walletID, "bet-1", "25.00")

	refundWrongAmount := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "refund-1", IdempotencyKey: "provider-a:refund-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Refund, Amount: mustMoney(t, "10.00"), ReferenceExternalTransactionID: "bet-1", Now: fixedTime,
	}
	result, err := h.useCase.Execute(context.Background(), refundWrongAmount)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != application.ResultRejected || result.FailureCode != domainerr.FailureReferenceMismatch {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestProcessRollback_RejectsWhenExceedsBalance(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "100.00")

	winCmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "win-1", IdempotencyKey: "provider-a:win-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Win, Amount: mustMoney(t, "50.00"), Now: fixedTime,
	}
	if _, err := h.useCase.Execute(context.Background(), winCmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	drainCmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "bet-1", IdempotencyKey: "provider-a:bet-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Bet, Amount: mustMoney(t, "140.00"), Now: fixedTime,
	}
	if r, err := h.useCase.Execute(context.Background(), drainCmd); err != nil || r.Status != application.ResultProcessed {
		t.Fatalf("setup drain failed: %+v, %v", r, err)
	}
	rollbackCmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "rollback-1", IdempotencyKey: "provider-a:rollback-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Rollback, Amount: mustMoney(t, "50.00"), ReferenceExternalTransactionID: "win-1", Now: fixedTime,
	}
	result, err := h.useCase.Execute(context.Background(), rollbackCmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != application.ResultRejected || result.FailureCode != domainerr.FailureReversalExceedsBalance {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestProcessWin_WithResolvableReference(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")
	processBetAndAssertOK(t, h, "provider-a", playerID, walletID, "bet-1", "25.00")

	winCmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "win-1", IdempotencyKey: "provider-a:win-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Win, Amount: mustMoney(t, "50.00"), ReferenceExternalTransactionID: "bet-1", Now: fixedTime,
	}
	result, err := h.useCase.Execute(context.Background(), winCmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != application.ResultProcessed {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestPendingReferenceRetryPolicy_NextBackoffGrowsAndCaps(t *testing.T) {
	policy := application.PendingReferenceRetryPolicy{BaseBackoff: time.Second}
	if got := policy.NextBackoff(0); got != time.Second {
		t.Fatalf("NextBackoff(0) = %s, want 1s", got)
	}
	if got := policy.NextBackoff(1); got != 2*time.Second {
		t.Fatalf("NextBackoff(1) = %s, want 2s", got)
	}
	if got := policy.NextBackoff(20); got != 5*time.Minute {
		t.Fatalf("NextBackoff(20) = %s, want capped at 5m", got)
	}
}
