package application

import (
	"context"
	"errors"
	"github.com/cassianobraz/wager-service/internal/domain/domainerr"
	"github.com/cassianobraz/wager-service/internal/domain/events"
	"github.com/cassianobraz/wager-service/internal/domain/ledger"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"github.com/cassianobraz/wager-service/internal/domain/wallet"
	"github.com/cassianobraz/wager-service/internal/ports"
	"time"
	"uuid"
)

type InboxDedup struct {
	ConsumerName string
	MessageID    string
}

type ProcessWagerTransactionCommand struct {
	ProviderID                     string
	ExternalTransactionID          string
	IdempotencyKey                 string
	PlayerID                       uuid.UUID
	WalletID                       uuid.UUID
	RoundID                        string
	GameID                         string
	Kind                           wager.Kind
	Amount                         money.Money
	ReferenceExternalTransactionID string
	CorrelationID                  uuid.UUID
	Now                            time.Time
	Inbox                          *InboxDedup
}

type ProcessResultStatus string

const (
	ResultProcessed        ProcessResultStatus = "PROCESSED"
	ResultRejected         ProcessResultStatus = "REJECTED"
	ResultPending          ProcessResultStatus = "PENDING"
	ResultPendingReference ProcessResultStatus = "PENDING_REFERENCE"
)

type ProcessResult struct {
	TransactionID    uuid.UUID
	Status           ProcessResultStatus
	Balance          *money.Money
	FailureCode      domainerr.FailureCode
	IdempotentReplay bool
}

var ErrIdempotencyKeyReused = errors.New("application: idempotency key reused with different content")

var ErrExternalTransactionKeyMismatch = errors.New("application: externalTransactionId already recorded under a different idempotency key")

type ProcessWagerTransactionUseCase struct {
	tx      ports.TxManager
	wallets ports.WalletRepository
	txs     ports.WagerTransactionRepository
	ledgers ports.LedgerRepository
	inbox   ports.InboxRepository
	outbox  ports.OutboxRepository
}

func NewProcessWagerTransactionUseCase(
	tx ports.TxManager,
	wallets ports.WalletRepository,
	txs ports.WagerTransactionRepository,
	ledgers ports.LedgerRepository,
	inbox ports.InboxRepository,
	outbox ports.OutboxRepository,
) *ProcessWagerTransactionUseCase {
	return &ProcessWagerTransactionUseCase{
		tx: tx, wallets: wallets, txs: txs, ledgers: ledgers, inbox: inbox, outbox: outbox,
	}
}

