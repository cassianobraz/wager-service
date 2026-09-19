package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cassianobraz/wager-service/internal/ports"
	amqp "github.com/rabbitmq/amqp091-go"
)

type EventPublisher struct {
	channel  *amqp.Channel
	exchange string
}

func NewEventPublisher(conn *amqp.Connection, exchange string) (*EventPublisher, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: open publisher channel: %w", err)
	}

	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("rabbitmq: enable publisher confirms: %w", err)
	}
	return &EventPublisher{channel: ch, exchange: exchange}, nil
}

func (p *EventPublisher) Close() error {
	return p.channel.Close()
}

func routingKeyFor(eventType string) string {
	var b []byte
	for i, r := range eventType {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b = append(b, '.')
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b = append(b, byte(r))
	}
	return "wager.event." + string(b)
}

func (p *EventPublisher) Publish(ctx context.Context, rec ports.OutboxRecord) error {
	body, err := json.Marshal(rec.Envelope)
	if err != nil {
		return fmt.Errorf("rabbitmq: marshal envelope for event %s: %w", rec.EventID, err)
	}

	confirmation, err := p.channel.PublishWithDeferredConfirmWithContext(ctx, p.exchange, routingKeyFor(rec.EventType), false, false, amqp.Publishing{
		ContentType:   "application/json",
		DeliveryMode:  amqp.Persistent,
		MessageId:     rec.EventID.String(),
		CorrelationId: rec.Envelope.CorrelationID.String(),
		Timestamp:     rec.OccurredAt,
		Type:          rec.EventType,
		Body:          body,
	})
	if err != nil {
		return fmt.Errorf("rabbitmq: publish event %s: %w", rec.EventID, err)
	}
	ok, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("rabbitmq: wait for publish confirm of event %s: %w", rec.EventID, err)
	}
	if !ok {
		return fmt.Errorf("rabbitmq: broker nacked publish of event %s", rec.EventID)
	}
	return nil
}
