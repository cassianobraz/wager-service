package http

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/cassianobraz/wager-service/internal/application"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"github.com/cassianobraz/wager-service/internal/infra/keycloak"
	"github.com/cassianobraz/wager-service/internal/infra/transport"
	"github.com/go-chi/chi/v5"
	"net/http"
	"time"
	"uuid"
)

type Handlers struct {
	openWallet   *application.OpenWalletUseCase
	processWager *application.ProcessWagerTransactionUseCase
	reconcile    *application.ReconciliationUseCase
	queries      *application.QueryService
	now          func() time.Time
}

func NewHandlers(
	openWallet *application.OpenWalletUseCase,
	processWager *application.ProcessWagerTransactionUseCase,
	reconcile *application.ReconciliationUseCase,
	queries *application.QueryService,
) *Handlers {
	return &Handlers{
		openWallet:   openWallet,
		processWager: processWager,
		reconcile:    reconcile,
		queries:      queries,
		now:          time.Now,
	}
}

type openWalletRequest struct {
	PlayerID       string `json:"playerId"`
	InitialBalance string `json:"initialBalance"`
	Currency       string `json:"currency"`
}

func (h *Handlers) OpenWallet(w http.ResponseWriter, r *http.Request) {
	var req openWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
		return
	}

	playerID, err := uuid.Parse(req.PlayerID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid playerId")
		return
	}

	initialBalance := money.Money{}
	if req.InitialBalance == "" {
		initialBalance, err = money.Zero(req.Currency)
	} else {
		initialBalance, err = money.Parse(req.InitialBalance, req.Currency)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid initialBalance/currency: "+err.Error())
		return
	}
	if initialBalance.IsNegative() {
		writeError(w, http.StatusBadRequest, "invalid_request", "initialBalance must not be negative")
		return
	}

	result, err := h.openWallet.Execute(r.Context(), application.OpenWalletCommand{
		PlayerID:       playerID,
		InitialBalance: initialBalance,
		CorrelationID:  uuid.NewV7(),
		Now:            h.now(),
	})
	if err != nil {
		writeError(w, statusForError(err), "open_wallet_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, walletResponse{
		ID:        result.WalletID.String(),
		PlayerID:  req.PlayerID,
		Currency:  result.Balance.Currency(),
		Balance:   result.Balance.String(),
		Version:   result.Version,
		CreatedAt: h.now().UTC().Format(time.RFC3339Nano),
		UpdatedAt: h.now().UTC().Format(time.RFC3339Nano),
	})
}

func (h *Handlers) GetWallet(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(chi.URLParam(r, "walletId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid walletId")
		return
	}
	wal, err := h.queries.GetWallet(r.Context(), id)
	if err != nil {
		writeError(w, statusForError(err), "wallet_lookup_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, walletToResponse(wal))
}

func (h *Handlers) GetWalletLedger(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(chi.URLParam(r, "walletId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid walletId")
		return
	}
	cursor := r.URL.Query().Get("cursor")
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, convErr := parsePositiveInt(v); convErr == nil {
			limit = n
		}
	}

	entries, nextCursor, err := h.queries.GetLedgerPage(r.Context(), id, cursor, limit)
	if err != nil {
		writeError(w, statusForError(err), "ledger_lookup_failed", err.Error())
		return
	}

	resp := ledgerPageResponse{Entries: make([]ledgerEntryResponse, 0, len(entries)), NextCursor: nextCursor}
	for _, e := range entries {
		resp.Entries = append(resp.Entries, ledgerEntryToResponse(e))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) Reconcile(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(chi.URLParam(r, "walletId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid walletId")
		return
	}
	result, err := h.reconcile.Execute(r.Context(), id)
	if err != nil {
		writeError(w, statusForError(err), "reconciliation_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, reconciliationResponse{
		WalletID:          result.WalletID.String(),
		StoredBalance:     result.StoredBalance.String(),
		CalculatedBalance: result.CalculatedBalance.String(),
		Difference:        result.Difference.String(),
		Consistent:        result.Consistent,
		CheckedEntries:    result.CheckedEntries,
	})
}

func (h *Handlers) SubmitWagerTransaction(w http.ResponseWriter, r *http.Request) {
	providerID, ok := keycloak.ProviderIDFromContext(r.Context())
	if !ok || providerID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing authenticated provider identity")
		return
	}

	var req transport.WagerOperationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
		return
	}

	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey != "" {
		req.IdempotencyKey = idempotencyKey
	}

	cmd, err := req.ToCommand(providerID, h.now(), uuid.NewV7(), nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	result, err := h.processWager.Execute(r.Context(), cmd)
	if err != nil {
		if errors.Is(err, application.ErrIdempotencyKeyReused) || errors.Is(err, application.ErrExternalTransactionKeyMismatch) {
			writeError(w, http.StatusConflict, "idempotency_conflict", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "processing_failed", err.Error())
		return
	}

	status := http.StatusOK
	if !result.IdempotentReplay {
		status = http.StatusCreated
	}
	writeJSON(w, status, processResultResponse{
		TransactionID:    result.TransactionID.String(),
		Status:           string(result.Status),
		Balance:          moneyToPtr(result.Balance),
		FailureCode:      string(result.FailureCode),
		IdempotentReplay: result.IdempotentReplay,
	})
}

func (h *Handlers) GetWagerTransaction(w http.ResponseWriter, r *http.Request) {
	providerID, _ := keycloak.ProviderIDFromContext(r.Context())

	id, err := parseUUIDParam(chi.URLParam(r, "transactionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid transactionId")
		return
	}
	tx, err := h.queries.GetTransaction(r.Context(), id)
	if err != nil {
		writeError(w, statusForError(err), "transaction_lookup_failed", err.Error())
		return
	}

	if tx.ProviderID() != providerID {
		writeError(w, http.StatusNotFound, "not_found", "transaction not found")
		return
	}
	writeJSON(w, http.StatusOK, transactionToResponse(tx))
}

func (h *Handlers) GetWagerTransactionByExternalID(w http.ResponseWriter, r *http.Request) {
	authenticatedProviderID, _ := keycloak.ProviderIDFromContext(r.Context())
	pathProviderID := chi.URLParam(r, "providerId")
	externalID := chi.URLParam(r, "externalId")

	if pathProviderID != authenticatedProviderID {
		writeError(w, http.StatusForbidden, "forbidden", "cannot access another provider's transactions")
		return
	}

	tx, err := h.queries.GetTransactionByExternalID(r.Context(), pathProviderID, externalID)
	if err != nil {
		writeError(w, statusForError(err), "transaction_lookup_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, transactionToResponse(tx))
}

func (h *Handlers) Liveness(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "live"})
}

type ReadinessChecker interface {
	Ready(ctx context.Context) error
}

func parsePositiveInt(s string) (int, error) {
	n := 0
	if s == "" {
		return 0, errInvalidInt
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errInvalidInt
		}
		n = n*10 + int(r-'0')
	}
	if n <= 0 {
		return 0, errInvalidInt
	}
	return n, nil
}

var errInvalidInt = errors.New("http: invalid positive integer")
