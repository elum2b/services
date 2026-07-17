-- name: AdminUpsertOffer :exec
INSERT INTO cpa_offer (
    workspace_id, id, payload, target, code_mode, code_source, shared_code,
    generated_length, generated_alphabet, is_active, start_at, end_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (workspace_id, id) DO UPDATE SET
    payload = EXCLUDED.payload,
    target = EXCLUDED.target,
    code_mode = EXCLUDED.code_mode,
    code_source = EXCLUDED.code_source,
    shared_code = EXCLUDED.shared_code,
    generated_length = EXCLUDED.generated_length,
    generated_alphabet = EXCLUDED.generated_alphabet,
    is_active = EXCLUDED.is_active,
    start_at = EXCLUDED.start_at,
    end_at = EXCLUDED.end_at,
    updated_at = now();

-- name: AdminGetOffer :one
SELECT *
FROM cpa_offer
WHERE workspace_id = $1 AND id = $2
LIMIT 1;

-- name: GetActiveOffer :one
SELECT *
FROM cpa_offer
WHERE workspace_id = $1
  AND id = $2
  AND is_active = TRUE
  AND (start_at IS NULL OR start_at <= now())
  AND (end_at IS NULL OR end_at > now())
LIMIT 1;

-- name: AdminListOfferIDs :many
SELECT id
FROM cpa_offer
WHERE workspace_id = $1;

-- name: AdminListOfferBundles :many
SELECT
    o.*,
    l.locale,
    l.title AS localization_title,
    l.description AS localization_description
FROM (
    SELECT *
    FROM cpa_offer
    WHERE cpa_offer.workspace_id = $1
    ORDER BY cpa_offer.created_at DESC, cpa_offer.id
    LIMIT NULLIF(sqlc.arg(page_limit)::integer, 0) OFFSET sqlc.arg(page_offset)::integer
) o
LEFT JOIN cpa_localization l
    ON l.workspace_id = o.workspace_id
   AND l.cpa_id = o.id
ORDER BY o.created_at DESC, o.id, l.locale;

-- name: AdminListOfferBundleRewards :many
SELECT
    o.workspace_id,
    o.id AS cpa_id,
    r.reward_key,
    r.reward_type,
    r.quantity AS reward_quantity,
    r.scale AS reward_scale,
    r.duration_unit
FROM (
    SELECT *
    FROM cpa_offer
    WHERE cpa_offer.workspace_id = $1
    ORDER BY cpa_offer.created_at DESC, cpa_offer.id
    LIMIT NULLIF(sqlc.arg(page_limit)::integer, 0) OFFSET sqlc.arg(page_offset)::integer
) o
JOIN cpa_reward r
    ON r.workspace_id = o.workspace_id
   AND r.cpa_id = o.id
ORDER BY o.created_at DESC, o.id, r.id;

-- name: ListActiveOfferCatalog :many
WITH active AS MATERIALIZED (
    SELECT o.*
    FROM cpa_offer o
    WHERE o.workspace_id = $2
      AND o.is_active = TRUE
)
SELECT
    o.workspace_id,
    o.id,
    o.payload,
    o.target,
    o.code_mode,
    o.code_source,
    o.shared_code,
    o.generated_length,
    o.generated_alphabet,
    o.is_active,
    o.start_at,
    o.end_at,
    o.created_at,
    o.updated_at,
    l.locale AS localized_locale,
    l.title AS localized_title,
    l.description AS localized_description,
    r.reward_key,
    r.reward_type,
    r.quantity AS reward_quantity,
    r.scale AS reward_scale,
    r.duration_unit
FROM active o
LEFT JOIN cpa_localization l
    ON l.workspace_id = o.workspace_id
   AND l.cpa_id = o.id
   AND l.locale = $1
LEFT JOIN cpa_reward r
    ON r.workspace_id = o.workspace_id
   AND r.cpa_id = o.id
ORDER BY o.created_at DESC, o.id, r.id;

-- name: AdminDeleteOffer :execrows
DELETE FROM cpa_offer
WHERE workspace_id = $1 AND id = $2;

-- name: AdminUpsertLocalization :exec
WITH workspace_lock AS MATERIALIZED (
    SELECT pg_advisory_xact_lock(hashtextextended($1, 0))
)
INSERT INTO cpa_localization (
    workspace_id, cpa_id, locale, title, description
)
SELECT $1, $2, $3, $4, $5
FROM workspace_lock
ON CONFLICT (workspace_id, cpa_id, locale) DO UPDATE SET
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    updated_at = now();

-- name: GetLocalization :one
SELECT *
FROM cpa_localization
WHERE workspace_id = $1 AND cpa_id = $2 AND locale = $3
LIMIT 1;

-- name: ListLocalizations :many
SELECT *
FROM cpa_localization
WHERE workspace_id = $1 AND cpa_id = $2
ORDER BY locale;

-- name: AdminDeleteLocalization :execrows
DELETE FROM cpa_localization
WHERE workspace_id = $1 AND cpa_id = $2 AND locale = $3;

-- name: AdminUpsertReward :exec
WITH workspace_lock AS MATERIALIZED (
    SELECT pg_advisory_xact_lock(hashtextextended($1, 0))
)
INSERT INTO cpa_reward (
    workspace_id, cpa_id, reward_key, reward_type, quantity, scale, duration_unit
)
SELECT $1, $2, $3, $4, $5, $6, $7
FROM workspace_lock
ON CONFLICT (workspace_id, cpa_id, reward_key) DO UPDATE SET
    reward_type = EXCLUDED.reward_type,
    quantity = EXCLUDED.quantity,
    scale = EXCLUDED.scale,
    duration_unit = EXCLUDED.duration_unit,
    updated_at = now();

-- name: ListRewards :many
SELECT *
FROM cpa_reward
WHERE workspace_id = $1 AND cpa_id = $2
ORDER BY id;

-- name: AdminDeleteReward :execrows
DELETE FROM cpa_reward
WHERE workspace_id = $1 AND cpa_id = $2 AND reward_key = $3;

-- name: AdminAddCode :execrows
INSERT INTO cpa_code (workspace_id, cpa_id, code, source)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, cpa_id, code) DO NOTHING;

