package wager_test

import (
	"errors"
	"github.com/cassianobraz/wager-service/internal/domain/domainerr"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"testing"
	"time"
	"uuid"
)

var fixedTime = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

func mustMoney(t *testing.T, amount string) money.Money {
	t.Helper()
	m, err := money.Parse(amount, "BRL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return m
}

func validParams(kind wager.Kind, amount string) wager.ExternalParams {
	p := wager.ExternalParams{
		ID:                    uuid.NewV7(),
		ProviderID:            "provider-a",
		ExternalTransactionID: "transaction-123",
		IdempotencyKey:        "provider-a:transaction-123",
		PayloadHash:           "deadbeef",
		WalletID:              uuid.NewV7(),
		PlayerID:              uuid.NewV7(),
		RoundID:               "round-987",
		GameID:                "fortune-chimp",
		Kind:                  kind,
	}
	m, err := money.Parse(amount, "BRL")
	if err != nil {
		panic(err)
	}
	p.Money = m
	if kind.RequiresReference() {
		p.ReferenceExternalTransactionID = "transaction-100"
	}
	return p
}

func TestNewExternal_RejectsOpeningKind(t *testing.T) {
	p := validParams(wager.Opening, "10.00")
	_, err := wager.NewExternal(p, fixedTime)
	if !errors.Is(err, wager.ErrInvalidKindForOperation) {
		t.Fatalf("expected ErrInvalidKindForOperation, got %v", err)
	}
}

func TestNewExternal_ValidBet(t *testing.T) {
	p := validParams(wager.Bet, "25.00")
	tx, err := wager.NewExternal(p, fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Status() != wager.Pending {
		t.Fatalf("status = %s, want PENDING", tx.Status())
	}
	if tx.Kind() != wager.Bet {
		t.Fatal("kind not preserved")
	}
}

func TestNewExternal_ZeroValuePolicyPerKind(t *testing.T) {
	cases := []struct {
		kind    wager.Kind
		amount  string
		wantErr bool
	}{
		{wager.Bet, "0.00", true},
		{wager.Bet, "25.00", false},
		{wager.Win, "0.00", true},
		{wager.Win, "25.00", false},
		{wager.Loss, "0.00", false},
		{wager.Loss, "25.00", true},
		{wager.Refund, "0.00", true},
		{wager.Refund, "25.00", false},
		{wager.Rollback, "0.00", true},
		{wager.Rollback, "25.00", false},
	}
	for _, tc := range cases {
		p := validParams(tc.kind, tc.amount)
		_, err := wager.NewExternal(p, fixedTime)
		if tc.wantErr && err == nil {
			t.Errorf("%s with amount %s: expected error, got nil", tc.kind, tc.amount)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s with amount %s: unexpected error: %v", tc.kind, tc.amount, err)
		}
	}
}

func TestNewExternal_NegativeAmountAlwaysRejected(t *testing.T) {
	for _, kind := range []wager.Kind{wager.Bet, wager.Win, wager.Loss, wager.Refund, wager.Rollback} {
		p := validParams(kind, "25.00")
		p.Money = mustMoney(t, "-25.00")
		if _, err := wager.NewExternal(p, fixedTime); err == nil {
			t.Errorf("%s: expected error for negative amount, got nil", kind)
		}
	}
}

func TestNewExternal_ReversalKindsRequireReference(t *testing.T) {
	for _, kind := range []wager.Kind{wager.Refund, wager.Rollback} {
		p := validParams(kind, "25.00")
		p.ReferenceExternalTransactionID = ""
		if _, err := wager.NewExternal(p, fixedTime); !errors.Is(err, wager.ErrInvalidTransaction) {
			t.Errorf("%s without reference: expected ErrInvalidTransaction, got %v", kind, err)
		}
	}
}

func TestNewExternal_BetAndLossMustNotCarryReference(t *testing.T) {
	for _, kind := range []wager.Kind{wager.Bet, wager.Loss} {
		p := validParams(kind, "25.00")
		p.ReferenceExternalTransactionID = "transaction-100"
		if _, err := wager.NewExternal(p, fixedTime); !errors.Is(err, wager.ErrInvalidTransaction) {
			t.Errorf("%s with reference: expected ErrInvalidTransaction, got %v", kind, err)
		}
	}
}

func TestNewExternal_WinMayOptionallyCarryReference(t *testing.T) {

	p := validParams(wager.Win, "25.00")
	p.ReferenceExternalTransactionID = "transaction-100"
	tx, err := wager.NewExternal(p, fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.ReferenceExternalTransactionID() != "transaction-100" {
		t.Fatal("optional WIN reference not preserved")
	}

	withoutRef := validParams(wager.Win, "25.00")
	if _, err := wager.NewExternal(withoutRef, fixedTime); err != nil {
		t.Fatalf("WIN without a reference must still be valid: %v", err)
	}
}

func TestNewExternal_RequiresIdempotencyKeyProviderAndExternalID(t *testing.T) {
	base := validParams(wager.Bet, "25.00")

	missingProvider := base
	missingProvider.ProviderID = ""
	if _, err := wager.NewExternal(missingProvider, fixedTime); !errors.Is(err, wager.ErrInvalidTransaction) {
		t.Error("expected ErrInvalidTransaction for missing providerId")
	}

	missingExternalID := base
	missingExternalID.ExternalTransactionID = ""
	if _, err := wager.NewExternal(missingExternalID, fixedTime); !errors.Is(err, wager.ErrInvalidTransaction) {
		t.Error("expected ErrInvalidTransaction for missing externalTransactionId")
	}

	missingKey := base
	missingKey.IdempotencyKey = ""
	if _, err := wager.NewExternal(missingKey, fixedTime); !errors.Is(err, wager.ErrInvalidTransaction) {
		t.Error("expected ErrInvalidTransaction for missing idempotencyKey")
	}

	missingHash := base
	missingHash.PayloadHash = ""
	if _, err := wager.NewExternal(missingHash, fixedTime); !errors.Is(err, wager.ErrInvalidTransaction) {
		t.Error("expected ErrInvalidTransaction for missing payloadHash")
	}
}

func TestNewExternal_RequiresRoundAndGame(t *testing.T) {
	missingRound := validParams(wager.Bet, "25.00")
	missingRound.RoundID = ""
	if _, err := wager.NewExternal(missingRound, fixedTime); !errors.Is(err, wager.ErrInvalidTransaction) {
		t.Error("expected ErrInvalidTransaction for missing roundId")
	}

	missingGame := validParams(wager.Bet, "25.00")
	missingGame.GameID = ""
	if _, err := wager.NewExternal(missingGame, fixedTime); !errors.Is(err, wager.ErrInvalidTransaction) {
		t.Error("expected ErrInvalidTransaction for missing gameId")
	}
}

func TestNewExternal_RejectsNilIdentities(t *testing.T) {
	missingID := validParams(wager.Bet, "25.00")
	missingID.ID = uuid.Nil()
	if _, err := wager.NewExternal(missingID, fixedTime); !errors.Is(err, wager.ErrInvalidTransaction) {
		t.Error("expected ErrInvalidTransaction for nil id")
	}

	missingWallet := validParams(wager.Bet, "25.00")
	missingWallet.WalletID = uuid.Nil()
	if _, err := wager.NewExternal(missingWallet, fixedTime); !errors.Is(err, wager.ErrInvalidTransaction) {
		t.Error("expected ErrInvalidTransaction for nil walletId")
	}
}

func TestNewExternal_AllGetters(t *testing.T) {
	p := validParams(wager.Refund, "25.00")
	tx, err := wager.NewExternal(p, fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.ProviderID() != p.ProviderID {
		t.Error("ProviderID mismatch")
	}
	if tx.ExternalTransactionID() != p.ExternalTransactionID {
		t.Error("ExternalTransactionID mismatch")
	}
	if tx.IdempotencyKey() != p.IdempotencyKey {
		t.Error("IdempotencyKey mismatch")
	}
	if tx.PayloadHash() != p.PayloadHash {
		t.Error("PayloadHash mismatch")
	}
	if tx.RoundID() != p.RoundID {
		t.Error("RoundID mismatch")
	}
	if tx.GameID() != p.GameID {
		t.Error("GameID mismatch")
	}
	if !tx.Money().Equals(p.Money) {
		t.Error("Money mismatch")
	}
	if tx.ReferenceExternalTransactionID() != p.ReferenceExternalTransactionID {
		t.Error("ReferenceExternalTransactionID mismatch")
	}
	if tx.CreatedAt() != fixedTime || tx.UpdatedAt() != fixedTime {
		t.Error("timestamps mismatch")
	}
	if tx.FailureCode() != "" {
		t.Error("a freshly created transaction must have no failure code")
	}

	if err := tx.MarkRejected(domainerr.FailureReferenceMismatch, fixedTime); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.FailureCode() != domainerr.FailureReferenceMismatch {
		t.Error("FailureCode not recorded by MarkRejected")
	}
}

func TestNewOpening_ValidCreatesProcessedDirectly(t *testing.T) {
	id, walletID, playerID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	amount := mustMoney(t, "1000.00")

	tx, err := wager.NewOpening(id, walletID, playerID, amount, fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Status() != wager.Processed {
		t.Fatalf("status = %s, want PROCESSED", tx.Status())
	}
	if tx.Kind() != wager.Opening {
		t.Fatal("kind not preserved")
	}
	if tx.ResultingBalance() == nil || tx.ResultingBalance().String() != "1000.00" {
		t.Fatal("expected resultingBalance to equal the initial amount")
	}
}

func TestNewOpening_RejectsNonPositiveAmount(t *testing.T) {
	_, err := wager.NewOpening(uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), mustMoney(t, "0.00"), fixedTime)
	if err == nil {
		t.Fatal("expected an error for a zero-amount OPENING")
	}
}

func TestNewOpening_RejectsNilIdentities(t *testing.T) {
	amount := mustMoney(t, "10.00")
	if _, err := wager.NewOpening(uuid.Nil(), uuid.NewV7(), uuid.NewV7(), amount, fixedTime); !errors.Is(err, wager.ErrInvalidTransaction) {
		t.Error("expected ErrInvalidTransaction for nil id")
	}
	if _, err := wager.NewOpening(uuid.NewV7(), uuid.Nil(), uuid.NewV7(), amount, fixedTime); !errors.Is(err, wager.ErrInvalidTransaction) {
		t.Error("expected ErrInvalidTransaction for nil walletId")
	}
	if _, err := wager.NewOpening(uuid.NewV7(), uuid.NewV7(), uuid.Nil(), amount, fixedTime); !errors.Is(err, wager.ErrInvalidTransaction) {
		t.Error("expected ErrInvalidTransaction for nil playerId")
	}
}

func TestStateMachine_PendingToProcessed(t *testing.T) {
	tx, err := wager.NewExternal(validParams(wager.Bet, "25.00"), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := tx.MarkProcessed(mustMoney(t, "975.00"), fixedTime.Add(time.Second)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Status() != wager.Processed {
		t.Fatalf("status = %s, want PROCESSED", tx.Status())
	}
	if tx.ResultingBalance() == nil || tx.ResultingBalance().String() != "975.00" {
		t.Fatal("resultingBalance not recorded")
	}
}

func TestStateMachine_PendingToPendingReferenceToProcessed(t *testing.T) {
	tx, err := wager.NewExternal(validParams(wager.Refund, "25.00"), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := tx.MarkPendingReference(fixedTime); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Status() != wager.PendingReference {
		t.Fatalf("status = %s, want PENDING_REFERENCE", tx.Status())
	}
	if err := tx.MarkProcessed(mustMoney(t, "1025.00"), fixedTime); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Status() != wager.Processed {
		t.Fatal("expected transition from PENDING_REFERENCE to PROCESSED")
	}
}

func TestStateMachine_TerminalStatesRejectFurtherTransitions(t *testing.T) {
	terminalStates := []func(tx *wager.Transaction) error{
		func(tx *wager.Transaction) error { return tx.MarkProcessed(mustMoney(t, "1.00"), fixedTime) },
		func(tx *wager.Transaction) error {
			return tx.MarkRejected(domainerr.FailureInsufficientFunds, fixedTime)
		},
		func(tx *wager.Transaction) error { return tx.MarkFailed(domainerr.FailureReferenceNotFound, fixedTime) },
	}

	for _, makeTerminal := range terminalStates {
		tx, err := wager.NewExternal(validParams(wager.Bet, "25.00"), fixedTime)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := makeTerminal(tx); err != nil {
			t.Fatalf("unexpected error reaching terminal state: %v", err)
		}

		if err := tx.MarkProcessed(mustMoney(t, "1.00"), fixedTime); !errors.Is(err, domainerr.ErrInvalidTransition) {
			t.Errorf("MarkProcessed after terminal state: expected ErrInvalidTransition, got %v", err)
		}
		if err := tx.MarkRejected(domainerr.FailureInsufficientFunds, fixedTime); !errors.Is(err, domainerr.ErrInvalidTransition) {
			t.Errorf("MarkRejected after terminal state: expected ErrInvalidTransition, got %v", err)
		}
		if err := tx.MarkFailed(domainerr.FailureReferenceNotFound, fixedTime); !errors.Is(err, domainerr.ErrInvalidTransition) {
			t.Errorf("MarkFailed after terminal state: expected ErrInvalidTransition, got %v", err)
		}
		if err := tx.MarkPendingReference(fixedTime); !errors.Is(err, domainerr.ErrInvalidTransition) {
			t.Errorf("MarkPendingReference after terminal state: expected ErrInvalidTransition, got %v", err)
		}
	}
}

func TestMarkPendingReference_OnlyAllowedFromPending(t *testing.T) {
	tx, err := wager.NewExternal(validParams(wager.Refund, "25.00"), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := tx.MarkPendingReference(fixedTime); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := tx.MarkPendingReference(fixedTime); !errors.Is(err, domainerr.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition on double PENDING_REFERENCE, got %v", err)
	}
}

func TestMarkRejected_RequiresFailureCode(t *testing.T) {
	tx, err := wager.NewExternal(validParams(wager.Bet, "25.00"), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := tx.MarkRejected("", fixedTime); !errors.Is(err, wager.ErrInvalidTransaction) {
		t.Fatalf("expected ErrInvalidTransaction for empty failure code, got %v", err)
	}
}

func TestMarkFailed_RequiresFailureCode(t *testing.T) {
	tx, err := wager.NewExternal(validParams(wager.Bet, "25.00"), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := tx.MarkFailed("", fixedTime); !errors.Is(err, wager.ErrInvalidTransaction) {
		t.Fatalf("expected ErrInvalidTransaction for empty failure code, got %v", err)
	}
}

func TestAttachResolvedReference(t *testing.T) {
	tx, err := wager.NewExternal(validParams(wager.Refund, "25.00"), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	refID := uuid.NewV7()
	if err := tx.AttachResolvedReference(refID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.ResolvedReferenceTransactionID() == nil || *tx.ResolvedReferenceTransactionID() != refID {
		t.Fatal("resolved reference not recorded")
	}
}

func TestAttachResolvedReference_RejectsNilID(t *testing.T) {
	tx, err := wager.NewExternal(validParams(wager.Refund, "25.00"), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := tx.AttachResolvedReference(uuid.Nil()); !errors.Is(err, wager.ErrInvalidTransaction) {
		t.Fatalf("expected ErrInvalidTransaction, got %v", err)
	}
}

func TestAttachResolvedReference_RejectsAfterTerminal(t *testing.T) {
	tx, err := wager.NewExternal(validParams(wager.Refund, "25.00"), fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := tx.MarkRejected(domainerr.FailureReferenceNotFound, fixedTime); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := tx.AttachResolvedReference(uuid.NewV7()); !errors.Is(err, domainerr.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestIsExternal(t *testing.T) {
	for _, kind := range []wager.Kind{wager.Bet, wager.Win, wager.Loss, wager.Refund, wager.Rollback} {
		if !kind.IsExternal() {
			t.Errorf("%s should be external", kind)
		}
	}
	if wager.Opening.IsExternal() {
		t.Error("OPENING should not be external")
	}
}

func TestStatus_IsTerminal(t *testing.T) {
	terminal := []wager.Status{wager.Processed, wager.Rejected, wager.Failed}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%s should be terminal", s)
		}
	}
	nonTerminal := []wager.Status{wager.Pending, wager.PendingReference}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("%s should not be terminal", s)
		}
	}
}

func TestRehydrate_PreservesEveryField(t *testing.T) {
	id, walletID, playerID := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	refID := uuid.NewV7()
	balance := mustMoney(t, "975.00")

	tx, err := wager.Rehydrate(wager.RehydrateParams{
		ID:                             id,
		ProviderID:                     "provider-a",
		ExternalTransactionID:          "transaction-123",
		IdempotencyKey:                 "provider-a:transaction-123",
		PayloadHash:                    "deadbeef",
		WalletID:                       walletID,
		PlayerID:                       playerID,
		RoundID:                        "round-987",
		GameID:                         "fortune-chimp",
		Kind:                           wager.Bet,
		Money:                          mustMoney(t, "25.00"),
		ResolvedReferenceTransactionID: &refID,
		Status:                         wager.Processed,
		ResultingBalance:               &balance,
		CreatedAt:                      fixedTime,
		UpdatedAt:                      fixedTime,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.ID() != id || tx.WalletID() != walletID || tx.PlayerID() != playerID {
		t.Fatal("identity fields not preserved")
	}
	if tx.Status() != wager.Processed {
		t.Fatal("status not preserved")
	}
	if tx.ResolvedReferenceTransactionID() == nil || *tx.ResolvedReferenceTransactionID() != refID {
		t.Fatal("resolved reference not preserved")
	}
}

func TestRehydrate_RejectsNilIdentities(t *testing.T) {
	_, err := wager.Rehydrate(wager.RehydrateParams{
		ID:       uuid.Nil(),
		WalletID: uuid.NewV7(),
		PlayerID: uuid.NewV7(),
	})
	if !errors.Is(err, wager.ErrInvalidTransaction) {
		t.Fatalf("expected ErrInvalidTransaction, got %v", err)
	}
}
