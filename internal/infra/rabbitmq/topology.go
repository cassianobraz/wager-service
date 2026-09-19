package rabbitmq

import (
	"fmt"
	amqp "github.com/rabbitmq/amqp091-go"
)

func DeclareTopology(ch *amqp.Channel, cfg Config) error {
	if err := ch.ExchangeDeclare(cfg.Exchange, amqp.ExchangeTopic, true, false, false, false, nil); err != nil {
		return fmt.Errorf("rabbitmq: declare exchange %s: %w", cfg.Exchange, err)
	}
	if err := ch.ExchangeDeclare(cfg.RetryExchange, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
		return fmt.Errorf("rabbitmq: declare exchange %s: %w", cfg.RetryExchange, err)
	}
	if err := ch.ExchangeDeclare(cfg.DeadLetterExchange, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
		return fmt.Errorf("rabbitmq: declare exchange %s: %w", cfg.DeadLetterExchange, err)
	}

	if _, err := ch.QueueDeclare(cfg.Queue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("rabbitmq: declare queue %s: %w", cfg.Queue, err)
	}
	if err := ch.QueueBind(cfg.Queue, cfg.RoutingKey, cfg.Exchange, false, nil); err != nil {
		return fmt.Errorf("rabbitmq: bind queue %s: %w", cfg.Queue, err)
	}

	if err := ch.QueueBind(cfg.Queue, retryRoutingKey, cfg.Exchange, false, nil); err != nil {
		return fmt.Errorf("rabbitmq: bind queue %s for retry routing key: %w", cfg.Queue, err)
	}

	if _, err := ch.QueueDeclare(cfg.RetryQueue, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    cfg.Exchange,
		"x-dead-letter-routing-key": retryRoutingKey,
	}); err != nil {
		return fmt.Errorf("rabbitmq: declare queue %s: %w", cfg.RetryQueue, err)
	}
	if err := ch.QueueBind(cfg.RetryQueue, retryRoutingKey, cfg.RetryExchange, false, nil); err != nil {
		return fmt.Errorf("rabbitmq: bind queue %s: %w", cfg.RetryQueue, err)
	}

	if _, err := ch.QueueDeclare(cfg.DeadLetterQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("rabbitmq: declare queue %s: %w", cfg.DeadLetterQueue, err)
	}
	if err := ch.QueueBind(cfg.DeadLetterQueue, deadLetterRoutingKey, cfg.DeadLetterExchange, false, nil); err != nil {
		return fmt.Errorf("rabbitmq: bind queue %s: %w", cfg.DeadLetterQueue, err)
	}

	return nil
}

const (
	retryRoutingKey       = "wager.transaction.retry"
	deadLetterRoutingKey  = "wager.transaction.dead-letter"
	retryCountHeader      = "x-retry-count"
	originalRoutingHeader = "x-original-routing-key"
)
