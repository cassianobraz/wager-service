package application

import (
	"context"
	"errors"
	"github.com/cassianobraz/wager-service/internal/domain/domainerr"
	"github.com/cassianobraz/wager-service/internal/domain/ledger"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"github.com/cassianobraz/wager-service/internal/domain/wallet"
	"github.com/cassianobraz/wager-service/internal/ports"
	"time"
	"uuid"
)

type PendingReferenceRetryPolicy struct {
	MaxAttempts int
	MaxAge      time.Duration
	BaseBackoff time.Duration
}

func DefaultPendingReferenceRetryPolicy() PendingReferenceRetryPolicy {
	return PendingReferenceRetryPolicy{MaxAttempts: 10, MaxAge: 24 * time.Hour, BaseBackoff: 2 * time.Second}
}

func (p PendingReferenceRetryPolicy) NextBackoff(attempts int) time.Duration {
	backoff := p.BaseBackoff
	for i := 0; i < attempts && backoff < 5*time.Minute; i++ {
		backoff *= 2
	}
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	return backoff
}

type ResolvePendingReferencesUseCase struct {
	tx      ports.TxManager
	wallets ports.WalletRepository
	txs     ports.WagerTransactionRepository
	ledgers ports.LedgerRepository
	outbox  ports.OutboxRepository
	policy  PendingReferenceRetryPolicy
}

func NewResolvePendingReferencesUseCase(tx ports.TxManager, wallets ports.WalletRepository, txs ports.WagerTransactionRepository, ledgers ports.LedgerRepository, outbox ports.OutboxRepository, policy PendingReferenceRetryPolicy) *ResolvePendingReferencesUseCase {
	return &ResolvePendingReferencesUseCase{tx: tx, wallets: wallets, txs: txs, ledgers: ledgers, outbox: outbox, policy: policy}
}

func (uc *ResolvePendingReferencesUseCase) RunOnce(ctx context.Context, now time.Time, limit int) (resolved, rescheduled int, err error) {
	due, err := uc.txs.ListPendingReferenceDue(ctx, now, limit)
	if err != nil {
		return 0, 0, err
	}
	for _, tx := range due {
		didResolve, err := uc.attempt(ctx, tx, now)
		if err != nil {
			return resolved, rescheduled, err
		}
		if didResolve {
			resolved++
		} else {
			rescheduled++
		}
	}
	return resolved, rescheduled, nil
}

func (uc *ResolvePendingReferencesUseCase) attempt(ctx context.Context, tx *wager.Transaction, now time.Time) (bool, error) {
	resolved := false
	err := uc.tx.WithinTx(ctx, func(ctx context.Context) error {
		ref, err := uc.txs.FindByProviderAndExternalID(ctx, tx.ProviderID(), tx.ReferenceExternalTransactionID())
		if err != nil && !errors.Is(err, domainerr.ErrNotFound) {
			return err
		}
		if ref == nil {
			expired, err := uc.rescheduleOrExpire(ctx, tx, now)
			resolved = expired
			return err
		}

		expectedKind := wager.Kind("")
		if tx.Kind() == wager.Refund {
			expectedKind = wager.Bet
		}
		probe := ProcessWagerTransactionCommand{
			ProviderID: tx.ProviderID(), PlayerID: tx.PlayerID(), WalletID: tx.WalletID(),
			RoundID: tx.RoundID(), Amount: tx.Money(), Now: now,
		}
		if code, ok := validateReversalReference(probe, ref, expectedKind); !ok {
			resolved = true
			return uc.rejectPending(ctx, tx, code, now)
		}

		refundCount, err := uc.txs.CountSuccessfulReversals(ctx, ref.ID(), wager.Refund)
		if err != nil {
			return err
		}
		rollbackCount, err := uc.txs.CountSuccessfulReversals(ctx, ref.ID(), wager.Rollback)
		if err != nil {
			return err
		}
		if refundCount+rollbackCount > 0 {
			resolved = true
			return uc.rejectPending(ctx, tx, domainerr.FailureDuplicateReversal, now)
		}

		w, err := uc.wallets.FindByIDForUpdate(ctx, tx.WalletID())
		if err != nil {
			return err
		}
		expectedVersion := w.Version()

		direction := reversalDirection(ref.Kind())
		var movement wallet.Movement
		if direction == ledger.Credit {
			movement, err = w.Credit(tx.Money(), now)
		} else {
			movement, err = w.Debit(tx.Money(), now)
		}
		if errors.Is(err, wallet.ErrInsufficientBalance) {
			resolved = true
			return uc.rejectPending(ctx, tx, domainerr.FailureReversalExceedsBalance, now)
		}
		if err != nil {
			return err
		}

		entry, err := ledger.New(uuid.NewV7(), w.ID(), tx.ID(), direction, tx.Money(), movement.BalanceBefore, movement.BalanceAfter, now)
		if err != nil {
			return err
		}
		if err := uc.ledgers.Append(ctx, entry); err != nil {
			return err
		}
		if err := uc.wallets.Save(ctx, w, expectedVersion); err != nil {
			return err
		}
		if err := tx.AttachResolvedReference(ref.ID()); err != nil {
			return err
		}
		if err := tx.MarkProcessed(movement.BalanceAfter, now); err != nil {
			return err
		}
		if err := uc.txs.Save(ctx, tx); err != nil {
			return err
		}

		resolved = true
		helper := &ProcessWagerTransactionUseCase{outbox: uc.outbox}
		cmd := ProcessWagerTransactionCommand{Now: now, Amount: tx.Money()}
		if err := helper.appendProcessedEvent(ctx, tx, cmd); err != nil {
			return err
		}
		return helper.appendBalanceChangedEvent(ctx, tx, w, movement, direction, cmd)
	})
	return resolved, err
}

func (uc *ResolvePendingReferencesUseCase) rescheduleOrExpire(ctx context.Context, tx *wager.Transaction, now time.Time) (bool, error) {
	elapsed := now.Sub(tx.CreatedAt())
	if elapsed >= uc.policy.MaxAge {
		return true, uc.rejectPending(ctx, tx, domainerr.FailureReferenceNotFound, now)
	}
	nextRetry := now.Add(uc.policy.BaseBackoff)
	return false, uc.txs.RecordPendingReferenceAttempt(ctx, tx.ID(), 1, nextRetry)
}

func (uc *ResolvePendingReferencesUseCase) rejectPending(ctx context.Context, tx *wager.Transaction, code domainerr.FailureCode, now time.Time) error {
	if err := tx.MarkRejected(code, now); err != nil {
		return err
	}
	if err := uc.txs.Save(ctx, tx); err != nil {
		return err
	}
	helper := &ProcessWagerTransactionUseCase{outbox: uc.outbox}
	cmd := ProcessWagerTransactionCommand{Now: now}
	return helper.appendRejectedEvent(ctx, tx, cmd)
}
