CREATE TABLE IF NOT EXISTS clb_event (
    id BIGSERIAL PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    source_service VARCHAR(64) NOT NULL,
    event_type VARCHAR(128) NOT NULL,
    event_key VARCHAR(128) NOT NULL,
    idempotency_key VARCHAR(191) NOT NULL,
    payload BYTEA NOT NULL,
    payload_content_type VARCHAR(64) NOT NULL DEFAULT 'application/json',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_by VARCHAR(128) NULL,
    locked_until TIMESTAMPTZ NULL,
    delivered_at TIMESTAMPTZ NULL,
    rejected_at TIMESTAMPTZ NULL,
    last_error TEXT NULL,
    reject_reason TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT clb_event_status_chk CHECK (status IN ('pending', 'processing', 'ok', 'reject')),
    CONSTRAINT clb_event_key_uq UNIQUE (source_service, event_key),
    CONSTRAINT clb_event_idempotency_uq UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS clb_event_due_idx
    ON clb_event (status, next_attempt_at, locked_until, id);

CREATE INDEX IF NOT EXISTS clb_event_type_idx
    ON clb_event (workspace_id, source_service, event_type, status, created_at);
