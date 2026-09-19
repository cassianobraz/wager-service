package application_test

import (
	"context"
	"github.com/cassianobraz/wager-service/internal/domain/domainerr"
	"github.com/cassianobraz/wager-service/internal/domain/ledger"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"github.com/cassianobraz/wager-service/internal/domain/wallet"
	"github.com/cassianobraz/wager-service/internal/ports"
	"sync"
	"time"
	"uuid"
)

type lockTrackerKey struct{}

type lockTracker struct {
	mu   sync.Mutex
	held []*sync.Mutex
}

func withLockTracker(ctx context.Context) context.Context {
	return context.WithValue(ctx, lockTrackerKey{}, &lockTracker{})
}

func trackLock(ctx context.Context, m *sync.Mutex) {
	if lt, ok := ctx.Value(lockTrackerKey{}).(*lockTracker); ok {
		lt.mu.Lock()
		lt.held = append(lt.held, m)
		lt.mu.Unlock()
	}
}

func releaseTrackedLocks(ctx context.Context) {
	if lt, ok := ctx.Value(lockTrackerKey{}).(*lockTracker); ok {
		lt.mu.Lock()
		defer lt.mu.Unlock()
		for _, m := range lt.held {
			m.Unlock()
		}
		lt.held = nil
	}
}

type fakeTxManager struct{}

func (f *fakeTxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	ctx = withLockTracker(ctx)
	err := fn(ctx)
	releaseTrackedLocks(ctx)
	return err
}

type fakeWalletRepository struct {
	mu    sync.Mutex
	byID  map[uuid.UUID]*wallet.Wallet
	locks map[uuid.UUID]*sync.Mutex
}

func newFakeWalletRepository() *fakeWalletRepository {
	return &fakeWalletRepository{
		byID:  make(map[uuid.UUID]*wallet.Wallet),
		locks: make(map[uuid.UUID]*sync.Mutex),
	}
}

func (r *fakeWalletRepository) lockFor(id uuid.UUID) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.locks[id]
	if !ok {
		m = &sync.Mutex{}
		r.locks[id] = m
	}
	return m
}

func (r *fakeWalletRepository) snapshot(id uuid.UUID) (*wallet.Wallet, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.byID[id]
	if !ok {
		return nil, domainerr.ErrNotFound
	}
	return copyWallet(w), nil
}

func (r *fakeWalletRepository) FindByID(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	return r.snapshot(id)
}

func (r *fakeWalletRepository) FindByPlayerAndCurrency(ctx context.Context, playerID uuid.UUID, currency string) (*wallet.Wallet, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, w := range r.byID {
		if w.PlayerID() == playerID && w.Currency() == currency {
			return copyWallet(w), nil
		}
	}
	return nil, domainerr.ErrNotFound
}

func (r *fakeWalletRepository) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	lock := r.lockFor(id)
	lock.Lock()
	trackLock(ctx, lock)
	w, err := r.snapshot(id)
	if err != nil {
		lock.Unlock()
		return nil, err
	}
	return w, nil
}

func (r *fakeWalletRepository) Create(ctx context.Context, w *wallet.Wallet) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.byID {
		if existing.PlayerID() == w.PlayerID() && existing.Currency() == w.Currency() {
			return domainerr.ErrConflict
		}
	}
	r.byID[w.ID()] = copyWallet(w)
	return nil
}

func (r *fakeWalletRepository) Save(ctx context.Context, w *wallet.Wallet, expectedVersion int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.byID[w.ID()]
	if !ok {
		return domainerr.ErrNotFound
	}
	if current.Version() != expectedVersion {
		return ports.ErrOptimisticLock
	}
	r.byID[w.ID()] = copyWallet(w)
	return nil
}

func copyWallet(w *wallet.Wallet) *wallet.Wallet {
	cp, err := wallet.Rehydrate(w.ID(), w.PlayerID(), w.Balance(), w.Version(), w.CreatedAt(), w.UpdatedAt())
	if err != nil {
		panic(err)
	}
	return cp
}

type fakeWagerTransactionRepository struct {
	mu               sync.Mutex
	byID             map[uuid.UUID]*wager.Transaction
	pendingRetryAt   map[uuid.UUID]time.Time
	idempotencyLocks map[string]*sync.Mutex
}

func newFakeWagerTransactionRepository() *fakeWagerTransactionRepository {
	return &fakeWagerTransactionRepository{
		byID:             make(map[uuid.UUID]*wager.Transaction),
		pendingRetryAt:   make(map[uuid.UUID]time.Time),
		idempotencyLocks: make(map[string]*sync.Mutex),
	}
}

