package config

import (
	"fmt"
	"github.com/cassianobraz/wager-service/internal/infra/keycloak"
	"github.com/cassianobraz/wager-service/internal/infra/postgres"
	"github.com/cassianobraz/wager-service/internal/infra/rabbitmq"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr                     string
	Postgres                     postgres.Config
	RabbitMQ                     rabbitmq.Config
	Keycloak                     keycloak.Config
	OutboxBatchSize              int
	OutboxPollInterval           time.Duration
	PendingReferencePollInterval time.Duration
	PendingReferenceBatchSize    int
}

func Load() (Config, error) {
	pgPort, err := envInt("POSTGRES_PORT", 5432)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		HTTPAddr: envString("HTTP_ADDR", ":8080"),

		Postgres: postgres.Config{
			Host:     envString("POSTGRES_HOST", "localhost"),
			Port:     pgPort,
			User:     envString("POSTGRES_USER", "wager"),
			Password: envString("POSTGRES_PASSWORD", "wager"),
			Database: envString("POSTGRES_DB", "wager"),
			SSLMode:  envString("POSTGRES_SSLMODE", "disable"),
		},

		RabbitMQ: rabbitmq.Config{
			URL:                envString("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
			Exchange:           envString("RABBITMQ_EXCHANGE", "wager.events"),
			Queue:              envString("RABBITMQ_QUEUE", "wager.transactions"),
			RoutingKey:         envString("RABBITMQ_ROUTING_KEY", "wager.transaction.#"),
			RetryExchange:      envString("RABBITMQ_RETRY_EXCHANGE", "wager.transactions.retry"),
			RetryQueue:         envString("RABBITMQ_RETRY_QUEUE", "wager.transactions.retry"),
			DeadLetterExchange: envString("RABBITMQ_DLX", "wager.transactions.dlx"),
			DeadLetterQueue:    envString("RABBITMQ_DLQ", "wager.transactions.dlq"),
			ConsumerName:       envString("RABBITMQ_CONSUMER_NAME", "wager-consumer"),
		},

		Keycloak: keycloak.Config{
			Issuer:        envString("KEYCLOAK_ISSUER", "http://localhost:8080/realms/wager"),
			JWKSURL:       envString("KEYCLOAK_JWKS_URL", "http://localhost:8080/realms/wager/protocol/openid-connect/certs"),
			Audience:      envString("KEYCLOAK_AUDIENCE", ""),
			ProviderClaim: envString("KEYCLOAK_PROVIDER_CLAIM", "azp"),
		},

		OutboxBatchSize:    100,
		OutboxPollInterval: time.Second,

		PendingReferencePollInterval: 5 * time.Second,
		PendingReferenceBatchSize:    50,
	}

	prefetch, err := envInt("RABBITMQ_PREFETCH_COUNT", 32)
	if err != nil {
		return Config{}, err
	}
	cfg.RabbitMQ.PrefetchCount = prefetch

	maxRetries, err := envInt("RABBITMQ_MAX_RETRIES", 5)
	if err != nil {
		return Config{}, err
	}
	cfg.RabbitMQ.MaxRetries = maxRetries

	return cfg, nil
}

func envString(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config: invalid integer for %s: %w", key, err)
	}
	return n, nil
}
