package fxmodules

import (
	"github.com/cassianobraz/wager-service/internal/infra/config"
	"go.uber.org/fx"
)

func ConfigModule() fx.Option {
	return fx.Provide(config.Load)
}
