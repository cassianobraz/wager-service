package fxmodules

import (
	"go.uber.org/fx"
	"log/slog"
	"os"
)

func LoggerModule() fx.Option {
	return fx.Provide(func() *slog.Logger {
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	})
}
