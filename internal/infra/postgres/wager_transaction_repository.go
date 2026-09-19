package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/cassianobraz/wager-service/internal/domain/domainerr"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"github.com/cassianobraz/wager-service/internal/domain/wager"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
	"uuid"
)

type WagerTransactionRepository struct {
	pool *pgxpool.Pool
}

func NewWagerTransactionRepository(pool *pgxpool.Pool) *WagerTransactionRepository {
	return &WagerTransactionRepository{pool: pool}
}

const wagerTxColumns = `
	id, provider_id, external_transaction_id, idempotency_key, payload_hash,
	wallet_id, player_id, round_id, game_id, kind, amount_minor_units, currency,
	reference_external_transaction_id, resolved_reference_transaction_id,
	status, failure_code, resulting_balance_minor_units, created_at, updated_at`

type wagerTxRow struct {
	id                             pgtype.UUID
	providerID                     *string
	externalTransactionID          *string
	idempotencyKey                 *string
	payloadHash                    *string
	walletID                       pgtype.UUID
	playerID                       pgtype.UUID
	roundID                        *string
	gameID                         *string
	kind                           string
	amountMinorUnits               int64
	currency                       string
	referenceExternalTransactionID *string
	resolvedReferenceTransactionID pgtype.UUID
	status                         string
	failureCode                    *string
	resultingBalanceMinorUnits     *int64
	createdAt                      time.Time
	updatedAt                      time.Time
}

func scanWagerTx(row pgx.Row) (*wager.Transaction, error) {
	var r wagerTxRow
	err := row.Scan(
		&r.id, &r.providerID, &r.externalTransactionID, &r.idempotencyKey, &r.payloadHash,
		&r.walletID, &r.playerID, &r.roundID, &r.gameID, &r.kind, &r.amountMinorUnits, &r.currency,
		&r.referenceExternalTransactionID, &r.resolvedReferenceTransactionID,
		&r.status, &r.failureCode, &r.resultingBalanceMinorUnits, &r.createdAt, &r.updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domainerr.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: scan wager transaction: %w", err)
	}
	return rehydrateWagerTxRow(r)
}

