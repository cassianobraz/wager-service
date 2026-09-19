package application

import (
	"context"
	"github.com/cassianobraz/wager-service/internal/domain/events"
	"github.com/cassianobraz/wager-service/internal/domain/ledger"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"github.com/cassianobraz/wager-service/internal/domain/wallet"
	"github.com/cassianobraz/wager-service/internal/ports"
	"time"
	"uuid"
)

type OpenWalletCommand struct {
	PlayerID       uuid.UUID
	InitialBalance money.Money
	CorrelationID  uuid.UUID
	Now            time.Time
}

type OpenWalletResult struct {
	WalletID uuid.UUID
	Balance  money.Money
	Version  int64
}

type OpenWalletUseCase struct {
	tx      ports.TxManager
	wallets ports.WalletRepository
	txs     ports.WagerTransactionRepository
	ledgers ports.LedgerRepository
	outbox  ports.OutboxRepository
}

func NewOpenWalletUseCase(tx ports.TxManager, wallets ports.WalletRepository, txs ports.WagerTransactionRepository, ledgers ports.LedgerRepository, outbox ports.OutboxRepository) *OpenWalletUseCase {
	return &OpenWalletUseCase{tx: tx, wallets: wallets, txs: txs, ledgers: ledgers, outbox: outbox}
}

func (uc *OpenWalletUseCase) Execute(ctx context.Context, cmd OpenWalletCommand) (OpenWalletResult, error) {
	var result OpenWalletResult
	err := uc.tx.WithinTx(ctx, func(ctx context.Context) error {
		walletID := uuid.NewV7()
		w, err := wallet.New(walletID, cmd.PlayerID, cmd.InitialBalance, cmd.Now)
		if err != nil {
			return err
		}
		if err := uc.wallets.Create(ctx, w); err != nil {
			return err
		}

		if cmd.InitialBalance.IsPositive() {
			zero, err := money.Zero(cmd.InitialBalance.Currency())
			if err != nil {
				return err
			}
			openingID := uuid.NewV7()
			openingTx, err := wager.NewOpening(openingID, walletID, cmd.PlayerID, cmd.InitialBalance, cmd.Now)
			if err != nil {
				return err
			}
			if err := uc.txs.Create(ctx, openingTx); err != nil {
				return err
			}

			entry, err := ledger.New(uuid.NewV7(), walletID, openingID, ledger.Credit, cmd.InitialBalance, zero, cmd.InitialBalance, cmd.Now)
			if err != nil {
				return err
			}
			if err := uc.ledgers.Append(ctx, entry); err != nil {
				return err
			}

			if err := uc.appendOpeningEvents(ctx, w, openingTx, cmd); err != nil {
				return err
			}
		}

		result = OpenWalletResult{WalletID: w.ID(), Balance: w.Balance(), Version: w.Version()}
		return nil
	})
	if err != nil {
		return OpenWalletResult{}, err
	}
	return result, nil
}

func (uc *OpenWalletUseCase) appendOpeningEvents(ctx context.Context, w *wallet.Wallet, openingTx *wager.Transaction, cmd OpenWalletCommand) error {
	correlationID := cmd.CorrelationID
	if correlationID == uuid.Nil() {
		correlationID = uuid.NewV7()
	}

	processedEnv, err := events.NewWagerTransactionProcessed(correlationID, nil, events.WagerTransactionProcessedData{
		TransactionID:    openingTx.ID(),
		WalletID:         w.ID(),
		PlayerID:         w.PlayerID(),
		Kind:             wager.Opening,
		Money:            cmd.InitialBalance,
		ResultingBalance: cmd.InitialBalance,
		ProcessedAt:      cmd.Now,
	}, cmd.Now)
	if err != nil {
		return err
	}
	if err := uc.outbox.Append(ctx, ports.OutboxRecord{
		EventID: processedEnv.EventID, AggregateID: processedEnv.AggregateID, EventType: processedEnv.EventType,
		Envelope: processedEnv, OccurredAt: processedEnv.OccurredAt, NextAttempt: cmd.Now,
	}); err != nil {
		return err
	}

	zero, err := money.Zero(cmd.InitialBalance.Currency())
	if err != nil {
		return err
	}
	balanceEnv, err := events.NewWalletBalanceChanged(correlationID, nil, events.WalletBalanceChangedData{
		WalletID:      w.ID(),
		TransactionID: openingTx.ID(),
		Direction:     ledger.Credit,
		Money:         cmd.InitialBalance,
		BalanceBefore: zero,
		BalanceAfter:  cmd.InitialBalance,
		WalletVersion: w.Version(),
	}, cmd.Now)
	if err != nil {
		return err
	}
	return uc.outbox.Append(ctx, ports.OutboxRecord{
		EventID: balanceEnv.EventID, AggregateID: balanceEnv.AggregateID, EventType: balanceEnv.EventType,
		Envelope: balanceEnv, OccurredAt: balanceEnv.OccurredAt, NextAttempt: cmd.Now,
	})
}
