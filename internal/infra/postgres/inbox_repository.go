package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/cassianobraz/wager-service/internal/domain/domainerr"
	"github.com/cassianobraz/wager-service/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type InboxRepository struct {
	pool *pgxpool.Pool
}

func NewInboxRepository(pool *pgxpool.Pool) *InboxRepository {
	return &InboxRepository{pool: pool}
}

func (r *InboxRepository) Find(ctx context.Context, consumerName, messageID string) (*ports.InboxRecord, error) {
	var rec ports.InboxRecord
	var completedAt *time.Time
	err := db(ctx, r.pool).QueryRow(ctx, `
		SELECT consumer_name, message_id, payload_hash, received_at, completed_at
		FROM inbox_records WHERE consumer_name = $1 AND message_id = $2`,
		consumerName, messageID,
	).Scan(&rec.ConsumerName, &rec.MessageID, &rec.PayloadHash, &rec.ReceivedAt, &completedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domainerr.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: find inbox record: %w", err)
	}
	rec.CompletedAt = completedAt
	return &rec, nil
}

func (r *InboxRepository) Insert(ctx context.Context, rec ports.InboxRecord) error {
	_, err := db(ctx, r.pool).Exec(ctx, `
		INSERT INTO inbox_records (consumer_name, message_id, payload_hash, received_at, completed_at)
		VALUES ($1, $2, $3, $4, $5)`,
		rec.ConsumerName, rec.MessageID, rec.PayloadHash, rec.ReceivedAt, rec.CompletedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return domainerr.ErrConflict
		}
		return fmt.Errorf("postgres: insert inbox record: %w", err)
	}
	return nil
}

func (r *InboxRepository) MarkCompleted(ctx context.Context, consumerName, messageID string, at time.Time) error {
	_, err := db(ctx, r.pool).Exec(ctx, `
		UPDATE inbox_records SET completed_at = $1 WHERE consumer_name = $2 AND message_id = $3`,
		at, consumerName, messageID)
	if err != nil {
		return fmt.Errorf("postgres: mark inbox record completed: %w", err)
	}
	return nil
}