func rehydrateWagerTxRow(r wagerTxRow) (*wager.Transaction, error) {
	amount, err := money.FromMinorUnits(r.amountMinorUnits, r.currency)
	if err != nil {
		return nil, fmt.Errorf("postgres: rehydrate transaction amount: %w", err)
	}
	var resultingBalance *money.Money
	if r.resultingBalanceMinorUnits != nil {
		b, err := money.FromMinorUnits(*r.resultingBalanceMinorUnits, r.currency)
		if err != nil {
			return nil, fmt.Errorf("postgres: rehydrate resulting balance: %w", err)
		}
		resultingBalance = &b
	}
	return wager.Rehydrate(wager.RehydrateParams{
		ID:                             fromPgUUID(r.id),
		ProviderID:                     deref(r.providerID),
		ExternalTransactionID:          deref(r.externalTransactionID),
		IdempotencyKey:                 deref(r.idempotencyKey),
		PayloadHash:                    deref(r.payloadHash),
		WalletID:                       fromPgUUID(r.walletID),
		PlayerID:                       fromPgUUID(r.playerID),
		RoundID:                        deref(r.roundID),
		GameID:                         deref(r.gameID),
		Kind:                           wager.Kind(r.kind),
		Money:                          amount,
		ReferenceExternalTransactionID: deref(r.referenceExternalTransactionID),
		ResolvedReferenceTransactionID: fromPgUUIDPtr(r.resolvedReferenceTransactionID),
		Status:                         wager.Status(r.status),
		FailureCode:                    domainerr.FailureCode(deref(r.failureCode)),
		ResultingBalance:               resultingBalance,
		CreatedAt:                      r.createdAt,
		UpdatedAt:                      r.updatedAt,
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (r *WagerTransactionRepository) FindByID(ctx context.Context, id uuid.UUID) (*wager.Transaction, error) {
	row := db(ctx, r.pool).QueryRow(ctx, `SELECT `+wagerTxColumns+` FROM wager_transactions WHERE id = $1`, toPgUUID(id))
	return scanWagerTx(row)
}

func (r *WagerTransactionRepository) FindByProviderAndExternalID(ctx context.Context, providerID, externalTransactionID string) (*wager.Transaction, error) {
	row := db(ctx, r.pool).QueryRow(ctx,
		`SELECT `+wagerTxColumns+` FROM wager_transactions WHERE provider_id = $1 AND external_transaction_id = $2`,
		providerID, externalTransactionID)
	return scanWagerTx(row)
}

func (r *WagerTransactionRepository) FindByIdempotencyKey(ctx context.Context, providerID, idempotencyKey string) (*wager.Transaction, error) {
	row := db(ctx, r.pool).QueryRow(ctx,
		`SELECT `+wagerTxColumns+` FROM wager_transactions WHERE provider_id = $1 AND idempotency_key = $2`,
		providerID, idempotencyKey)
	return scanWagerTx(row)
}

func (r *WagerTransactionRepository) Create(ctx context.Context, tx *wager.Transaction) error {
	_, err := db(ctx, r.pool).Exec(ctx, `
		INSERT INTO wager_transactions (
			id, provider_id, external_transaction_id, idempotency_key, payload_hash,
			wallet_id, player_id, round_id, game_id, kind, amount_minor_units, currency,
			reference_external_transaction_id, resolved_reference_transaction_id,
			status, failure_code, resulting_balance_minor_units, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		toPgUUID(tx.ID()), nullableString(tx.ProviderID()), nullableString(tx.ExternalTransactionID()),
		nullableString(tx.IdempotencyKey()), nullableString(tx.PayloadHash()),
		toPgUUID(tx.WalletID()), toPgUUID(tx.PlayerID()), nullableString(tx.RoundID()), nullableString(tx.GameID()),
		string(tx.Kind()), tx.Money().MinorUnits(), tx.Money().Currency(),
		nullableString(tx.ReferenceExternalTransactionID()), toPgUUIDPtr(tx.ResolvedReferenceTransactionID()),
		string(tx.Status()), nullableString(string(tx.FailureCode())), resultingBalanceMinorUnits(tx),
		tx.CreatedAt(), tx.UpdatedAt(),
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return domainerr.ErrConflict
		}
		return fmt.Errorf("postgres: create wager transaction: %w", err)
	}
	return nil
}

func (r *WagerTransactionRepository) Save(ctx context.Context, tx *wager.Transaction) error {
	_, err := db(ctx, r.pool).Exec(ctx, `
		UPDATE wager_transactions
		SET status = $1, failure_code = $2, resolved_reference_transaction_id = $3,
		    resulting_balance_minor_units = $4, updated_at = $5
		WHERE id = $6`,
		string(tx.Status()), nullableString(string(tx.FailureCode())), toPgUUIDPtr(tx.ResolvedReferenceTransactionID()),
		resultingBalanceMinorUnits(tx), tx.UpdatedAt(), toPgUUID(tx.ID()),
	)
	if err != nil {
		return fmt.Errorf("postgres: save wager transaction: %w", err)
	}
	return nil
}

func resultingBalanceMinorUnits(tx *wager.Transaction) *int64 {
	if tx.ResultingBalance() == nil {
		return nil
	}
	v := tx.ResultingBalance().MinorUnits()
	return &v
}

func (r *WagerTransactionRepository) ListPendingReferenceDue(ctx context.Context, now time.Time, limit int) ([]*wager.Transaction, error) {
	rows, err := db(ctx, r.pool).Query(ctx, `
		SELECT `+wagerTxColumns+`
		FROM wager_transactions
		WHERE status = 'PENDING_REFERENCE' AND pending_reference_next_retry_at <= $1
		ORDER BY pending_reference_next_retry_at
		LIMIT $2
		FOR UPDATE SKIP LOCKED`,
		now, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: list pending-reference due: %w", err)
	}
	defer rows.Close()

	var result []*wager.Transaction
	for rows.Next() {
		tx, err := scanWagerTx(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, tx)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list pending-reference due: %w", err)
	}
	return result, nil
}

func (r *WagerTransactionRepository) RecordPendingReferenceAttempt(ctx context.Context, transactionID uuid.UUID, attempts int, nextRetryAt time.Time) error {
	_, err := db(ctx, r.pool).Exec(ctx, `
		UPDATE wager_transactions
		SET pending_reference_attempts = $1, pending_reference_next_retry_at = $2
		WHERE id = $3`,
		attempts, nextRetryAt, toPgUUID(transactionID))
	if err != nil {
		return fmt.Errorf("postgres: record pending-reference attempt: %w", err)
	}
	return nil
}

func (r *WagerTransactionRepository) CountSuccessfulReversals(ctx context.Context, referencedTransactionID uuid.UUID, kind wager.Kind) (int, error) {
	var count int
	err := db(ctx, r.pool).QueryRow(ctx, `
		SELECT COUNT(*) FROM wager_transactions
		WHERE resolved_reference_transaction_id = $1 AND kind = $2 AND status = 'PROCESSED'`,
		toPgUUID(referencedTransactionID), string(kind)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("postgres: count successful reversals: %w", err)
	}
	return count, nil
}
