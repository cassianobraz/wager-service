package http

import (
	"encoding/json"
	"errors"
	"github.com/cassianobraz/wager-service/internal/application"
	"github.com/cassianobraz/wager-service/internal/domain/domainerr"
	"github.com/cassianobraz/wager-service/internal/domain/ledger"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"github.com/cassianobraz/wager-service/internal/domain/wallet"
	"github.com/cassianobraz/wager-service/internal/ports"
	"net/http"
	"time"
	"uuid"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(body)
}

type errorResponse struct {
	Error       string `json:"error"`
	Message     string `json:"message"`
	FailureCode string `json:"failureCode,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: code, Message: message})
}

func statusForError(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case isNotFound(err):
		return http.StatusNotFound
	case isConflict(err):
		return http.StatusConflict
	case isInvalidArgument(err):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func isNotFound(err error) bool { return errors.Is(err, domainerr.ErrNotFound) }

func isInvalidArgument(err error) bool { return errors.Is(err, domainerr.ErrInvalidArgument) }

func isConflict(err error) bool {
	return errors.Is(err, domainerr.ErrConflict) ||
		errors.Is(err, ports.ErrOptimisticLock) ||
		errors.Is(err, application.ErrIdempotencyKeyReused) ||
		errors.Is(err, application.ErrExternalTransactionKeyMismatch)
}

type walletResponse struct {
	ID        string `json:"id"`
	PlayerID  string `json:"playerId"`
	Currency  string `json:"currency"`
	Balance   string `json:"balance"`
	Version   int64  `json:"version"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func walletToResponse(w *wallet.Wallet) walletResponse {
	return walletResponse{
		ID:        w.ID().String(),
		PlayerID:  w.PlayerID().String(),
		Currency:  w.Currency(),
		Balance:   w.Balance().String(),
		Version:   w.Version(),
		CreatedAt: w.CreatedAt().UTC().Format(time.RFC3339Nano),
		UpdatedAt: w.UpdatedAt().UTC().Format(time.RFC3339Nano),
	}
}

type transactionResponse struct {
	ID                             string  `json:"id"`
	ProviderID                     string  `json:"providerId,omitempty"`
	ExternalTransactionID          string  `json:"externalTransactionId,omitempty"`
	WalletID                       string  `json:"walletId"`
	PlayerID                       string  `json:"playerId"`
	RoundID                        string  `json:"roundId,omitempty"`
	GameID                         string  `json:"gameId,omitempty"`
	Kind                           string  `json:"kind"`
	Amount                         string  `json:"amount"`
	Currency                       string  `json:"currency"`
	ReferenceExternalTransactionID string  `json:"referenceExternalTransactionId,omitempty"`
	Status                         string  `json:"status"`
	FailureCode                    string  `json:"failureCode,omitempty"`
	ResultingBalance               *string `json:"resultingBalance,omitempty"`
	CreatedAt                      string  `json:"createdAt"`
	UpdatedAt                      string  `json:"updatedAt"`
}

func transactionToResponse(tx *wager.Transaction) transactionResponse {
	resp := transactionResponse{
		ID:                             tx.ID().String(),
		ProviderID:                     tx.ProviderID(),
		ExternalTransactionID:          tx.ExternalTransactionID(),
		WalletID:                       tx.WalletID().String(),
		PlayerID:                       tx.PlayerID().String(),
		RoundID:                        tx.RoundID(),
		GameID:                         tx.GameID(),
		Kind:                           string(tx.Kind()),
		Amount:                         tx.Money().String(),
		Currency:                       tx.Money().Currency(),
		ReferenceExternalTransactionID: tx.ReferenceExternalTransactionID(),
		Status:                         string(tx.Status()),
		FailureCode:                    string(tx.FailureCode()),
		CreatedAt:                      tx.CreatedAt().UTC().Format(time.RFC3339Nano),
		UpdatedAt:                      tx.UpdatedAt().UTC().Format(time.RFC3339Nano),
	}
	if tx.ResultingBalance() != nil {
		s := tx.ResultingBalance().String()
		resp.ResultingBalance = &s
	}
	return resp
}

type ledgerEntryResponse struct {
	ID            string `json:"id"`
	WalletID      string `json:"walletId"`
	TransactionID string `json:"transactionId"`
	Direction     string `json:"direction"`
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
	BalanceBefore string `json:"balanceBefore"`
	BalanceAfter  string `json:"balanceAfter"`
	CreatedAt     string `json:"createdAt"`
}

func ledgerEntryToResponse(e *ledger.Entry) ledgerEntryResponse {
	return ledgerEntryResponse{
		ID:            e.ID().String(),
		WalletID:      e.WalletID().String(),
		TransactionID: e.TransactionID().String(),
		Direction:     string(e.Direction()),
		Amount:        e.Money().String(),
		Currency:      e.Money().Currency(),
		BalanceBefore: e.BalanceBefore().String(),
		BalanceAfter:  e.BalanceAfter().String(),
		CreatedAt:     e.CreatedAt().UTC().Format(time.RFC3339Nano),
	}
}

type ledgerPageResponse struct {
	Entries    []ledgerEntryResponse `json:"entries"`
	NextCursor string                `json:"nextCursor,omitempty"`
}

type reconciliationResponse struct {
	WalletID          string `json:"walletId"`
	StoredBalance     string `json:"storedBalance"`
	CalculatedBalance string `json:"calculatedBalance"`
	Difference        string `json:"difference"`
	Consistent        bool   `json:"consistent"`
	CheckedEntries    int    `json:"checkedEntries"`
}

type processResultResponse struct {
	TransactionID    string  `json:"transactionId"`
	Status           string  `json:"status"`
	Balance          *string `json:"balance,omitempty"`
	FailureCode      string  `json:"failureCode,omitempty"`
	IdempotentReplay bool    `json:"idempotentReplay"`
}

func parseUUIDParam(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

func moneyToPtr(m *money.Money) *string {
	if m == nil {
		return nil
	}
	s := m.String()
	return &s
}
