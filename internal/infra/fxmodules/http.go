package fxmodules

import (
	"context"
	"errors"
	"fmt"
	"github.com/cassianobraz/wager-service/internal/infra/config"
	infrahttp "github.com/cassianobraz/wager-service/internal/infra/http"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/fx"
	"log/slog"
	nethttp "net/http"
	"time"
)

func HTTPModule() fx.Option {
	return fx.Options(
		fx.Provide(infrahttp.NewHandlers),
		fx.Provide(newReadinessChecker),
		fx.Provide(infrahttp.NewRouter),
		fx.Invoke(runHTTPServer),
	)
}

type poolReadinessChecker struct {
	pool *pgxpool.Pool
	conn *amqp.Connection
}

func (c *poolReadinessChecker) Ready(ctx context.Context) error {
	if err := c.pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres not ready: %w", err)
	}
	if c.conn.IsClosed() {
		return errors.New("rabbitmq connection is closed")
	}
	return nil
}

func newReadinessChecker(pool *pgxpool.Pool, conn *amqp.Connection) infrahttp.ReadinessChecker {
	return &poolReadinessChecker{pool: pool, conn: conn}
}

func runHTTPServer(lc fx.Lifecycle, cfg config.Config, handler nethttp.Handler, logger *slog.Logger) {
	server := &nethttp.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				if err := server.ListenAndServe(); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
					logger.Error("http server stopped unexpectedly", "error", err)
				}
			}()
			logger.Info("http server listening", "addr", cfg.HTTPAddr)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			shutdownCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			return server.Shutdown(shutdownCtx)
		},
	})
}
