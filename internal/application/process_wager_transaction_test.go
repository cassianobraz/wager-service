package application_test

import (
	"context"
	"errors"
	"github.com/cassianobraz/wager-service/internal/application"
	"github.com/cassianobraz/wager-service/internal/domain/domainerr"
	"github.com/cassianobraz/wager-service/internal/domain/events"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"github.com/cassianobraz/wager-service/internal/domain/wallet"
	"github.com/cassianobraz/wager-service/internal/ports"
	"sync"
	"testing"
	"time"
	"uuid"
)

var fixedTime = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

type harness struct {
	tx      *fakeTxManager
	wallets *fakeWalletRepository
	txs     *fakeWagerTransactionRepository
	ledgers *fakeLedgerRepository
	inbox   *fakeInboxRepository
	outbox  *fakeOutboxRepository
	useCase *application.ProcessWagerTransactionUseCase
}

func newHarness() *harness {
	h := &harness{
		tx:      &fakeTxManager{},
		wallets: newFakeWalletRepository(),
		txs:     newFakeWagerTransactionRepository(),
		ledgers: newFakeLedgerRepository(),
		inbox:   newFakeInboxRepository(),
		outbox:  newFakeOutboxRepository(),
	}
	h.useCase = application.NewProcessWagerTransactionUseCase(h.tx, h.wallets, h.txs, h.ledgers, h.inbox, h.outbox)
	return h
}

