package application

import (
	"context"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"github.com/cassianobraz/wager-service/internal/ports"
	"uuid"
)

type ReconciliationResult struct {
	WalletID          uuid.UUID
	StoredBalance     money.Money
	CalculatedBalance money.Money
	Difference        money.Money
	Consistent        bool
	CheckedEntries    int
}

type ReconciliationUseCase struct {
	tx      ports.TxManager
	wallets ports.WalletRepository
	ledgers ports.LedgerRepository
}

func NewReconciliationUseCase(tx ports.TxManager, wallets ports.WalletRepository, ledgers ports.LedgerRepository) *ReconciliationUseCase {
	return &ReconciliationUseCase{tx: tx, wallets: wallets, ledgers: ledgers}
}

func (uc *ReconciliationUseCase) Execute(ctx context.Context, walletID uuid.UUID) (ReconciliationResult, error) {
	var result ReconciliationResult
	err := uc.tx.WithinTx(ctx, func(ctx context.Context) error {
		w, err := uc.wallets.FindByID(ctx, walletID)
		if err != nil {
			return err
		}

		minorUnits, currency, count, err := uc.ledgers.SumByWallet(ctx, walletID)
		if err != nil {
			return err
		}
		calculated, err := money.FromMinorUnits(minorUnits, currency)
		if err != nil {
			return err
		}

		difference, err := w.Balance().Subtract(calculated)
		if err != nil {
			return err
		}

		result = ReconciliationResult{
			WalletID:          walletID,
			StoredBalance:     w.Balance(),
			CalculatedBalance: calculated,
			Difference:        difference,
			Consistent:        difference.IsZero(),
			CheckedEntries:    count,
		}
		return nil
	})
	if err != nil {
		return ReconciliationResult{}, err
	}
	return result, nil
}
