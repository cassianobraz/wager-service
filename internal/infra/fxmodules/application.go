package fxmodules

import (
	"context"
	"github.com/cassianobraz/wager-service/internal/application"
	"github.com/cassianobraz/wager-service/internal/infra/config"
	"go.uber.org/fx"
	"log/slog"
	"time"
)

func ApplicationModule() fx.Option {
	return fx.Options(
		fx.Provide(application.NewProcessWagerTransactionUseCase),
		fx.Provide(application.NewOpenWalletUseCase),
		fx.Provide(application.NewReconciliationUseCase),
		fx.Provide(application.NewQueryService),
		fx.Provide(application.DefaultPendingReferenceRetryPolicy),
		fx.Provide(application.NewResolvePendingReferencesUseCase),
		fx.Invoke(runPendingReferenceResolver),
	)
}

func runPendingReferenceResolver(lc fx.Lifecycle, uc *application.ResolvePendingReferencesUseCase, cfg config.Config, logger *slog.Logger) {
	ctx, cancel := context.WithCancel(context.Background())

	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go pollPendingReferences(ctx, uc, cfg, logger)
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}

func pollPendingReferences(ctx context.Context, uc *application.ResolvePendingReferencesUseCase, cfg config.Config, logger *slog.Logger) {
	ticker := time.NewTicker(cfg.PendingReferencePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			resolved, rescheduled, err := uc.RunOnce(ctx, time.Now(), cfg.PendingReferenceBatchSize)
			if err != nil {
				logger.Error("pending-reference resolver poll failed", "error", err)
				continue
			}
			if resolved > 0 || rescheduled > 0 {
				logger.Info("pending-reference resolver poll completed", "resolved", resolved, "rescheduled", rescheduled)
			}
		}
	}
}
