package rabbitmq

import (
	"context"
	"github.com/cassianobraz/wager-service/internal/ports"
	"log/slog"
	"time"
)

type OutboxWorker struct {
	outbox       ports.OutboxRepository
	publisher    ports.EventPublisher
	batchSize    int
	pollInterval time.Duration
	backoff      BackoffPolicy
	logger       *slog.Logger
	now          func() time.Time
}

type BackoffPolicy struct {
	Base time.Duration
	Max  time.Duration
}

func DefaultBackoffPolicy() BackoffPolicy {
	return BackoffPolicy{Base: 2 * time.Second, Max: 5 * time.Minute}
}

func (p BackoffPolicy) NextAttempt(now time.Time, attempts int) time.Time {
	d := p.Base
	for i := 1; i < attempts; i++ {
		d *= 2
		if d >= p.Max {
			d = p.Max
			break
		}
	}
	return now.Add(d)
}

func NewOutboxWorker(outbox ports.OutboxRepository, publisher ports.EventPublisher, batchSize int, pollInterval time.Duration, logger *slog.Logger) *OutboxWorker {
	if logger == nil {
		logger = slog.Default()
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	return &OutboxWorker{
		outbox:       outbox,
		publisher:    publisher,
		batchSize:    batchSize,
		pollInterval: pollInterval,
		backoff:      DefaultBackoffPolicy(),
		logger:       logger,
		now:          time.Now,
	}
}

func (w *OutboxWorker) Run(ctx context.Context) error {
	for {
		n, err := w.RunOnce(ctx)
		if err != nil {
			w.logger.Error("rabbitmq: outbox worker poll failed", "error", err)
		}

		wait := w.pollInterval
		if err == nil && n == w.batchSize {
			wait = 0
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (w *OutboxWorker) RunOnce(ctx context.Context) (int, error) {
	now := w.now()
	records, err := w.outbox.ClaimPending(ctx, now, w.batchSize)
	if err != nil {
		return 0, err
	}

	for _, rec := range records {
		if err := w.publisher.Publish(ctx, rec); err != nil {
			attempts := rec.Attempts + 1
			next := w.backoff.NextAttempt(now, attempts)
			w.logger.Warn("rabbitmq: outbox publish failed, scheduling retry", "eventId", rec.EventID, "eventType", rec.EventType, "attempts", attempts, "error", err)
			if scheduleErr := w.outbox.ScheduleRetry(ctx, rec.EventID, attempts, next); scheduleErr != nil {
				return len(records), scheduleErr
			}
			continue
		}
		if err := w.outbox.MarkPublished(ctx, rec.EventID, now); err != nil {
			return len(records), err
		}
	}
	return len(records), nil
}
