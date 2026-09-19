-- Wallet aggregate: one row per (player_id, currency). Money is stored as
-- integer minor units (cents), never as a float or NUMERIC with implicit
-- rounding, matching the domain's money.Money representation.
CREATE TABLE wallets (
    id                  UUID PRIMARY KEY,
    player_id           UUID NOT NULL,
    currency            CHAR(3) NOT NULL,
    balance_minor_units BIGINT NOT NULL,
    version             BIGINT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,

    CONSTRAINT wallets_balance_non_negative CHECK (balance_minor_units >= 0),
    CONSTRAINT wallets_version_positive CHECK (version >= 1),
    CONSTRAINT wallets_player_currency_unique UNIQUE (player_id, currency)
);

-- Every wallet lookup in the hot path (FindByID, FindByIDForUpdate) is a
-- primary-key lookup already covered by the PK index; the natural-key
-- lookup used by wallet creation and by clients that only know the player
-- is covered by the unique constraint's backing index above.
CREATE INDEX wallets_player_id_idx ON wallets (player_id);
