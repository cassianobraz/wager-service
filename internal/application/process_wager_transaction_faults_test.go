package application_test

import (
	"context"
	"errors"
	"github.com/cassianobraz/wager-service/internal/application"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"github.com/cassianobraz/wager-service/internal/domain/wallet"
	"testing"
	"uuid"
)

type faultyHarness struct {
	tx      *fakeTxManager
	wallets *faultyWallets
	txs     *faultyTxs
	ledgers *faultyLedgers
	inbox   *faultyInbox
	outbox  *faultyOutbox
	useCase *application.ProcessWagerTransactionUseCase
}

func newFaultyHarness() *faultyHarness {
	h := &faultyHarness{
		tx:      &fakeTxManager{},
		wallets: &faultyWallets{fakeWalletRepository: newFakeWalletRepository()},
		txs:     &faultyTxs{fakeWagerTransactionRepository: newFakeWagerTransactionRepository()},
		ledgers: &faultyLedgers{fakeLedgerRepository: newFakeLedgerRepository()},
		inbox:   &faultyInbox{fakeInboxRepository: newFakeInboxRepository()},
		outbox:  &faultyOutbox{fakeOutboxRepository: newFakeOutboxRepository()},
	}
	h.useCase = application.NewProcessWagerTransactionUseCase(h.tx, h.wallets, h.txs, h.ledgers, h.inbox, h.outbox)
	return h
}

func TestExecute_PropagatesLedgerAppendFailure(t *testing.T) {
	h := newFaultyHarness()
	playerID := uuid.NewV7()
	w, err := walletFor(t, playerID, "1000.00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := h.wallets.Create(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h.ledgers.failAppend = true
	cmd := betCommand("provider-a", playerID, w.ID(), "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")

	if _, err := h.useCase.Execute(context.Background(), cmd); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom to propagate, got %v", err)
	}
}

func TestExecute_PropagatesWalletSaveFailure(t *testing.T) {
	h := newFaultyHarness()
	playerID := uuid.NewV7()
	w, err := walletFor(t, playerID, "1000.00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := h.wallets.Create(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h.wallets.failSave = true
	cmd := betCommand("provider-a", playerID, w.ID(), "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")

	if _, err := h.useCase.Execute(context.Background(), cmd); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom to propagate, got %v", err)
	}
}

func TestExecute_PropagatesTransactionSaveFailureOnReject(t *testing.T) {
	h := newFaultyHarness()
	playerID := uuid.NewV7()
	w, err := walletFor(t, playerID, "10.00")
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
		t.Fatalf("expected errBoom to propagate from the rejection path, got %v", err)
	}
}

func TestExecute_PropagatesOutboxAppendFailure(t *testing.T) {
	h := newFaultyHarness()
	playerID := uuid.NewV7()
	w, err := walletFor(t, playerID, "1000.00")
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
		t.Fatalf("expected errBoom to propagate, got %v", err)
	}
}

func TestExecute_PropagatesFindByIDForUpdateFailure(t *testing.T) {
	h := newFaultyHarness()
	playerID := uuid.NewV7()
	w, err := walletFor(t, playerID, "1000.00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := h.wallets.Create(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h.wallets.failFindForUpdate = true
	cmd := betCommand("provider-a", playerID, w.ID(), "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")

	if _, err := h.useCase.Execute(context.Background(), cmd); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom to propagate, got %v", err)
	}
}

func TestExecute_PropagatesFindByIDFailureOnLoss(t *testing.T) {
	h := newFaultyHarness()
	playerID := uuid.NewV7()
	w, err := walletFor(t, playerID, "1000.00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := h.wallets.Create(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h.wallets.failFindByID = true
	cmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "tx-1", IdempotencyKey: "provider-a:tx-1",
		PlayerID: playerID, WalletID: w.ID(), RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Loss, Amount: mustMoney(t, "0.00"), Now: fixedTime,
	}
	if _, err := h.useCase.Execute(context.Background(), cmd); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom to propagate, got %v", err)
	}
}

func TestExecute_PropagatesFindByProviderAndExternalIDFailure(t *testing.T) {
	h := newFaultyHarness()
	playerID := uuid.NewV7()
	w, err := walletFor(t, playerID, "1000.00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := h.wallets.Create(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h.txs.failFindByProviderExternalID = true
	cmd := betCommand("provider-a", playerID, w.ID(), "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")

	if _, err := h.useCase.Execute(context.Background(), cmd); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom to propagate, got %v", err)
	}
}

func TestExecute_PropagatesFindByIdempotencyKeyFailure(t *testing.T) {
	h := newFaultyHarness()
	playerID := uuid.NewV7()
	w, err := walletFor(t, playerID, "1000.00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := h.wallets.Create(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h.txs.failFindByIdempotencyKey = true
	cmd := betCommand("provider-a", playerID, w.ID(), "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")

	if _, err := h.useCase.Execute(context.Background(), cmd); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom to propagate, got %v", err)
	}
}

func TestExecute_PropagatesInboxFindFailure(t *testing.T) {
	h := newFaultyHarness()
	playerID := uuid.NewV7()
	w, err := walletFor(t, playerID, "1000.00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := h.wallets.Create(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h.inbox.failFind = true
	cmd := betCommand("provider-a", playerID, w.ID(), "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")
	cmd.Inbox = &application.InboxDedup{ConsumerName: "wager-consumer", MessageID: "msg-1"}

	if _, err := h.useCase.Execute(context.Background(), cmd); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom to propagate, got %v", err)
	}
}

func TestExecute_ReplayLookupFailurePropagates(t *testing.T) {
	h := newFaultyHarness()
	playerID := uuid.NewV7()
	w, err := walletFor(t, playerID, "1000.00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := h.wallets.Create(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := betCommand("provider-a", playerID, w.ID(), "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")
	cmd.Inbox = &application.InboxDedup{ConsumerName: "wager-consumer", MessageID: "msg-1"}
	if _, err := h.useCase.Execute(context.Background(), cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h.txs.failFindByIdempotencyKey = true
	if _, err := h.useCase.Execute(context.Background(), cmd); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom to propagate from the replay lookup, got %v", err)
	}
}

func walletFor(t *testing.T, playerID uuid.UUID, balance string) (*wallet.Wallet, error) {
	t.Helper()
	return wallet.New(uuid.NewV7(), playerID, mustMoney(t, balance), fixedTime)
}
