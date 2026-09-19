package fxmodules

import (
	"github.com/cassianobraz/wager-service/internal/infra/config"
	"github.com/cassianobraz/wager-service/internal/infra/keycloak"
	"go.uber.org/fx"
)

func KeycloakModule() fx.Option {
	return fx.Options(
		fx.Provide(func(cfg config.Config) keycloak.Config { return cfg.Keycloak }),
		fx.Provide(func(cfg keycloak.Config) *keycloak.KeyFetcher { return keycloak.NewKeyFetcher(cfg.JWKSURL, nil) }),
		fx.Provide(keycloak.NewAuthenticator),
	)
}
