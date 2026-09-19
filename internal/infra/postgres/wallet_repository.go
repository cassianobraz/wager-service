package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/cassianobraz/wager-service/internal/domain/domainerr"
	"github.com/cassianobraz/wager-service/internal/domain/money"
	"github.com/cassianobraz/wager-service/internal/domain/wallet"
	"github.com/cassianobraz/wager-service/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
	"uuid"
)

type WalletRepository struct {
	pool *pgxpool.Pool
}

func NewWalletRepository(pool *pgxpool.Pool) *WalletRepository {
	return &WalletRepository{pool: pool}
}

const walletColumns = `id, player_id, currency, balance_minor_units, version, created_at, updated_at`

func scanWallet(row pgx.Row) (*wallet.Wallet, error) {
	var (
		id, playerID      pgtype.UUID
		currency          string
		balanceMinorUnits int64
		version           int64
		createdAt         time.Time
		updatedAt         time.Time
	)
	if err := row.Scan(&id, &playerID, &currency, &balanceMinorUnits, &version, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domainerr.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: scan wallet: %w", err)
	}
	balance, err := money.FromMinorUnits(balanceMinorUnits, currency)
	if err != nil {
		return nil, fmt.Errorf("postgres: rehydrate wallet balance: %w", err)
	}
	return wallet.Rehydrate(fromPgUUID(id), fromPgUUID(playerID), balance, version, createdAt, updatedAt)
}

func (r *WalletRepository) FindByID(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	row := db(ctx, r.pool).QueryRow(ctx, `SELECT `+walletColumns+` FROM wallets WHERE id = $1`, toPgUUID(id))
	return scanWallet(row)
}

func (r *WalletRepository) FindByPlayerAndCurrency(ctx context.Context, playerID uuid.UUID, currency string) (*wallet.Wallet, error) {
	row := db(ctx, r.pool).QueryRow(ctx,
		`SELECT `+walletColumns+` FROM wallets WHERE player_id = $1 AND currency = $2`, toPgUUID(playerID), currency)
	return scanWallet(row)
}

func (r *WalletRepository) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	row := db(ctx, r.pool).QueryRow(ctx, `SELECT `+walletColumns+` FROM wallets WHERE id = $1 FOR UPDATE`, toPgUUID(id))
	return scanWallet(row)
}

func (r *WalletRepository) Create(ctx context.Context, w *wallet.Wallet) error {
	_, err := db(ctx, r.pool).Exec(ctx, `
		INSERT INTO wallets (id, player_id, currency, balance_minor_units, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		toPgUUID(w.ID()), toPgUUID(w.PlayerID()), w.Currency(), w.Balance().MinorUnits(), w.Version(), w.CreatedAt(), w.UpdatedAt())
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return domainerr.ErrConflict
		}
		return fmt.Errorf("postgres: create wallet: %w", err)
	}
	return nil
}

func (r *WalletRepository) Save(ctx context.Context, w *wallet.Wallet, expectedVersion int64) error {
	tag, err := db(ctx, r.pool).Exec(ctx, `
		UPDATE wallets
		SET balance_minor_units = $1, version = $2, updated_at = $3
		WHERE id = $4 AND version = $5`,
		w.Balance().MinorUnits(), w.Version(), w.UpdatedAt(), toPgUUID(w.ID()), expectedVersion)
	if err != nil {
		return fmt.Errorf("postgres: save wallet: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrOptimisticLock
	}
	return nil
}
