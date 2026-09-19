package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cassianobraz/wager-service/internal/application"
	"github.com/cassianobraz/wager-service/internal/infra/transport"
	amqp "github.com/rabbitmq/amqp091-go"
	"log/slog"
	"strconv"
	"time"
	"uuid"
)

type ProviderResolver func(delivery amqp.Delivery) (string, error)

type Consumer struct {
	channel         *amqp.Channel
	cfg             Config
	useCase         *application.ProcessWagerTransactionUseCase
	resolveProvider ProviderResolver
	logger          *slog.Logger
	now             func() time.Time
}

func NewConsumer(ch *amqp.Channel, cfg Config, useCase *application.ProcessWagerTransactionUseCase, resolveProvider ProviderResolver, logger *slog.Logger) *Consumer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Consumer{
		channel:         ch,
		cfg:             cfg,
		useCase:         useCase,
		resolveProvider: resolveProvider,
		logger:          logger,
		now:             time.Now,
	}
}

func (c *Consumer) Run(ctx context.Context) error {
	if err := c.channel.Qos(c.cfg.PrefetchCount, 0, false); err != nil {
		return fmt.Errorf("rabbitmq: set qos: %w", err)
	}

	deliveries, err := c.channel.ConsumeWithContext(ctx, c.cfg.Queue, c.cfg.ConsumerName, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("rabbitmq: consume %s: %w", c.cfg.Queue, err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("rabbitmq: delivery channel closed")
			}
			c.handle(ctx, d)
		}
	}
}

func (c *Consumer) handle(ctx context.Context, d amqp.Delivery) {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("rabbitmq: panic handling delivery, routing to dead-letter", "panic", r, "messageId", d.MessageId)
			c.deadLetter(ctx, d, fmt.Sprintf("panic: %v", r))
			_ = d.Ack(false)
		}
	}()

	if d.MessageId == "" {

		c.logger.Error("rabbitmq: message missing MessageId, routing to dead-letter")
		c.deadLetter(ctx, d, "missing MessageId, cannot deduplicate")
		_ = d.Ack(false)
		return
	}

	providerID, err := c.resolveProvider(d)
	if err != nil {
		c.logger.Error("rabbitmq: could not resolve provider identity, routing to dead-letter", "error", err, "messageId", d.MessageId)
		c.deadLetter(ctx, d, "provider resolution failed: "+err.Error())
		_ = d.Ack(false)
		return
	}

	var req transport.WagerOperationRequest
	if err := json.Unmarshal(d.Body, &req); err != nil {
		c.logger.Error("rabbitmq: malformed message body, routing to dead-letter", "error", err, "messageId", d.MessageId)
		c.deadLetter(ctx, d, "malformed JSON body: "+err.Error())
		_ = d.Ack(false)
		return
	}

	cmd, err := req.ToCommand(providerID, c.now(), uuid.NewV7(), &application.InboxDedup{
		ConsumerName: c.cfg.ConsumerName,
		MessageID:    d.MessageId,
	})
	if err != nil {
		c.logger.Error("rabbitmq: invalid operation payload, routing to dead-letter", "error", err, "messageId", d.MessageId)
		c.deadLetter(ctx, d, "invalid payload: "+err.Error())
		_ = d.Ack(false)
		return
	}

	result, err := c.useCase.Execute(ctx, cmd)
	if err != nil {
		if errors.Is(err, application.ErrIdempotencyKeyReused) || errors.Is(err, application.ErrExternalTransactionKeyMismatch) {

			c.logger.Warn("rabbitmq: idempotency conflict, routing to dead-letter", "error", err, "messageId", d.MessageId)
			c.deadLetter(ctx, d, "idempotency conflict: "+err.Error())
			_ = d.Ack(false)
			return
		}

		c.retry(ctx, d, err)
		return
	}

	c.logger.Info("rabbitmq: processed wager operation", "messageId", d.MessageId, "transactionId", result.TransactionID, "status", result.Status)
	_ = d.Ack(false)
}

func (c *Consumer) retry(ctx context.Context, d amqp.Delivery, cause error) {
	attempt := retryCount(d) + 1
	if attempt > c.cfg.MaxRetries {
		c.logger.Error("rabbitmq: retry budget exhausted, routing to dead-letter", "error", cause, "messageId", d.MessageId, "attempts", attempt-1)
		c.deadLetter(ctx, d, "retry budget exhausted: "+cause.Error())
		_ = d.Ack(false)
		return
	}

	headers := amqp.Table{}
	for k, v := range d.Headers {
		headers[k] = v
	}
	headers[retryCountHeader] = int32(attempt)

	backoff := backoffFor(attempt)
	c.logger.Warn("rabbitmq: transient failure, scheduling retry", "error", cause, "messageId", d.MessageId, "attempt", attempt, "backoff", backoff)

	err := c.channel.PublishWithContext(ctx, c.cfg.RetryExchange, retryRoutingKey, false, false, amqp.Publishing{
		ContentType:  d.ContentType,
		DeliveryMode: amqp.Persistent,
		MessageId:    d.MessageId,
		Timestamp:    c.now(),
		Headers:      headers,
		Expiration:   strconv.FormatInt(backoff.Milliseconds(), 10),
		Body:         d.Body,
	})
	if err != nil {

		c.logger.Error("rabbitmq: failed to publish retry, requeueing on channel", "error", err, "messageId", d.MessageId)
		_ = d.Nack(false, true)
		return
	}
	_ = d.Ack(false)
}

func (c *Consumer) deadLetter(ctx context.Context, d amqp.Delivery, reason string) {
	headers := amqp.Table{}
	for k, v := range d.Headers {
		headers[k] = v
	}
	headers["x-dead-letter-reason"] = reason
	headers[originalRoutingHeader] = d.RoutingKey

	if err := c.channel.PublishWithContext(ctx, c.cfg.DeadLetterExchange, deadLetterRoutingKey, false, false, amqp.Publishing{
		ContentType:  d.ContentType,
		DeliveryMode: amqp.Persistent,
		MessageId:    d.MessageId,
		Timestamp:    c.now(),
		Headers:      headers,
		Body:         d.Body,
	}); err != nil {
		c.logger.Error("rabbitmq: failed to publish to dead-letter exchange", "error", err, "messageId", d.MessageId)
	}
}

func retryCount(d amqp.Delivery) int {
	if d.Headers == nil {
		return 0
	}
	switch v := d.Headers[retryCountHeader].(type) {
	case int32:
		return int(v)
	case int64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

func backoffFor(attempt int) time.Duration {
	const base = 2 * time.Second
	const capDuration = time.Minute
	d := base
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= capDuration {
			return capDuration
		}
	}
	return d
}
