package transport

import (
	"fmt"
	"github.com/cassianobraz/wager-service/internal/application"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"time"
	"uuid"
)

type WagerOperationRequest struct {
	ExternalTransactionID          string `json:"externalTransactionId"`
	IdempotencyKey                 string `json:"idempotencyKey"`
	PlayerID                       string `json:"playerId"`
	WalletID                       string `json:"walletId"`
	RoundID                        string `json:"roundId"`
	GameID                         string `json:"gameId"`
	Kind                           string `json:"kind"`
	Amount                         string `json:"amount"`
	Currency                       string `json:"currency"`
	ReferenceExternalTransactionID string `json:"referenceExternalTransactionId,omitempty"`
}

func (req WagerOperationRequest) ToCommand(providerID string, now time.Time, correlationID uuid.UUID, inbox *application.InboxDedup) (application.ProcessWagerTransactionCommand, error) {
	if providerID == "" {
		return application.ProcessWagerTransactionCommand{}, fmt.Errorf("transport: providerId is required and must come from the authenticated caller")
	}
	if req.ExternalTransactionID == "" || req.IdempotencyKey == "" {
		return application.ProcessWagerTransactionCommand{}, fmt.Errorf("transport: externalTransactionId and idempotencyKey are required")
	}

	playerID, err := uuid.Parse(req.PlayerID)
	if err != nil {
		return application.ProcessWagerTransactionCommand{}, fmt.Errorf("transport: invalid playerId: %w", err)
	}
	walletID, err := uuid.Parse(req.WalletID)
	if err != nil {
		return application.ProcessWagerTransactionCommand{}, fmt.Errorf("transport: invalid walletId: %w", err)
	}

	kind := wager.Kind(req.Kind)
	if !kind.IsExternal() {
		return application.ProcessWagerTransactionCommand{}, fmt.Errorf("transport: unsupported or non-submittable kind %q", req.Kind)
	}

	amount, err := money.ParseExternalAmount(req.Amount, req.Currency)
	if err != nil {
		return application.ProcessWagerTransactionCommand{}, fmt.Errorf("transport: invalid amount/currency: %w", err)
	}

	return application.ProcessWagerTransactionCommand{
		ProviderID:                     providerID,
		ExternalTransactionID:          req.ExternalTransactionID,
		IdempotencyKey:                 req.IdempotencyKey,
		PlayerID:                       playerID,
		WalletID:                       walletID,
		RoundID:                        req.RoundID,
		GameID:                         req.GameID,
		Kind:                           kind,
		Amount:                         amount,
		ReferenceExternalTransactionID: req.ReferenceExternalTransactionID,
		Now:                            now,
		CorrelationID:                  correlationID,
		Inbox:                          inbox,
	}, nil
}