-- name: CreateGeneratedCode :one
INSERT INTO cpa_code (workspace_id, cpa_id, code, source)
VALUES ($1, $2, $3, 'generated')
RETURNING id;

-- name: GetAvailableCodeForUpdate :one
SELECT *
FROM cpa_code
WHERE workspace_id = $1
  AND cpa_id = $2
  AND source = 'pool'
  AND status = 'available'
ORDER BY id
LIMIT 1
FOR UPDATE SKIP LOCKED;

-- name: GetCodeByValue :one
SELECT *
FROM cpa_code
WHERE workspace_id = $1 AND cpa_id = $2 AND code = $3
LIMIT 1;

-- name: MarkCodeIssued :execrows
UPDATE cpa_code
SET status = 'issued', updated_at = now()
WHERE id = $1 AND status = 'available';

-- name: MarkCodeCompleted :execrows
UPDATE cpa_code
SET status = 'completed', updated_at = now()
WHERE id = $1 AND status = 'issued';

-- name: GetAssignment :one
SELECT *
FROM cpa_assignment
WHERE workspace_id = $1
  AND cpa_id = $2
  AND app_id = $3
  AND platform_id = $4
  AND platform_user_id = $5
LIMIT 1;

-- name: GetAssignmentForUpdate :one
SELECT *
FROM cpa_assignment
WHERE workspace_id = $1
  AND cpa_id = $2
  AND app_id = $3
  AND platform_id = $4
  AND platform_user_id = $5
LIMIT 1
FOR UPDATE;

