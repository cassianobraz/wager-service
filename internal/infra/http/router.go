package http

import (
	"context"
	"github.com/cassianobraz/wager-service/internal/infra/keycloak"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"net/http"
	"time"
)

func NewRouter(h *Handlers, auth *keycloak.Authenticator, ready ReadinessChecker) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/health/live", h.Liveness)
	r.Get("/health/ready", readinessHandler(ready))

	r.Group(func(r chi.Router) {
		if auth != nil {
			r.Use(auth.Middleware)
		}

		r.Post("/wallets", h.OpenWallet)
		r.Get("/wallets/{walletId}", h.GetWallet)
		r.Get("/wallets/{walletId}/ledger", h.GetWalletLedger)
		r.Post("/wallets/{walletId}/reconciliation", h.Reconcile)

		r.Post("/wagering/transactions", h.SubmitWagerTransaction)
		r.Get("/wagering/transactions/{transactionId}", h.GetWagerTransaction)
		r.Get("/providers/{providerId}/wagering/transactions/{externalId}", h.GetWagerTransactionByExternalID)
	})

	return r
}

func readinessHandler(ready ReadinessChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ready == nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := ready.Ready(ctx); err != nil {
			writeError(w, http.StatusServiceUnavailable, "not_ready", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}