func mustMoney(t *testing.T, amount string) money.Money {
	t.Helper()
	m, err := money.Parse(amount, "BRL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return m
}

func (h *harness) createWallet(t *testing.T, playerID uuid.UUID, balance string) uuid.UUID {
	t.Helper()
	w, err := wallet.New(uuid.NewV7(), playerID, mustMoney(t, balance), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := h.wallets.Create(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return w.ID()
}

func betCommand(providerID string, playerID, walletID uuid.UUID, externalID, amount string) application.ProcessWagerTransactionCommand {
	return application.ProcessWagerTransactionCommand{
		ProviderID: providerID, ExternalTransactionID: externalID,
		IdempotencyKey: providerID + ":" + externalID,
		PlayerID:       playerID, WalletID: walletID,
		RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Bet, Amount: money.Money{}, Now: fixedTime,
	}
}

func TestProcessBet_Success(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")

	cmd := betCommand("provider-a", playerID, walletID, "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")

	result, err := h.useCase.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != application.ResultProcessed {
		t.Fatalf("status = %s, want PROCESSED", result.Status)
	}
	if result.Balance == nil || result.Balance.String() != "975.00" {
		t.Fatalf("balance = %v, want 975.00", result.Balance)
	}

	w, err := h.wallets.FindByID(context.Background(), walletID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Balance().String() != "975.00" || w.Version() != 2 {
		t.Fatalf("wallet after debit = %s v%d, want 975.00 v2", w.Balance(), w.Version())
	}

	processed := h.outbox.byType(events.TypeWagerTransactionProcessed)
	balanceChanged := h.outbox.byType(events.TypeWalletBalanceChanged)
	if len(processed) != 1 || len(balanceChanged) != 1 {
		t.Fatalf("expected exactly one Processed and one BalanceChanged event, got %d and %d", len(processed), len(balanceChanged))
	}
}

func TestProcessBet_InsufficientFunds(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "10.00")

	cmd := betCommand("provider-a", playerID, walletID, "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")

	result, err := h.useCase.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != application.ResultRejected {
		t.Fatalf("status = %s, want REJECTED", result.Status)
	}
	if result.FailureCode != domainerr.FailureInsufficientFunds {
		t.Fatalf("failureCode = %s, want %s", result.FailureCode, domainerr.FailureInsufficientFunds)
	}

	w, err := h.wallets.FindByID(context.Background(), walletID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Balance().String() != "10.00" || w.Version() != 1 {
		t.Fatal("a rejected BET must not move the wallet")
	}
	rejected := h.outbox.byType(events.TypeWagerTransactionRejected)
	if len(rejected) != 1 {
		t.Fatalf("expected exactly one Rejected event, got %d", len(rejected))
	}
}

func TestProcessWin_CreditsWallet(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "100.00")

	cmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "tx-1", IdempotencyKey: "provider-a:tx-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Win, Amount: mustMoney(t, "50.00"), Now: fixedTime,
	}
	result, err := h.useCase.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != application.ResultProcessed || result.Balance.String() != "150.00" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestProcessLoss_NoMovementNoBalanceChangedEvent(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "100.00")

	cmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "tx-1", IdempotencyKey: "provider-a:tx-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Loss, Amount: mustMoney(t, "0.00"), Now: fixedTime,
	}
	result, err := h.useCase.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != application.ResultProcessed || result.Balance.String() != "100.00" {
		t.Fatalf("unexpected result: %+v", result)
	}
	w, _ := h.wallets.FindByID(context.Background(), walletID)
	if w.Version() != 1 {
		t.Fatal("LOSS must not increment the wallet version")
	}
	if len(h.outbox.byType(events.TypeWalletBalanceChanged)) != 0 {
		t.Fatal("LOSS must never publish WalletBalanceChanged")
	}
	if len(h.outbox.byType(events.TypeWagerTransactionProcessed)) != 1 {
		t.Fatal("LOSS must still publish WagerTransactionProcessed")
	}
}

func processBetAndAssertOK(t *testing.T, h *harness, providerID string, playerID, walletID uuid.UUID, externalID, amount string) {
	t.Helper()
	cmd := betCommand(providerID, playerID, walletID, externalID, "")
	cmd.Amount = mustMoney(t, amount)
	result, err := h.useCase.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != application.ResultProcessed {
		t.Fatalf("bet setup failed: %+v", result)
	}
}

func TestProcessRefund_ReversesProcessedBet(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")
	processBetAndAssertOK(t, h, "provider-a", playerID, walletID, "bet-1", "25.00")

	cmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "refund-1", IdempotencyKey: "provider-a:refund-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Refund, Amount: mustMoney(t, "25.00"), ReferenceExternalTransactionID: "bet-1", Now: fixedTime,
	}
	result, err := h.useCase.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != application.ResultProcessed || result.Balance.String() != "1000.00" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestProcessRollback_ReversesProcessedWin(t *testing.T) {
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

	rollbackCmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "rollback-1", IdempotencyKey: "provider-a:rollback-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Rollback, Amount: mustMoney(t, "50.00"), ReferenceExternalTransactionID: "win-1", Now: fixedTime,
	}
	result, err := h.useCase.Execute(context.Background(), rollbackCmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != application.ResultProcessed || result.Balance.String() != "100.00" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestProcessReversal_ReferenceNotFoundGoesToPendingReference(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")

	cmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "refund-1", IdempotencyKey: "provider-a:refund-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Refund, Amount: mustMoney(t, "25.00"), ReferenceExternalTransactionID: "bet-does-not-exist-yet", Now: fixedTime,
	}
	result, err := h.useCase.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != application.ResultPendingReference {
		t.Fatalf("status = %s, want PENDING_REFERENCE", result.Status)
	}
	if len(h.outbox.byType(events.TypeWagerTransactionPendingReference)) != 1 {
		t.Fatal("expected exactly one WagerTransactionPendingReference event")
	}
}

func TestProcessReversal_DuplicateReversalRejected(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")
	processBetAndAssertOK(t, h, "provider-a", playerID, walletID, "bet-1", "25.00")

	firstRefund := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "refund-1", IdempotencyKey: "provider-a:refund-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Refund, Amount: mustMoney(t, "25.00"), ReferenceExternalTransactionID: "bet-1", Now: fixedTime,
	}
	if r, err := h.useCase.Execute(context.Background(), firstRefund); err != nil || r.Status != application.ResultProcessed {
		t.Fatalf("setup refund failed: %+v, %v", r, err)
	}

	secondRollback := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "rollback-1", IdempotencyKey: "provider-a:rollback-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Rollback, Amount: mustMoney(t, "25.00"), ReferenceExternalTransactionID: "bet-1", Now: fixedTime,
	}
	result, err := h.useCase.Execute(context.Background(), secondRollback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != application.ResultRejected || result.FailureCode != domainerr.FailureDuplicateReversal {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestExecute_IdempotentReplaySameKeyAndContent(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")

	cmd := betCommand("provider-a", playerID, walletID, "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")

	first, err := h.useCase.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := h.useCase.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if second.IdempotentReplay != true {
		t.Fatal("expected second call to be flagged as an idempotent replay")
	}
	if second.Balance.String() != first.Balance.String() {
		t.Fatalf("replay balance = %s, want original %s", second.Balance, first.Balance)
	}
	w, _ := h.wallets.FindByID(context.Background(), walletID)
	if w.Version() != 2 {
		t.Fatal("a replay must not debit the wallet a second time")
	}
}

func TestExecute_IdempotencyKeyReusedWithDifferentContent(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")

	cmd := betCommand("provider-a", playerID, walletID, "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")
	if _, err := h.useCase.Execute(context.Background(), cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mutated := cmd
	mutated.Amount = mustMoney(t, "99.00")
	_, err := h.useCase.Execute(context.Background(), mutated)
	if !errors.Is(err, application.ErrIdempotencyKeyReused) {
		t.Fatalf("expected ErrIdempotencyKeyReused, got %v", err)
	}
}

func TestExecute_ExternalTransactionIDReusedWithDifferentKey(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")

	cmd := betCommand("provider-a", playerID, walletID, "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")
	if _, err := h.useCase.Execute(context.Background(), cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sameExternalDifferentKey := cmd
	sameExternalDifferentKey.IdempotencyKey = "provider-a:some-other-key"
	_, err := h.useCase.Execute(context.Background(), sameExternalDifferentKey)
	if !errors.Is(err, application.ErrExternalTransactionKeyMismatch) {
		t.Fatalf("expected ErrExternalTransactionKeyMismatch, got %v", err)
	}
}

func TestConcurrency_SameBetSubmitted50TimesInParallel_SingleDebit(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")

	cmd := betCommand("provider-a", playerID, walletID, "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")

	const n = 50
	var wg sync.WaitGroup
	results := make([]application.ProcessResult, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = h.useCase.Execute(context.Background(), cmd)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}

	w, err := h.wallets.FindByID(context.Background(), walletID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Balance().String() != "975.00" {
		t.Fatalf("final balance = %s, want 975.00 (a single debit)", w.Balance())
	}
	if w.Version() != 2 {
		t.Fatalf("final version = %d, want 2 (exactly one successful movement)", w.Version())
	}

	entries, _, err := h.ledgers.ListByWallet(context.Background(), walletID, "", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("ledger has %d entries, want exactly 1", len(entries))
	}

	processedCount := 0
	for _, r := range results {
		if r.Status == application.ResultProcessed {
			processedCount++
		}
	}
	if processedCount != n {
		t.Fatalf("expected all %d calls to report PROCESSED (one original + %d replays), got %d", n, n-1, processedCount)
	}
}

func TestConcurrency_TwoCompetingBetsExceedBalance_OneProcessedOneRejected(t *testing.T) {

	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "100.00")

	cmdA := betCommand("provider-a", playerID, walletID, "bet-a", "")
	cmdA.Amount = mustMoney(t, "80.00")
	cmdB := betCommand("provider-a", playerID, walletID, "bet-b", "")
	cmdB.Amount = mustMoney(t, "80.00")

	var wg sync.WaitGroup
	var resA, resB application.ProcessResult
	var errA, errB error
	wg.Add(2)
	go func() { defer wg.Done(); resA, errA = h.useCase.Execute(context.Background(), cmdA) }()
	go func() { defer wg.Done(); resB, errB = h.useCase.Execute(context.Background(), cmdB) }()
	wg.Wait()

	if errA != nil || errB != nil {
		t.Fatalf("unexpected errors: %v, %v", errA, errB)
	}

	processed, rejected := 0, 0
	for _, r := range []application.ProcessResult{resA, resB} {
		switch r.Status {
		case application.ResultProcessed:
			processed++
		case application.ResultRejected:
			rejected++
			if r.FailureCode != domainerr.FailureInsufficientFunds {
				t.Fatalf("rejected result has failureCode %s, want %s", r.FailureCode, domainerr.FailureInsufficientFunds)
			}
		}
	}
	if processed != 1 || rejected != 1 {
		t.Fatalf("expected exactly one PROCESSED and one REJECTED, got %d processed, %d rejected", processed, rejected)
	}

	w, err := h.wallets.FindByID(context.Background(), walletID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Balance().String() != "20.00" {
		t.Fatalf("final balance = %s, want 20.00", w.Balance())
	}

	entries, _, err := h.ledgers.ListByWallet(context.Background(), walletID, "", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("ledger has %d entries, want exactly 1 (a single debit)", len(entries))
	}

	replayA, err := h.useCase.Execute(context.Background(), cmdA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	replayB, err := h.useCase.Execute(context.Background(), cmdB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if replayA.Status != resA.Status || replayB.Status != resB.Status {
		t.Fatal("replays must reproduce the original outcome")
	}
	wAfterReplay, _ := h.wallets.FindByID(context.Background(), walletID)
	if wAfterReplay.Balance().String() != "20.00" {
		t.Fatal("replays must not change the wallet balance")
	}
}

func TestConcurrency_DifferentWalletsProcessIndependently(t *testing.T) {
	h := newHarness()
	playerA, playerB := uuid.NewV7(), uuid.NewV7()
	walletA := h.createWallet(t, playerA, "100.00")
	walletB := h.createWallet(t, playerB, "100.00")

	cmdA := betCommand("provider-a", playerA, walletA, "bet-a", "")
	cmdA.Amount = mustMoney(t, "40.00")
	cmdB := betCommand("provider-a", playerB, walletB, "bet-b", "")
	cmdB.Amount = mustMoney(t, "60.00")

	var wg sync.WaitGroup
	var errA, errB error
	wg.Add(2)
	go func() { defer wg.Done(); _, errA = h.useCase.Execute(context.Background(), cmdA) }()
	go func() { defer wg.Done(); _, errB = h.useCase.Execute(context.Background(), cmdB) }()
	wg.Wait()

	if errA != nil || errB != nil {
		t.Fatalf("unexpected errors: %v, %v", errA, errB)
	}

	wA, _ := h.wallets.FindByID(context.Background(), walletA)
	wB, _ := h.wallets.FindByID(context.Background(), walletB)
	if wA.Balance().String() != "60.00" {
		t.Fatalf("wallet A balance = %s, want 60.00", wA.Balance())
	}
	if wB.Balance().String() != "40.00" {
		t.Fatalf("wallet B balance = %s, want 40.00", wB.Balance())
	}
}

func TestExecute_InboxDedup_RedeliveryAfterCompletionDoesNotReprocess(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")

	cmd := betCommand("provider-a", playerID, walletID, "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")
	cmd.Inbox = &application.InboxDedup{ConsumerName: "wager-consumer", MessageID: "msg-1"}

	first, err := h.useCase.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second, err := h.useCase.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !second.IdempotentReplay {
		t.Fatal("expected redelivery to be reported as an idempotent replay")
	}
	if second.Balance.String() != first.Balance.String() {
		t.Fatal("redelivery must not change the reported balance")
	}

	w, _ := h.wallets.FindByID(context.Background(), walletID)
	if w.Version() != 2 {
		t.Fatal("redelivery after completion must not debit the wallet again")
	}
}

func TestExecute_InboxDedup_CrashBetweenInsertAndCompletionIsSafeToRetry(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")

	cmd := betCommand("provider-a", playerID, walletID, "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")
	cmd.Inbox = &application.InboxDedup{ConsumerName: "wager-consumer", MessageID: "msg-1"}

	hash, err := application.CanonicalHash(application.CanonicalPayload{
		ProviderID: "provider-a", ExternalTransactionID: "tx-1", PlayerID: playerID.String(),
		WalletID: walletID.String(), RoundID: "round-1", GameID: "fortune-chimp",
		Kind: "BET", Amount: "25.00", Currency: "BRL",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := h.inbox.Insert(context.Background(), ports.InboxRecord{
		ConsumerName: "wager-consumer", MessageID: "msg-1", PayloadHash: hash, ReceivedAt: fixedTime,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := h.useCase.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != application.ResultProcessed {
		t.Fatalf("unexpected result: %+v", result)
	}
	w, _ := h.wallets.FindByID(context.Background(), walletID)
	if w.Version() != 2 {
		t.Fatal("exactly one debit expected after resuming an interrupted delivery")
	}
}
