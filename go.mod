module github.com/cassianobraz/wager-service

go 1.27.1

require (
	github.com/go-chi/chi/v5 v5.3.2
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/jackc/pgx/v5 v5.7.2
	github.com/rabbitmq/amqp091-go v1.15.0
	go.uber.org/fx v1.23.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	go.uber.org/dig v1.18.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.26.0 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/sync v0.10.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	golang.org/x/text v0.21.0 // indirect
)

replace go.uber.org/fx => github.com/uber-go/fx v1.23.0

replace go.uber.org/dig => github.com/uber-go/dig v1.18.0

replace go.uber.org/multierr => github.com/uber-go/multierr v1.11.0

replace go.uber.org/zap => github.com/uber-go/zap v1.27.0

replace golang.org/x/sys => github.com/golang/sys v0.26.0

replace golang.org/x/crypto => github.com/golang/crypto v0.31.0

replace golang.org/x/text => github.com/golang/text v0.21.0

replace gopkg.in/yaml.v3 => github.com/go-yaml/yaml/v3 v3.0.1

replace gopkg.in/check.v1 => github.com/go-check/check v0.0.0-20201130134442-10cb98267c6c

replace github.com/jackc/puddle/v2 => github.com/jackc/puddle/v2 v2.2.2

replace golang.org/x/sync => github.com/golang/sync v0.10.0
