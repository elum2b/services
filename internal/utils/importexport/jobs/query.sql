-- name: LeaseJob :one
WITH due AS (
    SELECT id FROM importexport_job
    WHERE status IN ('queued', 'processing')
      AND (locked_until IS NULL OR locked_until <= now())
    ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED
)
UPDATE importexport_job AS job
SET status = 'processing', locked_by = $1, lease_token = $2,
    locked_until = now() + ($3 * interval '1 microsecond'),
    started_at = COALESCE(started_at, now()), updated_at = now()
FROM due WHERE job.id = due.id
RETURNING job.id, job.service, job.workspace_id, job.type, job.status, job.file_name,
          job.archive_key, job.archive_expires_at, job.error, job.locked_by,
          job.lease_token, job.locked_until, job.created_at, job.started_at,
          job.finished_at, job.updated_at;

-- name: CompleteJob :execrows
UPDATE importexport_job
SET status = 'completed', archive_key = $1, archive_expires_at = now() + ($2 * interval '1 microsecond'),
    error = NULL, locked_by = NULL, lease_token = NULL, locked_until = NULL, finished_at = now(), updated_at = now()
WHERE id = $3 AND status = 'processing' AND locked_by = $4 AND lease_token = $5 AND locked_until > now();

-- name: FailJob :execrows
UPDATE importexport_job
SET status = 'failed', error = $1, locked_by = NULL, lease_token = NULL, locked_until = NULL,
    finished_at = now(), updated_at = now()
WHERE id = $2 AND status = 'processing' AND locked_by = $3 AND lease_token = $4 AND locked_until > now();

-- name: ClaimExpiredArchives :many
WITH due AS (
    SELECT id FROM importexport_job
    WHERE archive_key IS NOT NULL AND archive_expires_at <= now()
      AND (archive_claimed_until IS NULL OR archive_claimed_until <= now())
    ORDER BY archive_expires_at, id LIMIT $1 FOR UPDATE SKIP LOCKED
)
UPDATE importexport_job AS job
SET archive_claim_token = $2,
    archive_claimed_until = now() + ($3 * interval '1 microsecond')
FROM due WHERE job.id = due.id
RETURNING job.id, job.archive_key, job.archive_claim_token, job.archive_claimed_until;

-- name: ClearArchive :execrows
UPDATE importexport_job
SET archive_key = NULL, archive_expires_at = NULL,
    archive_claim_token = NULL, archive_claimed_until = NULL, updated_at = now()
WHERE id = $1 AND archive_key = $2 AND archive_claim_token = $3;
