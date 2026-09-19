package ports

import (
	"context"
	"github.com/cassianobraz/wager-service/internal/domain/events"
	"github.com/cassianobraz/wager-service/internal/domain/ledger"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"github.com/cassianobraz/wager-service/internal/domain/wallet"
	"time"
	"uuid"
)

type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type WalletRepository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error)
	FindByPlayerAndCurrency(ctx context.Context, playerID uuid.UUID, currency string) (*wallet.Wallet, error)
	FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error)
	Create(ctx context.Context, w *wallet.Wallet) error
	Save(ctx context.Context, w *wallet.Wallet, expectedVersion int64) error
}

type WagerTransactionRepository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*wager.Transaction, error)
	FindByProviderAndExternalID(ctx context.Context, providerID, externalTransactionID string) (*wager.Transaction, error)
	FindByIdempotencyKey(ctx context.Context, providerID, idempotencyKey string) (*wager.Transaction, error)
	Create(ctx context.Context, tx *wager.Transaction) error
	Save(ctx context.Context, tx *wager.Transaction) error
	ListPendingReferenceDue(ctx context.Context, now time.Time, limit int) ([]*wager.Transaction, error)
	RecordPendingReferenceAttempt(ctx context.Context, transactionID uuid.UUID, attempts int, nextRetryAt time.Time) error
	CountSuccessfulReversals(ctx context.Context, referencedTransactionID uuid.UUID, kind wager.Kind) (int, error)
}

type LedgerRepository interface {
	Append(ctx context.Context, e *ledger.Entry) error
	ListByWallet(ctx context.Context, walletID uuid.UUID, cursor string, limit int) (entries []*ledger.Entry, nextCursor string, err error)
	SumByWallet(ctx context.Context, walletID uuid.UUID) (minorUnits int64, currency string, count int, err error)
}

type InboxRecord struct {
	ConsumerName string
	MessageID    string
	PayloadHash  string
	ReceivedAt   time.Time
	CompletedAt  *time.Time
}

type InboxRepository interface {
	Find(ctx context.Context, consumerName, messageID string) (*InboxRecord, error)
	Insert(ctx context.Context, rec InboxRecord) error
	MarkCompleted(ctx context.Context, consumerName, messageID string, at time.Time) error
}

type OutboxRecord struct {
	EventID     uuid.UUID
	AggregateID uuid.UUID
	EventType   string
	Envelope    events.Envelope
	OccurredAt  time.Time
	Attempts    int
	NextAttempt time.Time
	PublishedAt *time.Time
}

type OutboxRepository interface {
	Append(ctx context.Context, rec OutboxRecord) error
	ClaimPending(ctx context.Context, now time.Time, limit int) ([]OutboxRecord, error)
	MarkPublished(ctx context.Context, eventID uuid.UUID, at time.Time) error
	ScheduleRetry(ctx context.Context, eventID uuid.UUID, attempts int, nextAttempt time.Time) error
}

type EventPublisher interface {
	Publish(ctx context.Context, rec OutboxRecord) error
}
