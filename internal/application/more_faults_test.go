package application_test

import (
	"context"
	"errors"
	"github.com/cassianobraz/wager-service/internal/application"
	"github.com/cassianobraz/wager-service/internal/domain/ledger"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"testing"
	"time"
	"uuid"
)

type faultyLedgersForSum struct {
	*fakeLedgerRepository
	failSum bool
}

func (f *faultyLedgersForSum) SumByWallet(ctx context.Context, walletID uuid.UUID) (int64, string, int, error) {
	if f.failSum {
		return 0, "", 0, errBoom
	}
	return f.fakeLedgerRepository.SumByWallet(ctx, walletID)
}

func TestReconciliation_PropagatesWalletLookupFailure(t *testing.T) {
	wallets := &faultyWallets{fakeWalletRepository: newFakeWalletRepository(), failFindByID: true}
	ledgers := newFakeLedgerRepository()
	uc := application.NewReconciliationUseCase(&fakeTxManager{}, wallets, ledgers)
	if _, err := uc.Execute(context.Background(), uuid.NewV7()); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestReconciliation_PropagatesSumFailure(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "100.00")

	faultyLedgers := &faultyLedgersForSum{fakeLedgerRepository: h.ledgers, failSum: true}
	uc := application.NewReconciliationUseCase(h.tx, h.wallets, faultyLedgers)
	if _, err := uc.Execute(context.Background(), walletID); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

type faultyTxsForCreate struct {
	*fakeWagerTransactionRepository
	failCreate bool
}

func (f *faultyTxsForCreate) Create(ctx context.Context, tx *wager.Transaction) error {
	if f.failCreate {
		return errBoom
	}
	return f.fakeWagerTransactionRepository.Create(ctx, tx)
}

func TestOpenWallet_PropagatesOpeningTransactionCreateFailure(t *testing.T) {
	wallets := newFakeWalletRepository()
	txs := &faultyTxsForCreate{fakeWagerTransactionRepository: newFakeWagerTransactionRepository(), failCreate: true}
	ledgers := newFakeLedgerRepository()
	outbox := newFakeOutboxRepository()
	uc := application.NewOpenWalletUseCase(&fakeTxManager{}, wallets, txs, ledgers, outbox)

	_, err := uc.Execute(context.Background(), application.OpenWalletCommand{
		PlayerID: uuid.NewV7(), InitialBalance: mustMoney(t, "100.00"), Now: fixedTime,
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestOpenWallet_PropagatesLedgerAppendFailure(t *testing.T) {
	wallets := newFakeWalletRepository()
	txs := newFakeWagerTransactionRepository()
	ledgers := &faultyLedgers{fakeLedgerRepository: newFakeLedgerRepository(), failAppend: true}
	outbox := newFakeOutboxRepository()
	uc := application.NewOpenWalletUseCase(&fakeTxManager{}, wallets, txs, ledgers, outbox)

	_, err := uc.Execute(context.Background(), application.OpenWalletCommand{
		PlayerID: uuid.NewV7(), InitialBalance: mustMoney(t, "100.00"), Now: fixedTime,
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestOpenWallet_PropagatesOutboxAppendFailure(t *testing.T) {
	wallets := newFakeWalletRepository()
	txs := newFakeWagerTransactionRepository()
	ledgers := newFakeLedgerRepository()
	outbox := &faultyOutbox{fakeOutboxRepository: newFakeOutboxRepository(), failAppend: true}
	uc := application.NewOpenWalletUseCase(&fakeTxManager{}, wallets, txs, ledgers, outbox)

	_, err := uc.Execute(context.Background(), application.OpenWalletCommand{
		PlayerID: uuid.NewV7(), InitialBalance: mustMoney(t, "100.00"), Now: fixedTime,
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestExecute_HonorsExplicitCorrelationID(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")

	correlationID := uuid.NewV7()
	cmd := betCommand("provider-a", playerID, walletID, "tx-1", "")
	cmd.Amount = mustMoney(t, "25.00")
	cmd.CorrelationID = correlationID

	if _, err := h.useCase.Execute(context.Background(), cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, rec := range h.outbox.records {
		if rec.Envelope.CorrelationID != correlationID {
			t.Fatalf("event %s carries correlationId %s, want the explicit %s", rec.EventType, rec.Envelope.CorrelationID, correlationID)
		}
	}
}

type faultyCountTxs struct {
	*fakeWagerTransactionRepository
}

func (f *faultyCountTxs) CountSuccessfulReversals(ctx context.Context, referencedTransactionID uuid.UUID, kind wager.Kind) (int, error) {
	return 0, errBoom
}

func TestResolvePendingReferences_PropagatesCountSuccessfulReversalsFailure(t *testing.T) {
	h := newHarness()
	playerID := uuid.NewV7()
	walletID := h.createWallet(t, playerID, "1000.00")
	processBetAndAssertOK(t, h, "provider-a", playerID, walletID, "bet-1", "25.00")

	refundCmd := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-a", ExternalTransactionID: "refund-1", IdempotencyKey: "provider-a:refund-1",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Refund, Amount: mustMoney(t, "25.00"), ReferenceExternalTransactionID: "bet-missing", Now: fixedTime,
	}
	if _, err := h.useCase.Execute(context.Background(), refundCmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	refundCmd2 := application.ProcessWagerTransactionCommand{
		ProviderID: "provider-b", ExternalTransactionID: "refund-2", IdempotencyKey: "provider-b:refund-2",
		PlayerID: playerID, WalletID: walletID, RoundID: "round-1", GameID: "fortune-chimp",
		Kind: wager.Refund, Amount: mustMoney(t, "25.00"), ReferenceExternalTransactionID: "bet-missing-2", Now: fixedTime,
	}
	if _, err := h.useCase.Execute(context.Background(), refundCmd2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := h.txs.FindByProviderAndExternalID(context.Background(), "provider-b", "bet-missing-2"); err == nil {
		t.Fatal("test setup assumption violated: reference must not exist yet")
	}

	processBetAndAssertOK(t, h, "provider-b", playerID, walletID, "bet-missing-2", "25.00")

	faultyTxs := &faultyCountTxs{fakeWagerTransactionRepository: h.txs}
	worker := application.NewResolvePendingReferencesUseCase(h.tx, h.wallets, faultyTxs, h.ledgers, h.outbox, application.DefaultPendingReferenceRetryPolicy())

	if _, _, err := worker.RunOnce(context.Background(), fixedTime.Add(time.Minute), 10); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

func TestResolvePendingReferences_PropagatesListDueFailure(t *testing.T) {
	failingTxs := &faultyListTxs{fakeWagerTransactionRepository: newFakeWagerTransactionRepository()}
	worker := application.NewResolvePendingReferencesUseCase(&fakeTxManager{}, newFakeWalletRepository(), failingTxs, newFakeLedgerRepository(), newFakeOutboxRepository(), application.DefaultPendingReferenceRetryPolicy())
	if _, _, err := worker.RunOnce(context.Background(), fixedTime, 10); !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
}

type faultyListTxs struct {
	*fakeWagerTransactionRepository
}

func (f *faultyListTxs) ListPendingReferenceDue(ctx context.Context, now time.Time, limit int) ([]*wager.Transaction, error) {
	return nil, errBoom
}

var _ = ledger.Debit