func (r *fakeWagerTransactionRepository) lockForIdempotencyKey(providerID, idempotencyKey string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := providerID + "\x00" + idempotencyKey
	m, ok := r.idempotencyLocks[key]
	if !ok {
		m = &sync.Mutex{}
		r.idempotencyLocks[key] = m
	}
	return m
}

func (r *fakeWagerTransactionRepository) snapshot(tx *wager.Transaction) *wager.Transaction {
	cp, err := wager.Rehydrate(wager.RehydrateParams{
		ID: tx.ID(), ProviderID: tx.ProviderID(), ExternalTransactionID: tx.ExternalTransactionID(),
		IdempotencyKey: tx.IdempotencyKey(), PayloadHash: tx.PayloadHash(), WalletID: tx.WalletID(),
		PlayerID: tx.PlayerID(), RoundID: tx.RoundID(), GameID: tx.GameID(), Kind: tx.Kind(),
		Money: tx.Money(), ReferenceExternalTransactionID: tx.ReferenceExternalTransactionID(),
		ResolvedReferenceTransactionID: tx.ResolvedReferenceTransactionID(), Status: tx.Status(),
		FailureCode: tx.FailureCode(), ResultingBalance: tx.ResultingBalance(),
		CreatedAt: tx.CreatedAt(), UpdatedAt: tx.UpdatedAt(),
	})
	if err != nil {
		panic(err)
	}
	return cp
}

func (r *fakeWagerTransactionRepository) FindByID(ctx context.Context, id uuid.UUID) (*wager.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tx, ok := r.byID[id]
	if !ok {
		return nil, domainerr.ErrNotFound
	}
	return r.snapshot(tx), nil
}

func (r *fakeWagerTransactionRepository) FindByProviderAndExternalID(ctx context.Context, providerID, externalTransactionID string) (*wager.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, tx := range r.byID {
		if tx.ProviderID() == providerID && tx.ExternalTransactionID() == externalTransactionID {
			return r.snapshot(tx), nil
		}
	}
	return nil, domainerr.ErrNotFound
}

func (r *fakeWagerTransactionRepository) FindByIdempotencyKey(ctx context.Context, providerID, idempotencyKey string) (*wager.Transaction, error) {
	// Wait for any in-flight Create of this key to finish, mirroring how a
	// plain SELECT under Postgres read-committed isolation never observes an
	// uncommitted concurrent INSERT.
	lock := r.lockForIdempotencyKey(providerID, idempotencyKey)
	lock.Lock()
	lock.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, tx := range r.byID {
		if tx.ProviderID() == providerID && tx.IdempotencyKey() == idempotencyKey {
			return r.snapshot(tx), nil
		}
	}
	return nil, domainerr.ErrNotFound
}

func (r *fakeWagerTransactionRepository) Create(ctx context.Context, tx *wager.Transaction) error {
	lock := r.lockForIdempotencyKey(tx.ProviderID(), tx.IdempotencyKey())
	lock.Lock()

	r.mu.Lock()
	conflict := false
	if _, exists := r.byID[tx.ID()]; exists {
		conflict = true
	}
	if !conflict {
		for _, existing := range r.byID {
			if existing.ProviderID() == tx.ProviderID() && existing.IdempotencyKey() == tx.IdempotencyKey() {
				conflict = true
				break
			}
		}
	}
	if conflict {
		r.mu.Unlock()
		lock.Unlock()
		return domainerr.ErrConflict
	}
	r.byID[tx.ID()] = r.snapshot(tx)
	r.mu.Unlock()

	// Hold the key lock until the enclosing use-case transaction commits, so
	// concurrent readers/writers of this idempotency key see this row only
	// once it reaches its final state.
	trackLock(ctx, lock)
	return nil
}

func (r *fakeWagerTransactionRepository) Save(ctx context.Context, tx *wager.Transaction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[tx.ID()]; !ok {
		return domainerr.ErrNotFound
	}
	r.byID[tx.ID()] = r.snapshot(tx)
	return nil
}

func (r *fakeWagerTransactionRepository) ListPendingReferenceDue(ctx context.Context, now time.Time, limit int) ([]*wager.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var due []*wager.Transaction
	for id, tx := range r.byID {
		if tx.Status() != wager.PendingReference {
			continue
		}
		retryAt, scheduled := r.pendingRetryAt[id]
		if scheduled && retryAt.After(now) {
			continue
		}
		due = append(due, r.snapshot(tx))
		if len(due) >= limit {
			break
		}
	}
	return due, nil
}

