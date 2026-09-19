-- WagerTransaction aggregate. A single table holds both external
-- operations (BET/WIN/LOSS/REFUND/ROLLBACK, submitted by a provider over
-- HTTP or the broker) and the internal OPENING record created alongside a
-- new wallet -- OPENING simply leaves provider_id/external_transaction_id/
-- idempotency_key/payload_hash/round_id/game_id NULL, since it never goes
-- through the provider-facing idempotency machinery.
CREATE TABLE wager_transactions (
    id                                 UUID PRIMARY KEY,
    provider_id                        TEXT NULL,
    external_transaction_id            TEXT NULL,
    idempotency_key                    TEXT NULL,
    payload_hash                       TEXT NULL,
    wallet_id                          UUID NOT NULL REFERENCES wallets (id),
    player_id                          UUID NOT NULL,
    round_id                           TEXT NULL,
    game_id                            TEXT NULL,
    kind                               TEXT NOT NULL,
    amount_minor_units                 BIGINT NOT NULL,
    currency                           CHAR(3) NOT NULL,
    reference_external_transaction_id  TEXT NULL,
    resolved_reference_transaction_id  UUID NULL REFERENCES wager_transactions (id),
    status                             TEXT NOT NULL,
    failure_code                       TEXT NULL,
    resulting_balance_minor_units      BIGINT NULL,
    -- Pending-reference retry bookkeeping. This is infrastructure-only
    -- state (the domain aggregate has no notion of a retry schedule); it
    -- lives alongside the aggregate's own columns purely for locality,
    -- populated only while status = 'PENDING_REFERENCE'.
    pending_reference_attempts         INT NOT NULL DEFAULT 0,
    pending_reference_next_retry_at    TIMESTAMPTZ NULL,
    created_at                         TIMESTAMPTZ NOT NULL,
    updated_at                         TIMESTAMPTZ NOT NULL,

    CONSTRAINT wager_tx_kind_check
        CHECK (kind IN ('OPENING', 'BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK')),
    CONSTRAINT wager_tx_status_check
        CHECK (status IN ('PENDING', 'PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED')),
    CONSTRAINT wager_tx_amount_non_negative CHECK (amount_minor_units >= 0)
);

-- Idempotency: (providerId, externalTransactionId) and (providerId,
-- idempotencyKey) must each be unique among provider-submitted rows.
-- Partial indexes (WHERE provider_id IS NOT NULL) exclude the single
-- OPENING row per wallet, which never carries a provider_id.
CREATE UNIQUE INDEX wager_tx_provider_external_id_unique
    ON wager_transactions (provider_id, external_transaction_id)
    WHERE provider_id IS NOT NULL;

CREATE UNIQUE INDEX wager_tx_provider_idempotency_key_unique
    ON wager_transactions (provider_id, idempotency_key)
    WHERE provider_id IS NOT NULL;

CREATE INDEX wager_tx_wallet_id_idx ON wager_transactions (wallet_id);

-- Backs CountSuccessfulReversals(referencedTransactionId, kind), which
-- filters on resolved_reference_transaction_id + kind + status.
CREATE INDEX wager_tx_resolved_reference_idx
    ON wager_transactions (resolved_reference_transaction_id, kind, status)
    WHERE resolved_reference_transaction_id IS NOT NULL;

-- Backs ListPendingReferenceDue's `WHERE status = 'PENDING_REFERENCE' AND
-- pending_reference_next_retry_at <= now()` scan.
CREATE INDEX wager_tx_pending_reference_due_idx
    ON wager_transactions (pending_reference_next_retry_at)
    WHERE status = 'PENDING_REFERENCE';