-- name: CreateAssignment :one
INSERT INTO cpa_assignment (
    workspace_id, cpa_id, app_id, platform_id, platform_user_id,
    code_id, code, code_mode, rewards_snapshot
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id;

-- name: GetAssignmentByID :one
SELECT *
FROM cpa_assignment
WHERE workspace_id = $1 AND id = $2
LIMIT 1;

-- name: CompleteAssignment :execrows
UPDATE cpa_assignment
SET status = 'completed', completed_at = now(), updated_at = now()
WHERE workspace_id = $1
  AND id = $2
  AND status = 'issued';

-- name: ListUserAssignments :many
SELECT *
FROM cpa_assignment
WHERE workspace_id = $1
  AND app_id = $2
  AND platform_id = $3
  AND platform_user_id = $4
ORDER BY issued_at DESC, id DESC;

-- name: AdminListAssignments :many
SELECT *
FROM cpa_assignment
WHERE workspace_id = $1
  AND cpa_id = $2
  AND (NULLIF($3, '')::cpa_assignment_status IS NULL OR status = NULLIF($3, '')::cpa_assignment_status)
ORDER BY issued_at DESC, id DESC
LIMIT $4 OFFSET $5;

-- name: AdminListCodes :many
SELECT *
FROM cpa_code
WHERE workspace_id = $1
  AND cpa_id = $2
  AND (NULLIF($3, '')::cpa_code_status IS NULL OR status = NULLIF($3, '')::cpa_code_status)
ORDER BY id DESC
LIMIT $4 OFFSET $5;

-- name: AdminListAssignmentEvents :many
SELECT *
FROM cpa_assignment_event
WHERE workspace_id = $1
  AND cpa_id = $2
  AND (NULLIF($3, '')::cpa_assignment_event_type IS NULL OR event_type = NULLIF($3, '')::cpa_assignment_event_type)
ORDER BY occurred_at DESC, id DESC
LIMIT $4 OFFSET $5;

-- name: CreateAssignmentEvent :one
INSERT INTO cpa_assignment_event (
    workspace_id, cpa_id, assignment_id, event_type
) VALUES ($1, $2, $3, $4)
ON CONFLICT (assignment_id, event_type) DO UPDATE SET
    event_type = EXCLUDED.event_type
RETURNING id;

-- name: AdminDeleteAvailableCodes :execrows
UPDATE cpa_code
SET status = 'deleted', deleted_at = now(), updated_at = now()
WHERE workspace_id = $1
  AND cpa_id = $2
  AND status = 'available';

-- name: AdminDeleteIssuedCodes :execrows
UPDATE cpa_code c
SET status = 'deleted', deleted_at = now(), updated_at = now()
FROM cpa_assignment a
WHERE a.code_id = c.id
  AND a.workspace_id = $1
  AND a.cpa_id = $2
  AND a.status = 'issued'
  AND c.status = 'issued';

-- name: AdminDeleteCompletedCodes :execrows
UPDATE cpa_code c
SET status = 'deleted', deleted_at = now(), updated_at = now()
FROM cpa_assignment a
WHERE a.code_id = c.id
  AND a.workspace_id = $1
  AND a.cpa_id = $2
  AND a.status = 'completed'
  AND c.status = 'completed';

-- name: AdminGetOfferStats :one
SELECT
    COUNT(*) AS assignments_total,
    COALESCE(SUM((status = 'issued')::int), 0)::bigint AS issued_total,
    COALESCE(SUM((status = 'completed')::int), 0)::bigint AS completed_total,
    COALESCE(SUM((deleted_at IS NOT NULL)::int), 0)::bigint AS deleted_total
FROM cpa_assignment
WHERE workspace_id = $1 AND cpa_id = $2;

-- name: AdminGetCodeStats :one
SELECT
    COUNT(*) AS codes_total,
    COALESCE(SUM((status = 'available')::int), 0)::bigint AS available_total,
    COALESCE(SUM((status = 'issued')::int), 0)::bigint AS issued_total,
    COALESCE(SUM((status = 'completed')::int), 0)::bigint AS completed_total,
    COALESCE(SUM((status = 'deleted')::int), 0)::bigint AS deleted_total
FROM cpa_code
WHERE workspace_id = $1 AND cpa_id = $2;

-- name: AdminListDailyStats :many
SELECT *
FROM cpa_stats_daily
WHERE workspace_id = $1
  AND cpa_id = $2
  AND stats_date >= $3
  AND stats_date <= $4
ORDER BY stats_date;

-- name: RefreshDailyStats :exec
INSERT INTO cpa_stats_daily (
    workspace_id, cpa_id, stats_date,
    issued_count, completed_count, unique_users
)
SELECT
    e.workspace_id,
    e.cpa_id,
    (e.occurred_at AT TIME ZONE 'UTC')::date,
    SUM((e.event_type = 'issued')::int)::bigint,
    SUM((e.event_type = 'completed')::int)::bigint,
    COUNT(DISTINCT e.assignment_id)::bigint
FROM cpa_assignment_event e
WHERE e.workspace_id = sqlc.arg(refresh_workspace_id)
  AND e.occurred_at >= $1
  AND e.occurred_at < $2
GROUP BY e.workspace_id, e.cpa_id, (e.occurred_at AT TIME ZONE 'UTC')::date
ON CONFLICT (workspace_id, cpa_id, stats_date) DO UPDATE SET
    issued_count = EXCLUDED.issued_count,
    completed_count = EXCLUDED.completed_count,
    unique_users = EXCLUDED.unique_users,
    updated_at = now();
