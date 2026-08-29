CREATE TABLE IF NOT EXISTS importexport_job (
    id BIGSERIAL PRIMARY KEY,
    service VARCHAR(64) NOT NULL,
    workspace_id VARCHAR(36) NOT NULL,
    type VARCHAR(16) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'queued',
    file_name TEXT NOT NULL DEFAULT '',
    archive_key TEXT NULL,
    archive_expires_at TIMESTAMPTZ NULL,
    error TEXT NULL,
    locked_by VARCHAR(128) NULL,
    lease_token TEXT NULL,
    locked_until TIMESTAMPTZ NULL,
    archive_claim_token TEXT NULL,
    archive_claimed_until TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ NULL,
    finished_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT importexport_job_type_chk CHECK (type IN ('export', 'import')),
    CONSTRAINT importexport_job_status_chk CHECK (status IN ('queued', 'processing', 'completed', 'failed'))
);

CREATE UNIQUE INDEX IF NOT EXISTS importexport_job_active_workspace_uq
    ON importexport_job (service, workspace_id)
    WHERE status IN ('queued', 'processing');

CREATE INDEX IF NOT EXISTS importexport_job_due_idx
    ON importexport_job (status, locked_until, id);

CREATE INDEX IF NOT EXISTS importexport_job_history_idx
    ON importexport_job (service, workspace_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS importexport_job_archive_cleanup_idx
    ON importexport_job (archive_expires_at, archive_claimed_until, id)
    WHERE archive_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS importexport_job_history (
    id BIGSERIAL PRIMARY KEY,
    job_id BIGINT NOT NULL REFERENCES importexport_job(id) ON DELETE CASCADE,
    status VARCHAR(16) NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS importexport_job_history_job_idx
    ON importexport_job_history (job_id, id);
