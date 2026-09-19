package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cassianobraz/wager-service/internal/domain/events"
	"github.com/cassianobraz/wager-service/internal/ports"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
	"uuid"
)

type OutboxRepository struct {
	pool *pgxpool.Pool
}

func NewOutboxRepository(pool *pgxpool.Pool) *OutboxRepository {
	return &OutboxRepository{pool: pool}
}

func (r *OutboxRepository) Append(ctx context.Context, rec ports.OutboxRecord) error {
	envelope, err := json.Marshal(rec.Envelope)
	if err != nil {
		return fmt.Errorf("postgres: marshal outbox envelope: %w", err)
	}
	nextAttempt := rec.NextAttempt
	if nextAttempt.IsZero() {
		nextAttempt = rec.OccurredAt
	}
	_, err = db(ctx, r.pool).Exec(ctx, `
		INSERT INTO outbox_records (event_id, aggregate_id, event_type, envelope, occurred_at, attempts, next_attempt, published_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		toPgUUID(rec.EventID), toPgUUID(rec.AggregateID), rec.EventType, envelope, rec.OccurredAt, rec.Attempts, nextAttempt, rec.PublishedAt)
	if err != nil {
		return fmt.Errorf("postgres: append outbox record: %w", err)
	}
	return nil
}

func (r *OutboxRepository) ClaimPending(ctx context.Context, now time.Time, limit int) ([]ports.OutboxRecord, error) {
	rows, err := db(ctx, r.pool).Query(ctx, `
		SELECT event_id, aggregate_id, event_type, envelope, occurred_at, attempts, next_attempt, published_at
		FROM outbox_records
		WHERE published_at IS NULL AND next_attempt <= $1
		ORDER BY next_attempt, event_id
		LIMIT $2
		FOR UPDATE SKIP LOCKED`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: claim pending outbox records: %w", err)
	}
	defer rows.Close()

	var records []ports.OutboxRecord
	for rows.Next() {
		var (
			eventID, aggregateID pgtype.UUID
			eventType            string
			envelopeRaw          []byte
			occurredAt           time.Time
			attempts             int
			nextAttempt          time.Time
			publishedAt          *time.Time
		)
		if err := rows.Scan(&eventID, &aggregateID, &eventType, &envelopeRaw, &occurredAt, &attempts, &nextAttempt, &publishedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan outbox record: %w", err)
		}
		var envelope events.Envelope
		if err := json.Unmarshal(envelopeRaw, &envelope); err != nil {
			return nil, fmt.Errorf("postgres: unmarshal outbox envelope: %w", err)
		}
		records = append(records, ports.OutboxRecord{
			EventID:     fromPgUUID(eventID),
			AggregateID: fromPgUUID(aggregateID),
			EventType:   eventType,
			Envelope:    envelope,
			OccurredAt:  occurredAt,
			Attempts:    attempts,
			NextAttempt: nextAttempt,
			PublishedAt: publishedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: claim pending outbox records: %w", err)
	}
	return records, nil
}

func (r *OutboxRepository) MarkPublished(ctx context.Context, eventID uuid.UUID, at time.Time) error {
	_, err := db(ctx, r.pool).Exec(ctx, `UPDATE outbox_records SET published_at = $1 WHERE event_id = $2`, at, toPgUUID(eventID))
	if err != nil {
		return fmt.Errorf("postgres: mark outbox record published: %w", err)
	}
	return nil
}

func (r *OutboxRepository) ScheduleRetry(ctx context.Context, eventID uuid.UUID, attempts int, nextAttempt time.Time) error {
	_, err := db(ctx, r.pool).Exec(ctx, `
		UPDATE outbox_records SET attempts = $1, next_attempt = $2 WHERE event_id = $3`,
		attempts, nextAttempt, toPgUUID(eventID))
	if err != nil {
		return fmt.Errorf("postgres: schedule outbox retry: %w", err)
	}
	return nil
}
