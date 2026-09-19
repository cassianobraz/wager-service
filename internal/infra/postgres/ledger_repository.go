package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/cassianobraz/wager-service/internal/domain/domainerr"
	"github.com/cassianobraz/wager-service/internal/domain/ledger"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
	"uuid"
)

type LedgerRepository struct {
	pool *pgxpool.Pool
}

func NewLedgerRepository(pool *pgxpool.Pool) *LedgerRepository {
	return &LedgerRepository{pool: pool}
}

func (r *LedgerRepository) Append(ctx context.Context, e *ledger.Entry) error {
	_, err := db(ctx, r.pool).Exec(ctx, `
		INSERT INTO ledger_entries (
			id, wallet_id, transaction_id, direction, amount_minor_units, currency,
			balance_before_minor_units, balance_after_minor_units, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		toPgUUID(e.ID()), toPgUUID(e.WalletID()), toPgUUID(e.TransactionID()), string(e.Direction()),
		e.Money().MinorUnits(), e.Money().Currency(),
		e.BalanceBefore().MinorUnits(), e.BalanceAfter().MinorUnits(), e.CreatedAt(),
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case pgUniqueViolation:
				return domainerr.ErrConflict
			}
		}
		return fmt.Errorf("postgres: append ledger entry: %w", err)
	}
	return nil
}

type ledgerCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

func encodeLedgerCursor(c ledgerCursor) string {
	raw := c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeLedgerCursor(s string) (ledgerCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return ledgerCursor{}, fmt.Errorf("postgres: invalid ledger cursor: %w", err)
	}
	parts := splitOnce(string(raw), '|')
	if len(parts) != 2 {
		return ledgerCursor{}, fmt.Errorf("postgres: malformed ledger cursor")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return ledgerCursor{}, fmt.Errorf("postgres: invalid ledger cursor timestamp: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return ledgerCursor{}, fmt.Errorf("postgres: invalid ledger cursor id: %w", err)
	}
	return ledgerCursor{CreatedAt: createdAt, ID: id}, nil
}

func splitOnce(s string, sep byte) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}

func (r *LedgerRepository) ListByWallet(ctx context.Context, walletID uuid.UUID, cursor string, limit int) ([]*ledger.Entry, string, error) {
	if limit <= 0 {
		limit = 50
	}

	var (
		rows pgx.Rows
		err  error
	)
	if cursor == "" {
		rows, err = db(ctx, r.pool).Query(ctx, `
			SELECT id, wallet_id, transaction_id, direction, amount_minor_units, currency,
			       balance_before_minor_units, balance_after_minor_units, created_at
			FROM ledger_entries
			WHERE wallet_id = $1
			ORDER BY created_at, id
			LIMIT $2`, toPgUUID(walletID), limit+1)
	} else {
		c, decodeErr := decodeLedgerCursor(cursor)
		if decodeErr != nil {
			return nil, "", decodeErr
		}
		rows, err = db(ctx, r.pool).Query(ctx, `
			SELECT id, wallet_id, transaction_id, direction, amount_minor_units, currency,
			       balance_before_minor_units, balance_after_minor_units, created_at
			FROM ledger_entries
			WHERE wallet_id = $1 AND (created_at, id) > ($2, $3)
			ORDER BY created_at, id
			LIMIT $4`, toPgUUID(walletID), c.CreatedAt, toPgUUID(c.ID), limit+1)
	}
	if err != nil {
		return nil, "", fmt.Errorf("postgres: list ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []*ledger.Entry
	for rows.Next() {
		var (
			id, wID, txID      pgtype.UUID
			direction          string
			amountMinorUnits   int64
			currency           string
			balanceBeforeMinor int64
			balanceAfterMinor  int64
			createdAt          time.Time
		)
		if err := rows.Scan(&id, &wID, &txID, &direction, &amountMinorUnits, &currency,
			&balanceBeforeMinor, &balanceAfterMinor, &createdAt); err != nil {
			return nil, "", fmt.Errorf("postgres: scan ledger entry: %w", err)
		}
		amount, err := money.FromMinorUnits(amountMinorUnits, currency)
		if err != nil {
			return nil, "", err
		}
		before, err := money.FromMinorUnits(balanceBeforeMinor, currency)
		if err != nil {
			return nil, "", err
		}
		after, err := money.FromMinorUnits(balanceAfterMinor, currency)
		if err != nil {
			return nil, "", err
		}
		entry, err := ledger.Rehydrate(fromPgUUID(id), fromPgUUID(wID), fromPgUUID(txID), ledger.Direction(direction), amount, before, after, createdAt)
		if err != nil {
			return nil, "", err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("postgres: list ledger entries: %w", err)
	}

	var nextCursor string
	if len(entries) > limit {
		last := entries[limit-1]
		nextCursor = encodeLedgerCursor(ledgerCursor{CreatedAt: last.CreatedAt(), ID: last.ID()})
		entries = entries[:limit]
	}
	return entries, nextCursor, nil
}

func (r *LedgerRepository) SumByWallet(ctx context.Context, walletID uuid.UUID) (int64, string, int, error) {
	var (
		sum      *int64
		currency *string
		count    int
	)
	err := db(ctx, r.pool).QueryRow(ctx, `
		SELECT
			SUM(CASE WHEN direction = 'CREDIT' THEN amount_minor_units ELSE -amount_minor_units END),
			MAX(currency),
			COUNT(*)
		FROM ledger_entries
		WHERE wallet_id = $1`, toPgUUID(walletID)).Scan(&sum, &currency, &count)
	if err != nil {
		return 0, "", 0, fmt.Errorf("postgres: sum ledger by wallet: %w", err)
	}
	if sum == nil || currency == nil {
		return 0, "", 0, nil
	}
	return *sum, *currency, count, nil
}