func (r *fakeWagerTransactionRepository) RecordPendingReferenceAttempt(ctx context.Context, transactionID uuid.UUID, attempts int, nextRetryAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pendingRetryAt[transactionID] = nextRetryAt
	return nil
}

func (r *fakeWagerTransactionRepository) CountSuccessfulReversals(ctx context.Context, referencedTransactionID uuid.UUID, kind wager.Kind) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, tx := range r.byID {
		if tx.Kind() != kind || tx.Status() != wager.Processed {
			continue
		}
		if ref := tx.ResolvedReferenceTransactionID(); ref != nil && *ref == referencedTransactionID {
			count++
		}
	}
	return count, nil
}

type fakeLedgerRepository struct {
	mu      sync.Mutex
	entries []*ledger.Entry
}

func newFakeLedgerRepository() *fakeLedgerRepository {
	return &fakeLedgerRepository{}
}

func (r *fakeLedgerRepository) Append(ctx context.Context, e *ledger.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.entries {
		if existing.WalletID() == e.WalletID() && existing.TransactionID() == e.TransactionID() {
			return domainerr.ErrConflict
		}
	}
	r.entries = append(r.entries, e)
	return nil
}

func (r *fakeLedgerRepository) ListByWallet(ctx context.Context, walletID uuid.UUID, cursor string, limit int) ([]*ledger.Entry, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*ledger.Entry
	for _, e := range r.entries {
		if e.WalletID() == walletID {
			out = append(out, e)
		}
	}
	return out, "", nil
}

func (r *fakeLedgerRepository) SumByWallet(ctx context.Context, walletID uuid.UUID) (int64, string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var sum int64
	var currency string
	count := 0
	for _, e := range r.entries {
		if e.WalletID() != walletID {
			continue
		}
		signed, err := e.SignedAmount()
		if err != nil {
			return 0, "", 0, err
		}
		sum += signed.MinorUnits()
		currency = signed.Currency()
		count++
	}
	return sum, currency, count, nil
}

type fakeInboxRepository struct {
	mu      sync.Mutex
	records map[string]*ports.InboxRecord
}

func newFakeInboxRepository() *fakeInboxRepository {
	return &fakeInboxRepository{records: make(map[string]*ports.InboxRecord)}
}

func inboxKey(consumerName, messageID string) string { return consumerName + "|" + messageID }

func (r *fakeInboxRepository) Find(ctx context.Context, consumerName, messageID string) (*ports.InboxRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[inboxKey(consumerName, messageID)]
	if !ok {
		return nil, domainerr.ErrNotFound
	}
	cp := *rec
	return &cp, nil
}

func (r *fakeInboxRepository) Insert(ctx context.Context, rec ports.InboxRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := inboxKey(rec.ConsumerName, rec.MessageID)
	if _, exists := r.records[key]; exists {
		return domainerr.ErrConflict
	}
	cp := rec
	r.records[key] = &cp
	return nil
}

func (r *fakeInboxRepository) MarkCompleted(ctx context.Context, consumerName, messageID string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[inboxKey(consumerName, messageID)]
	if !ok {
		return domainerr.ErrNotFound
	}
	rec.CompletedAt = &at
	return nil
}

type fakeOutboxRepository struct {
	mu      sync.Mutex
	records []ports.OutboxRecord
}

func newFakeOutboxRepository() *fakeOutboxRepository {
	return &fakeOutboxRepository{}
}

func (r *fakeOutboxRepository) Append(ctx context.Context, rec ports.OutboxRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
	return nil
}

func (r *fakeOutboxRepository) ClaimPending(ctx context.Context, now time.Time, limit int) ([]ports.OutboxRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ports.OutboxRecord
	for _, rec := range r.records {
		if rec.PublishedAt == nil {
			out = append(out, rec)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (r *fakeOutboxRepository) MarkPublished(ctx context.Context, eventID uuid.UUID, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.records {
		if r.records[i].EventID == eventID {
			r.records[i].PublishedAt = &at
			return nil
		}
	}
	return domainerr.ErrNotFound
}

func (r *fakeOutboxRepository) ScheduleRetry(ctx context.Context, eventID uuid.UUID, attempts int, nextAttempt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.records {
		if r.records[i].EventID == eventID {
			r.records[i].Attempts = attempts
			r.records[i].NextAttempt = nextAttempt
			return nil
		}
	}
	return domainerr.ErrNotFound
}

func (r *fakeOutboxRepository) byType(eventType string) []ports.OutboxRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ports.OutboxRecord
	for _, rec := range r.records {
		if rec.EventType == eventType {
			out = append(out, rec)
		}
	}
	return out
}
