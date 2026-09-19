package fxmodules

import (
	"context"
	"github.com/cassianobraz/wager-service/internal/infra/config"
	"github.com/cassianobraz/wager-service/internal/infra/postgres"
	"github.com/cassianobraz/wager-service/internal/ports"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"
)

func PostgresModule() fx.Option {
	return fx.Options(
		fx.Provide(func(cfg config.Config) postgres.Config { return cfg.Postgres }),
		fx.Provide(newPool),

		fx.Provide(postgres.NewTxManager),
		fx.Provide(func(tm *postgres.TxManager) ports.TxManager { return tm }),
		fx.Provide(fx.Annotate(postgres.NewWalletRepository, fx.As(new(ports.WalletRepository)))),
		fx.Provide(fx.Annotate(postgres.NewWagerTransactionRepository, fx.As(new(ports.WagerTransactionRepository)))),
		fx.Provide(fx.Annotate(postgres.NewLedgerRepository, fx.As(new(ports.LedgerRepository)))),
		fx.Provide(fx.Annotate(postgres.NewInboxRepository, fx.As(new(ports.InboxRepository)))),
		fx.Provide(fx.Annotate(postgres.NewOutboxRepository, fx.As(new(ports.OutboxRepository)))),
	)
}

func newPool(lc fx.Lifecycle, cfg postgres.Config) (*pgxpool.Pool, error) {
	pool, err := postgres.NewPool(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			pool.Close()
			return nil
		},
	})
	return pool, nil
}
