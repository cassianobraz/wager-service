-- Inbox: broker-delivery deduplication, keyed by (consumerName,
-- messageId). This is deliberately independent of the business-level
-- idempotency enforced on wager_transactions (providerId +
-- idempotencyKey): the inbox exists only to stop the *same broker
-- message* from being handled twice under at-least-once delivery,
-- regardless of what business operation it carries.
CREATE TABLE inbox_records (
    consumer_name TEXT NOT NULL,
    message_id    TEXT NOT NULL,
    payload_hash  TEXT NOT NULL,
    received_at   TIMESTAMPTZ NOT NULL,
    completed_at  TIMESTAMPTZ NULL,

    PRIMARY KEY (consumer_name, message_id)
);
