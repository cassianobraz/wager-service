-- Append-only WalletLedgerEntry. UPDATE and DELETE are blocked at the
-- database level (not merely by convention) via a trigger, so a bad
-- migration, a rogue script, or a future engineer cannot silently corrupt
-- the audit trail even with direct table access.
CREATE TABLE ledger_entries (
    id                         UUID PRIMARY KEY,
    wallet_id                  UUID NOT NULL REFERENCES wallets (id),
    transaction_id             UUID NOT NULL REFERENCES wager_transactions (id),
    direction                  TEXT NOT NULL,
    amount_minor_units         BIGINT NOT NULL,
    currency                   CHAR(3) NOT NULL,
    balance_before_minor_units BIGINT NOT NULL,
    balance_after_minor_units  BIGINT NOT NULL,
    created_at                 TIMESTAMPTZ NOT NULL,

    CONSTRAINT ledger_direction_check CHECK (direction IN ('DEBIT', 'CREDIT')),
    CONSTRAINT ledger_amount_positive CHECK (amount_minor_units > 0),
    CONSTRAINT ledger_balance_after_non_negative CHECK (balance_after_minor_units >= 0),
    -- One wallet can never carry two ledger entries for the same
    -- transaction: a transaction produces at most one movement.
    CONSTRAINT ledger_wallet_transaction_unique UNIQUE (wallet_id, transaction_id)
);

CREATE INDEX ledger_entries_wallet_id_created_at_idx
    ON ledger_entries (wallet_id, created_at, id);

CREATE OR REPLACE FUNCTION ledger_entries_immutable()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'ledger_entries is append-only: % is not permitted', TG_OP;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER ledger_entries_no_update
    BEFORE UPDATE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION ledger_entries_immutable();

CREATE TRIGGER ledger_entries_no_delete
    BEFORE DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION ledger_entries_immutable();
