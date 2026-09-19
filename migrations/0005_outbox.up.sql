-- Transactional outbox: every event a use case appends here is committed
-- in the exact same database transaction as the domain change that
-- produced it, so a publisher can never observe an event whose cause was
-- rolled back. The envelope is stored verbatim (as JSONB) so the
-- publisher republishes byte-for-byte what was decided at commit time,
-- never a value recomputed from current state.
CREATE TABLE outbox_records (
    event_id      UUID PRIMARY KEY,
    aggregate_id  UUID NOT NULL,
    event_type    TEXT NOT NULL,
    envelope      JSONB NOT NULL,
    occurred_at   TIMESTAMPTZ NOT NULL,
    attempts      INT NOT NULL DEFAULT 0,
    next_attempt  TIMESTAMPTZ NOT NULL,
    published_at  TIMESTAMPTZ NULL
);

-- Backs ClaimPending's `WHERE published_at IS NULL AND next_attempt <=
-- now()` scan, ordered so multiple publisher instances (using SELECT ...
-- FOR UPDATE SKIP LOCKED at the query level) drain the oldest events
-- first.
CREATE INDEX outbox_records_claimable_idx
    ON outbox_records (next_attempt, event_id)
    WHERE published_at IS NULL;
