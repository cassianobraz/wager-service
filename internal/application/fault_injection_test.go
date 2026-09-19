package application_test

import (
	"context"
	"errors"
	"github.com/cassianobraz/wager-service/internal/domain/ledger"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"github.com/cassianobraz/wager-service/internal/domain/wallet"
	"github.com/cassianobraz/wager-service/internal/ports"
	"uuid"
)

var errBoom = errors.New("boom: injected infrastructure failure")

type faultyWallets struct {
	*fakeWalletRepository
	failSave          bool
	failFindForUpdate bool
	failFindByID      bool
}

func (f *faultyWallets) Save(ctx context.Context, w *wallet.Wallet, expectedVersion int64) error {
	if f.failSave {
		return errBoom
	}
	return f.fakeWalletRepository.Save(ctx, w, expectedVersion)
}

func (f *faultyWallets) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	if f.failFindForUpdate {
		return nil, errBoom
	}
	return f.fakeWalletRepository.FindByIDForUpdate(ctx, id)
}

func (f *faultyWallets) FindByID(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	if f.failFindByID {
		return nil, errBoom
	}
	return f.fakeWalletRepository.FindByID(ctx, id)
}

type faultyLedgers struct {
	*fakeLedgerRepository
	failAppend bool
}

func (f *faultyLedgers) Append(ctx context.Context, e *ledger.Entry) error {
	if f.failAppend {
		return errBoom
	}
	return f.fakeLedgerRepository.Append(ctx, e)
}

type faultyOutbox struct {
	*fakeOutboxRepository
	failAppend bool
}

func (f *faultyOutbox) Append(ctx context.Context, rec ports.OutboxRecord) error {
	if f.failAppend {
		return errBoom
	}
	return f.fakeOutboxRepository.Append(ctx, rec)
}

type faultyTxs struct {
	*fakeWagerTransactionRepository
	failSave                     bool
	failFindByProviderExternalID bool
	failFindByIdempotencyKey     bool
}

func (f *faultyTxs) Save(ctx context.Context, tx *wager.Transaction) error {
	if f.failSave {
		return errBoom
	}
	return f.fakeWagerTransactionRepository.Save(ctx, tx)
}

func (f *faultyTxs) FindByProviderAndExternalID(ctx context.Context, providerID, externalTransactionID string) (*wager.Transaction, error) {
	if f.failFindByProviderExternalID {
		return nil, errBoom
	}
	return f.fakeWagerTransactionRepository.FindByProviderAndExternalID(ctx, providerID, externalTransactionID)
}

func (f *faultyTxs) FindByIdempotencyKey(ctx context.Context, providerID, idempotencyKey string) (*wager.Transaction, error) {
	if f.failFindByIdempotencyKey {
		return nil, errBoom
	}
	return f.fakeWagerTransactionRepository.FindByIdempotencyKey(ctx, providerID, idempotencyKey)
}

type faultyInbox struct {
	*fakeInboxRepository
	failFind bool
}

func (f *faultyInbox) Find(ctx context.Context, consumerName, messageID string) (*ports.InboxRecord, error) {
	if f.failFind {
		return nil, errBoom
	}
	return f.fakeInboxRepository.Find(ctx, consumerName, messageID)
}
