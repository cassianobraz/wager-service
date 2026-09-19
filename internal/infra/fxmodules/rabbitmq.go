package fxmodules

import (
	"context"
	"fmt"
	"github.com/cassianobraz/wager-service/internal/application"
	"github.com/cassianobraz/wager-service/internal/infra/config"
	"github.com/cassianobraz/wager-service/internal/infra/rabbitmq"
	"github.com/cassianobraz/wager-service/internal/ports"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/fx"
	"log/slog"
)

func RabbitMQModule() fx.Option {
	return fx.Options(
		fx.Provide(func(cfg config.Config) rabbitmq.Config { return cfg.RabbitMQ }),
		fx.Provide(newConnection),
		fx.Provide(newChannel),
		fx.Provide(fx.Annotate(newEventPublisher, fx.As(new(ports.EventPublisher)))),
		fx.Invoke(runOutboxWorker),
		fx.Invoke(runConsumer),
	)
}

func newConnection(lc fx.Lifecycle, cfg rabbitmq.Config) (*amqp.Connection, error) {
	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: dial: %w", err)
	}
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return conn.Close()
		},
	})
	return conn, nil
}

func newChannel(lc fx.Lifecycle, conn *amqp.Connection, cfg rabbitmq.Config) (*amqp.Channel, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: open channel: %w", err)
	}
	if err := rabbitmq.DeclareTopology(ch, cfg); err != nil {
		_ = ch.Close()
		return nil, err
	}
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return ch.Close()
		},
	})
	return ch, nil
}

func newEventPublisher(lc fx.Lifecycle, conn *amqp.Connection, cfg rabbitmq.Config) (*rabbitmq.EventPublisher, error) {
	pub, err := rabbitmq.NewEventPublisher(conn, cfg.Exchange)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			return pub.Close()
		},
	})
	return pub, nil
}

func runConsumer(lc fx.Lifecycle, ch *amqp.Channel, cfg rabbitmq.Config, useCase *application.ProcessWagerTransactionUseCase, logger *slog.Logger) {
	ctx, cancel := context.WithCancel(context.Background())
	consumer := rabbitmq.NewConsumer(ch, cfg, useCase, singleProviderResolver(cfg), logger)

	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				if err := consumer.Run(ctx); err != nil && ctx.Err() == nil {
					logger.Error("rabbitmq: consumer stopped unexpectedly", "error", err)
				}
			}()
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}

func singleProviderResolver(cfg rabbitmq.Config) rabbitmq.ProviderResolver {
	return func(d amqp.Delivery) (string, error) {
		if v, ok := d.Headers["x-provider-id"].(string); ok && v != "" {
			return v, nil
		}
		return "", fmt.Errorf("message is missing the x-provider-id header")
	}
}

func runOutboxWorker(lc fx.Lifecycle, outbox ports.OutboxRepository, publisher ports.EventPublisher, cfg config.Config, logger *slog.Logger) {
	ctx, cancel := context.WithCancel(context.Background())
	worker := rabbitmq.NewOutboxWorker(outbox, publisher, cfg.OutboxBatchSize, cfg.OutboxPollInterval, logger)

	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				if err := worker.Run(ctx); err != nil && ctx.Err() == nil {
					logger.Error("rabbitmq: outbox worker stopped unexpectedly", "error", err)
				}
			}()
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}
