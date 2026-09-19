package fxmodules

import "go.uber.org/fx"

func Module() fx.Option {
	return fx.Options(
		LoggerModule(),
		ConfigModule(),
		PostgresModule(),
		RabbitMQModule(),
		KeycloakModule(),
		ApplicationModule(),
		HTTPModule(),
	)
}
