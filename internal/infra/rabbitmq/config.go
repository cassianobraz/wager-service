package rabbitmq

type Config struct {
	URL                string
	Exchange           string
	Queue              string
	RoutingKey         string
	RetryExchange      string
	RetryQueue         string
	DeadLetterExchange string
	DeadLetterQueue    string
	ConsumerName       string
	PrefetchCount      int
	MaxRetries         int
}

func DefaultConfig() Config {
	return Config{
		URL:                "amqp://guest:guest@localhost:5672/",
		Exchange:           "wager.events",
		Queue:              "wager.transactions",
		RoutingKey:         "wager.transaction.#",
		RetryExchange:      "wager.transactions.retry",
		RetryQueue:         "wager.transactions.retry",
		DeadLetterExchange: "wager.transactions.dlx",
		DeadLetterQueue:    "wager.transactions.dlq",
		ConsumerName:       "wager-consumer",
		PrefetchCount:      32,
		MaxRetries:         5,
	}
}
