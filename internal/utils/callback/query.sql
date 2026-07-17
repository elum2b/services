-- name: CreateEvent :one
INSERT INTO clb_event (
    workspace_id,
    source_service,
    event_type,
    event_key,
    idempotency_key,
    payload,
    payload_content_type,
    next_attempt_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (idempotency_key) DO UPDATE SET
    idempotency_key = EXCLUDED.idempotency_key
RETURNING id;

-- name: GetEvent :one
SELECT
    id,
    workspace_id,
    source_service,
    event_type,
    event_key,
    idempotency_key,
    payload,
    payload_content_type,
    status,
    attempt_count,
    next_attempt_at,
    locked_by,
    locked_until,
    delivered_at,
    rejected_at,
    last_error,
    reject_reason,
    created_at,
    updated_at
FROM clb_event
WHERE workspace_id = $1
  AND id = $2
LIMIT 1;

-- name: ListDueEventsForUpdate :many
SELECT
    id,
    workspace_id,
    source_service,
    event_type,
    event_key,
    idempotency_key,
    payload,
    payload_content_type,
    status,
    attempt_count,
    next_attempt_at,
    locked_by,
    locked_until,
    delivered_at,
    rejected_at,
    last_error,
    reject_reason,
    created_at,
    updated_at
FROM clb_event
WHERE ($1 = '' OR source_service = $2)
  AND status IN ('pending', 'processing')
  AND next_attempt_at <= NOW()
  AND (locked_until IS NULL OR locked_until <= NOW())
ORDER BY next_attempt_at, id
LIMIT $3
FOR UPDATE SKIP LOCKED;

-- name: MarkEventProcessing :execrows
UPDATE clb_event
SET status = 'processing',
    locked_by = $1,
    locked_until = $2,
    updated_at = NOW()
WHERE id = $3
  AND status IN ('pending', 'processing')
  AND (locked_until IS NULL OR locked_until <= NOW());

-- name: MarkEventOK :execrows
UPDATE clb_event
SET status = 'ok',
    delivered_at = NOW(),
    locked_by = NULL,
    locked_until = NULL,
    last_error = NULL,
    updated_at = NOW()
WHERE id = $1
  AND status = 'processing'
  AND locked_by = $2;

-- name: MarkEventReject :execrows
UPDATE clb_event
SET status = 'reject',
    rejected_at = NOW(),
    reject_reason = $1,
    locked_by = NULL,
    locked_until = NULL,
    updated_at = NOW()
WHERE id = $2
  AND status = 'processing'
  AND locked_by = $3;

-- name: MarkEventFailed :execrows
UPDATE clb_event
SET status = 'pending',
    attempt_count = attempt_count + 1,
    next_attempt_at = $1,
    locked_by = NULL,
    locked_until = NULL,
    last_error = $2,
    updated_at = NOW()
WHERE id = $3
  AND status = 'processing'
  AND locked_by = $4;

-- name: AdminListEvents :many
SELECT
    id,
    workspace_id,
    source_service,
    event_type,
    event_key,
    idempotency_key,
    payload,
    payload_content_type,
    status,
    attempt_count,
    next_attempt_at,
    locked_by,
    locked_until,
    delivered_at,
    rejected_at,
    last_error,
    reject_reason,
    created_at,
    updated_at
FROM clb_event
WHERE workspace_id = $1
  AND ($2 = '' OR source_service = $3)
  AND ($4 = '' OR event_type = $5)
  AND ($6 = '' OR status = $7)
ORDER BY created_at DESC, id DESC
LIMIT $8 OFFSET $9;

-- name: AdminRetryEventNow :execrows
UPDATE clb_event
SET status = 'pending',
    next_attempt_at = NOW(),
    locked_by = NULL,
    locked_until = NULL,
    last_error = NULL,
    updated_at = NOW()
WHERE workspace_id = $1
  AND id = $2
  AND status IN ('pending', 'processing');

-- name: AdminMarkEventOK :execrows
UPDATE clb_event
SET status = 'ok',
    delivered_at = NOW(),
    locked_by = NULL,
    locked_until = NULL,
    last_error = NULL,
    updated_at = NOW()
WHERE workspace_id = $1
  AND id = $2
  AND status IN ('pending', 'processing');

-- name: AdminMarkEventReject :execrows
UPDATE clb_event
SET status = 'reject',
    rejected_at = NOW(),
    reject_reason = $1,
    locked_by = NULL,
    locked_until = NULL,
    updated_at = NOW()
WHERE workspace_id = $2
  AND id = $3
  AND status IN ('pending', 'processing');

-- name: AdminResetExpiredProcessing :execrows
UPDATE clb_event
SET status = 'pending',
    locked_by = NULL,
    locked_until = NULL,
    next_attempt_at = NOW(),
    updated_at = NOW()
WHERE workspace_id = $1
  AND status = 'processing'
  AND locked_until IS NOT NULL
  AND locked_until <= NOW();