func (uc *ProcessWagerTransactionUseCase) Execute(ctx context.Context, cmd ProcessWagerTransactionCommand) (ProcessResult, error) {
	hash, err := CanonicalHash(canonicalPayloadFrom(cmd))
	if err != nil {
		return ProcessResult{}, err
	}

	var result ProcessResult
	err = uc.tx.WithinTx(ctx, func(ctx context.Context) error {
		if cmd.Inbox != nil {
			replay, handled, err := uc.handleInbox(ctx, cmd, hash)
			if err != nil {
				return err
			}
			if handled {
				result = replay
				return nil
			}
		}

		r, err := uc.processIdempotently(ctx, cmd, hash)
		if err != nil {
			return err
		}
		result = r

		if cmd.Inbox != nil {
			if err := uc.inbox.MarkCompleted(ctx, cmd.Inbox.ConsumerName, cmd.Inbox.MessageID, cmd.Now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ProcessResult{}, err
	}
	return result, nil
}

func (uc *ProcessWagerTransactionUseCase) handleInbox(ctx context.Context, cmd ProcessWagerTransactionCommand, hash string) (ProcessResult, bool, error) {
	existing, err := uc.inbox.Find(ctx, cmd.Inbox.ConsumerName, cmd.Inbox.MessageID)
	if err != nil && !errors.Is(err, domainerr.ErrNotFound) {
		return ProcessResult{}, false, err
	}
	if existing != nil {
		if existing.PayloadHash != hash {
			return ProcessResult{}, false, ErrIdempotencyKeyReused
		}
		if existing.CompletedAt != nil {
			r, err := uc.replayByIdempotencyKey(ctx, cmd.ProviderID, cmd.IdempotencyKey)
			if err != nil {
				return ProcessResult{}, false, err
			}
			return r, true, nil
		}
		return ProcessResult{}, false, nil
	}
	if err := uc.inbox.Insert(ctx, ports.InboxRecord{
		ConsumerName: cmd.Inbox.ConsumerName,
		MessageID:    cmd.Inbox.MessageID,
		PayloadHash:  hash,
		ReceivedAt:   cmd.Now,
	}); err != nil {
		return ProcessResult{}, false, err
	}
	return ProcessResult{}, false, nil
}

func (uc *ProcessWagerTransactionUseCase) replayByIdempotencyKey(ctx context.Context, providerID, idempotencyKey string) (ProcessResult, error) {
	tx, err := uc.txs.FindByIdempotencyKey(ctx, providerID, idempotencyKey)
	if err != nil {
		return ProcessResult{}, err
	}
	return resultFromTransaction(tx, true), nil
}

func (uc *ProcessWagerTransactionUseCase) processIdempotently(ctx context.Context, cmd ProcessWagerTransactionCommand, hash string) (ProcessResult, error) {
	byExternalID, err := uc.txs.FindByProviderAndExternalID(ctx, cmd.ProviderID, cmd.ExternalTransactionID)
	if err != nil && !errors.Is(err, domainerr.ErrNotFound) {
		return ProcessResult{}, err
	}
	if byExternalID != nil && byExternalID.IdempotencyKey() != cmd.IdempotencyKey {
		return ProcessResult{}, ErrExternalTransactionKeyMismatch
	}

	byKey, err := uc.txs.FindByIdempotencyKey(ctx, cmd.ProviderID, cmd.IdempotencyKey)
	if err != nil && !errors.Is(err, domainerr.ErrNotFound) {
		return ProcessResult{}, err
	}
	if byKey != nil {
		if byKey.PayloadHash() != hash {
			return ProcessResult{}, ErrIdempotencyKeyReused
		}
		return resultFromTransaction(byKey, true), nil
	}

	id := uuid.NewV7()
	tx, err := wager.NewExternal(wager.ExternalParams{
		ID:                             id,
		ProviderID:                     cmd.ProviderID,
		ExternalTransactionID:          cmd.ExternalTransactionID,
		IdempotencyKey:                 cmd.IdempotencyKey,
		PayloadHash:                    hash,
		WalletID:                       cmd.WalletID,
		PlayerID:                       cmd.PlayerID,
		RoundID:                        cmd.RoundID,
		GameID:                         cmd.GameID,
		Kind:                           cmd.Kind,
		Money:                          cmd.Amount,
		ReferenceExternalTransactionID: cmd.ReferenceExternalTransactionID,
	}, cmd.Now)
	if err != nil {
		return ProcessResult{}, err
	}
	if err := uc.txs.Create(ctx, tx); err != nil {
		if errors.Is(err, domainerr.ErrConflict) {
			concurrent, findErr := uc.txs.FindByIdempotencyKey(ctx, cmd.ProviderID, cmd.IdempotencyKey)
			if findErr != nil {
				return ProcessResult{}, err
			}
			if concurrent.PayloadHash() != hash {
				return ProcessResult{}, ErrIdempotencyKeyReused
			}
			return resultFromTransaction(concurrent, true), nil
		}
		return ProcessResult{}, err
	}

	switch cmd.Kind {
	case wager.Bet:
		return uc.processBet(ctx, tx, cmd)
	case wager.Win:
		return uc.processWin(ctx, tx, cmd)
	case wager.Loss:
		return uc.processLoss(ctx, tx, cmd)
	case wager.Refund:
		return uc.processReversal(ctx, tx, cmd, wager.Bet)
	case wager.Rollback:
		return uc.processReversal(ctx, tx, cmd, "")
	default:
		return ProcessResult{}, wager.ErrInvalidKindForOperation
	}
}

func (uc *ProcessWagerTransactionUseCase) processBet(ctx context.Context, tx *wager.Transaction, cmd ProcessWagerTransactionCommand) (ProcessResult, error) {
	w, err := uc.wallets.FindByIDForUpdate(ctx, cmd.WalletID)
	if err != nil {
		return ProcessResult{}, err
	}
	expectedVersion := w.Version()

	movement, err := w.Debit(cmd.Amount, cmd.Now)
	if errors.Is(err, wallet.ErrInsufficientBalance) {
		return uc.rejectAndSave(ctx, tx, cmd, domainerr.FailureInsufficientFunds)
	}
	if err != nil {
		return ProcessResult{}, err
	}

	return uc.commitMovement(ctx, tx, w, expectedVersion, movement, ledger.Debit, cmd)
}

func (uc *ProcessWagerTransactionUseCase) processWin(ctx context.Context, tx *wager.Transaction, cmd ProcessWagerTransactionCommand) (ProcessResult, error) {
	w, err := uc.wallets.FindByIDForUpdate(ctx, cmd.WalletID)
	if err != nil {
		return ProcessResult{}, err
	}
	expectedVersion := w.Version()

	movement, err := w.Credit(cmd.Amount, cmd.Now)
	if err != nil {
		return ProcessResult{}, err
	}

	if cmd.ReferenceExternalTransactionID != "" {
		if ref, err := uc.txs.FindByProviderAndExternalID(ctx, cmd.ProviderID, cmd.ReferenceExternalTransactionID); err == nil {
			_ = tx.AttachResolvedReference(ref.ID())
		}
	}

	return uc.commitMovement(ctx, tx, w, expectedVersion, movement, ledger.Credit, cmd)
}

func (uc *ProcessWagerTransactionUseCase) processLoss(ctx context.Context, tx *wager.Transaction, cmd ProcessWagerTransactionCommand) (ProcessResult, error) {
	w, err := uc.wallets.FindByID(ctx, cmd.WalletID)
	if err != nil {
		return ProcessResult{}, err
	}
	balance := w.Balance()

	if err := tx.MarkProcessed(balance, cmd.Now); err != nil {
		return ProcessResult{}, err
	}
	if err := uc.txs.Save(ctx, tx); err != nil {
		return ProcessResult{}, err
	}
	if err := uc.appendProcessedEvent(ctx, tx, cmd); err != nil {
		return ProcessResult{}, err
	}
	return resultFromTransaction(tx, false), nil
}

func (uc *ProcessWagerTransactionUseCase) processReversal(ctx context.Context, tx *wager.Transaction, cmd ProcessWagerTransactionCommand, expectedReferenceKind wager.Kind) (ProcessResult, error) {
	ref, err := uc.txs.FindByProviderAndExternalID(ctx, cmd.ProviderID, cmd.ReferenceExternalTransactionID)
	if err != nil && !errors.Is(err, domainerr.ErrNotFound) {
		return ProcessResult{}, err
	}
	if ref == nil {
		if err := tx.MarkPendingReference(cmd.Now); err != nil {
			return ProcessResult{}, err
		}
		if err := uc.txs.Save(ctx, tx); err != nil {
			return ProcessResult{}, err
		}
		if err := uc.appendPendingReferenceEvent(ctx, tx, cmd); err != nil {
			return ProcessResult{}, err
		}
		return resultFromTransaction(tx, false), nil
	}

	if code, ok := validateReversalReference(cmd, ref, expectedReferenceKind); !ok {
		return uc.rejectAndSave(ctx, tx, cmd, code)
	}

	refundCount, err := uc.txs.CountSuccessfulReversals(ctx, ref.ID(), wager.Refund)
	if err != nil {
		return ProcessResult{}, err
	}
	rollbackCount, err := uc.txs.CountSuccessfulReversals(ctx, ref.ID(), wager.Rollback)
	if err != nil {
		return ProcessResult{}, err
	}
	if refundCount+rollbackCount > 0 {
		return uc.rejectAndSave(ctx, tx, cmd, domainerr.FailureDuplicateReversal)
	}

	w, err := uc.wallets.FindByIDForUpdate(ctx, cmd.WalletID)
	if err != nil {
		return ProcessResult{}, err
	}
	expectedVersion := w.Version()

	direction := reversalDirection(ref.Kind())
	var movement wallet.Movement
	if direction == ledger.Credit {
		movement, err = w.Credit(cmd.Amount, cmd.Now)
	} else {
		movement, err = w.Debit(cmd.Amount, cmd.Now)
	}
	if errors.Is(err, wallet.ErrInsufficientBalance) {
		return uc.rejectAndSave(ctx, tx, cmd, domainerr.FailureReversalExceedsBalance)
	}
	if err != nil {
		return ProcessResult{}, err
	}

	if err := tx.AttachResolvedReference(ref.ID()); err != nil {
		return ProcessResult{}, err
	}
	return uc.commitMovement(ctx, tx, w, expectedVersion, movement, direction, cmd)
}

func reversalDirection(originalKind wager.Kind) ledger.Direction {
	if originalKind == wager.Bet {
		return ledger.Credit
	}
	return ledger.Debit
}

func validateReversalReference(cmd ProcessWagerTransactionCommand, ref *wager.Transaction, expectedKind wager.Kind) (domainerr.FailureCode, bool) {
	if expectedKind != "" && ref.Kind() != expectedKind {
		return domainerr.FailureReferenceMismatch, false
	}
	if expectedKind == "" && ref.Kind() != wager.Bet && ref.Kind() != wager.Win && ref.Kind() != wager.Refund {
		return domainerr.FailureReferenceMismatch, false
	}
	if ref.Status() != wager.Processed {
		return domainerr.FailureReferenceNotProcessed, false
	}
	if ref.ProviderID() != cmd.ProviderID || ref.PlayerID() != cmd.PlayerID || ref.WalletID() != cmd.WalletID {
		return domainerr.FailureReferenceMismatch, false
	}
	if ref.RoundID() != cmd.RoundID {
		return domainerr.FailureReferenceMismatch, false
	}
	if ref.Money().Currency() != cmd.Amount.Currency() || !ref.Money().Equals(cmd.Amount) {
		return domainerr.FailureReferenceMismatch, false
	}
	return "", true
}

func (uc *ProcessWagerTransactionUseCase) commitMovement(ctx context.Context, tx *wager.Transaction, w *wallet.Wallet, expectedVersion int64, movement wallet.Movement, direction ledger.Direction, cmd ProcessWagerTransactionCommand) (ProcessResult, error) {
	entry, err := ledger.New(uuid.NewV7(), w.ID(), tx.ID(), direction, cmd.Amount, movement.BalanceBefore, movement.BalanceAfter, cmd.Now)
	if err != nil {
		return ProcessResult{}, err
	}
	if err := uc.ledgers.Append(ctx, entry); err != nil {
		return ProcessResult{}, err
	}
	if err := uc.wallets.Save(ctx, w, expectedVersion); err != nil {
		return ProcessResult{}, err
	}
	if err := tx.MarkProcessed(movement.BalanceAfter, cmd.Now); err != nil {
		return ProcessResult{}, err
	}
	if err := uc.txs.Save(ctx, tx); err != nil {
		return ProcessResult{}, err
	}
	if err := uc.appendProcessedEvent(ctx, tx, cmd); err != nil {
		return ProcessResult{}, err
	}
	if err := uc.appendBalanceChangedEvent(ctx, tx, w, movement, direction, cmd); err != nil {
		return ProcessResult{}, err
	}
	return resultFromTransaction(tx, false), nil
}

func (uc *ProcessWagerTransactionUseCase) rejectAndSave(ctx context.Context, tx *wager.Transaction, cmd ProcessWagerTransactionCommand, code domainerr.FailureCode) (ProcessResult, error) {
	if err := tx.MarkRejected(code, cmd.Now); err != nil {
		return ProcessResult{}, err
	}
	if err := uc.txs.Save(ctx, tx); err != nil {
		return ProcessResult{}, err
	}
	if err := uc.appendRejectedEvent(ctx, tx, cmd); err != nil {
		return ProcessResult{}, err
	}
	return resultFromTransaction(tx, false), nil
}

func (uc *ProcessWagerTransactionUseCase) appendProcessedEvent(ctx context.Context, tx *wager.Transaction, cmd ProcessWagerTransactionCommand) error {
	balance := money.Money{}
	if tx.ResultingBalance() != nil {
		balance = *tx.ResultingBalance()
	}
	env, err := events.NewWagerTransactionProcessed(correlationID(cmd), nil, events.WagerTransactionProcessedData{
		TransactionID:         tx.ID(),
		ProviderID:            tx.ProviderID(),
		ExternalTransactionID: tx.ExternalTransactionID(),
		WalletID:              tx.WalletID(),
		PlayerID:              tx.PlayerID(),
		Kind:                  tx.Kind(),
		Money:                 tx.Money(),
		ResultingBalance:      balance,
		ProcessedAt:           cmd.Now,
	}, cmd.Now)
	if err != nil {
		return err
	}
	return uc.outbox.Append(ctx, ports.OutboxRecord{
		EventID: env.EventID, AggregateID: env.AggregateID, EventType: env.EventType,
		Envelope: env, OccurredAt: env.OccurredAt, NextAttempt: cmd.Now,
	})
}

func (uc *ProcessWagerTransactionUseCase) appendRejectedEvent(ctx context.Context, tx *wager.Transaction, cmd ProcessWagerTransactionCommand) error {
	env, err := events.NewWagerTransactionRejected(correlationID(cmd), nil, events.WagerTransactionRejectedData{
		TransactionID:         tx.ID(),
		ProviderID:            tx.ProviderID(),
		ExternalTransactionID: tx.ExternalTransactionID(),
		WalletID:              tx.WalletID(),
		PlayerID:              tx.PlayerID(),
		Kind:                  tx.Kind(),
		FailureCode:           tx.FailureCode(),
		RejectedAt:            cmd.Now,
	}, cmd.Now)
	if err != nil {
		return err
	}
	return uc.outbox.Append(ctx, ports.OutboxRecord{
		EventID: env.EventID, AggregateID: env.AggregateID, EventType: env.EventType,
		Envelope: env, OccurredAt: env.OccurredAt, NextAttempt: cmd.Now,
	})
}

func (uc *ProcessWagerTransactionUseCase) appendPendingReferenceEvent(ctx context.Context, tx *wager.Transaction, cmd ProcessWagerTransactionCommand) error {
	env, err := events.NewWagerTransactionPendingReference(correlationID(cmd), nil, events.WagerTransactionPendingReferenceData{
		TransactionID:                  tx.ID(),
		ProviderID:                     tx.ProviderID(),
		ExternalTransactionID:          tx.ExternalTransactionID(),
		WalletID:                       tx.WalletID(),
		ReferenceExternalTransactionID: tx.ReferenceExternalTransactionID(),
	}, cmd.Now)
	if err != nil {
		return err
	}
	return uc.outbox.Append(ctx, ports.OutboxRecord{
		EventID: env.EventID, AggregateID: env.AggregateID, EventType: env.EventType,
		Envelope: env, OccurredAt: env.OccurredAt, NextAttempt: cmd.Now,
	})
}

func (uc *ProcessWagerTransactionUseCase) appendBalanceChangedEvent(ctx context.Context, tx *wager.Transaction, w *wallet.Wallet, movement wallet.Movement, direction ledger.Direction, cmd ProcessWagerTransactionCommand) error {
	env, err := events.NewWalletBalanceChanged(correlationID(cmd), nil, events.WalletBalanceChangedData{
		WalletID:      w.ID(),
		TransactionID: tx.ID(),
		Direction:     direction,
		Money:         cmd.Amount,
		BalanceBefore: movement.BalanceBefore,
		BalanceAfter:  movement.BalanceAfter,
		WalletVersion: movement.WalletVersion,
	}, cmd.Now)
	if err != nil {
		return err
	}
	return uc.outbox.Append(ctx, ports.OutboxRecord{
		EventID: env.EventID, AggregateID: env.AggregateID, EventType: env.EventType,
		Envelope: env, OccurredAt: env.OccurredAt, NextAttempt: cmd.Now,
	})
}

func correlationID(cmd ProcessWagerTransactionCommand) uuid.UUID {
	if cmd.CorrelationID == uuid.Nil() {
		return uuid.NewV7()
	}
	return cmd.CorrelationID
}

func canonicalPayloadFrom(cmd ProcessWagerTransactionCommand) CanonicalPayload {
	return CanonicalPayload{
		ProviderID:                     cmd.ProviderID,
		ExternalTransactionID:          cmd.ExternalTransactionID,
		PlayerID:                       cmd.PlayerID.String(),
		WalletID:                       cmd.WalletID.String(),
		RoundID:                        cmd.RoundID,
		GameID:                         cmd.GameID,
		Kind:                           string(cmd.Kind),
		Amount:                         cmd.Amount.String(),
		Currency:                       cmd.Amount.Currency(),
		ReferenceExternalTransactionID: cmd.ReferenceExternalTransactionID,
	}
}

func resultFromTransaction(tx *wager.Transaction, replay bool) ProcessResult {
	r := ProcessResult{
		TransactionID:    tx.ID(),
		FailureCode:      tx.FailureCode(),
		Balance:          tx.ResultingBalance(),
		IdempotentReplay: replay,
	}
	switch tx.Status() {
	case wager.Processed:
		r.Status = ResultProcessed
	case wager.Rejected:
		r.Status = ResultRejected
	case wager.PendingReference:
		r.Status = ResultPendingReference
	default:
		r.Status = ResultPending
	}
	return r
}
