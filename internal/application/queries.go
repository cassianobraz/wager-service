package application

import (
	"context"
	"github.com/cassianobraz/wager-service/internal/domain/ledger"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"github.com/cassianobraz/wager-service/internal/domain/wallet"
	"github.com/cassianobraz/wager-service/internal/ports"
	"uuid"
)

type QueryService struct {
	wallets ports.WalletRepository
	txs     ports.WagerTransactionRepository
	ledgers ports.LedgerRepository
}

func NewQueryService(wallets ports.WalletRepository, txs ports.WagerTransactionRepository, ledgers ports.LedgerRepository) *QueryService {
	return &QueryService{wallets: wallets, txs: txs, ledgers: ledgers}
}

func (q *QueryService) GetWallet(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	return q.wallets.FindByID(ctx, id)
}

func (q *QueryService) GetLedgerPage(ctx context.Context, walletID uuid.UUID, cursor string, limit int) ([]*ledger.Entry, string, error) {
	return q.ledgers.ListByWallet(ctx, walletID, cursor, limit)
}

func (q *QueryService) GetTransaction(ctx context.Context, id uuid.UUID) (*wager.Transaction, error) {
	return q.txs.FindByID(ctx, id)
}

func (q *QueryService) GetTransactionByExternalID(ctx context.Context, providerID, externalTransactionID string) (*wager.Transaction, error) {
	return q.txs.FindByProviderAndExternalID(ctx, providerID, externalTransactionID)
}
