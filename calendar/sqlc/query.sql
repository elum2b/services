-- name: AdminCreateCalendar :exec
INSERT INTO calendar_definition (
    id, workspace_id, type, mode, interval_type, interval_unit,
    interval_count, reset_after_intervals, end_behavior, timezone,
    hide_future_rewards, is_active, start_at, end_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14);

-- name: AdminUpdateCalendar :execrows
UPDATE calendar_definition
SET type = $1,
    mode = $2,
    interval_type = $3,
    interval_unit = $4,
    interval_count = $5,
    reset_after_intervals = $6,
    end_behavior = $7,
    timezone = $8,
    hide_future_rewards = $9,
    is_active = $10,
    start_at = $11,
    end_at = $12,
    updated_at = now()
WHERE workspace_id = $13 AND id = $14 AND deleted_at IS NULL;

-- name: AdminGetCalendar :one
SELECT *
FROM calendar_definition
WHERE workspace_id = $1 AND id = $2
LIMIT 1;

-- name: AdminListCalendars :many
SELECT *
FROM calendar_definition
WHERE workspace_id = $1
ORDER BY created_at DESC, id
LIMIT $2 OFFSET $3;

-- name: ListExportCalendars :many
SELECT *
FROM calendar_definition
WHERE workspace_id = $1
ORDER BY created_at DESC, id;

-- name: ListExportLocalizations :many
SELECT *
FROM calendar_localization
WHERE workspace_id = $1
ORDER BY calendar_id, locale;

-- name: ListExportStepsWithRewards :many
SELECT
    s.calendar_id,
    s.id AS step_id,
    s.position AS step_position,
    r.item_key AS reward_item_key,
    r.reward_type AS reward_type,
    r.item_count AS reward_item_count,
    r.scale AS reward_scale,
    r.duration_unit AS reward_duration_unit,
    r.position AS reward_position
FROM calendar_step s
LEFT JOIN calendar_reward r
  ON r.workspace_id = s.workspace_id
 AND r.calendar_id = s.calendar_id
 AND r.step_id = s.id
WHERE s.workspace_id = $1
ORDER BY s.calendar_id, s.position, r.position, r.id;

-- name: AdminSetCalendarActive :execrows
UPDATE calendar_definition
SET is_active = $1,
    updated_at = now()
WHERE workspace_id = $2 AND id = $3 AND deleted_at IS NULL;

-- name: AdminSoftDeleteCalendar :execrows
UPDATE calendar_definition
SET deleted_at = now(),
    is_active = FALSE,
    updated_at = now()
WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: AdminUpsertLocalization :exec
INSERT INTO calendar_localization (
    workspace_id, calendar_id, locale, title, description
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, calendar_id, locale) DO UPDATE SET
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    updated_at = now();

-- name: AdminGetLocalization :one
SELECT *
FROM calendar_localization
WHERE workspace_id = $1 AND calendar_id = $2 AND locale = $3
LIMIT 1;

-- name: AdminListLocalizations :many
SELECT *
FROM calendar_localization
WHERE workspace_id = $1 AND calendar_id = $2
ORDER BY locale;

-- name: AdminDeleteLocalization :execrows
DELETE FROM calendar_localization
WHERE workspace_id = $1 AND calendar_id = $2 AND locale = $3;

-- name: AdminCreateStep :one
INSERT INTO calendar_step (workspace_id, calendar_id, position)
VALUES ($1, $2, $3)
RETURNING id;

-- name: AdminUpdateStep :execrows
UPDATE calendar_step
SET position = $1,
    updated_at = now()
WHERE workspace_id = $2 AND calendar_id = $3 AND id = $4;

-- name: AdminDeleteStep :execrows
DELETE FROM calendar_step
WHERE workspace_id = $1 AND calendar_id = $2 AND id = $3;

-- name: AdminUpsertReward :one
INSERT INTO calendar_reward (
    workspace_id, calendar_id, step_id, item_key,
    reward_type, item_count, scale, duration_unit, position
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (workspace_id, calendar_id, step_id, item_key) DO UPDATE SET
    reward_type = EXCLUDED.reward_type,
    item_count = EXCLUDED.item_count,
    scale = EXCLUDED.scale,
    duration_unit = EXCLUDED.duration_unit,
    position = EXCLUDED.position,
    updated_at = now()
RETURNING id;

-- name: AdminGetReward :one
SELECT *
FROM calendar_reward
WHERE workspace_id = $1 AND calendar_id = $2 AND id = $3
LIMIT 1;

-- name: AdminUpdateReward :execrows
UPDATE calendar_reward
SET step_id = $1,
    item_key = $2,
    reward_type = $3,
    item_count = $4,
    scale = $5,
    duration_unit = $6,
    position = $7,
    updated_at = now()
WHERE workspace_id = $8 AND calendar_id = $9 AND id = $10;

-- name: AdminDeleteReward :execrows
DELETE FROM calendar_reward
WHERE workspace_id = $1 AND calendar_id = $2 AND id = $3;

-- name: GetCalendarBundle :many
SELECT
    c.id,
    c.workspace_id,
    c.type,
    c.mode,
    c.interval_type,
    c.interval_unit,
    c.interval_count,
    c.reset_after_intervals,
    c.end_behavior,
    c.timezone,
    c.hide_future_rewards,
    c.is_active,
    c.start_at,
    c.end_at,
    c.deleted_at,
    c.created_at,
    c.updated_at,
    l.locale AS localization_locale,
    l.title AS localization_title,
    l.description AS localization_description,
    s.id AS step_id,
    s.position AS step_position,
    r.id AS reward_id,
    r.item_key AS reward_item_key,
    r.reward_type AS reward_type,
    r.item_count AS reward_item_count,
    r.scale AS reward_scale,
    r.duration_unit AS reward_duration_unit,
    r.position AS reward_position
FROM calendar_definition c
LEFT JOIN calendar_localization l
  ON l.workspace_id = c.workspace_id
 AND l.calendar_id = c.id
 AND l.locale = $1
LEFT JOIN calendar_step s
  ON s.workspace_id = c.workspace_id
 AND s.calendar_id = c.id
LEFT JOIN calendar_reward r
  ON r.workspace_id = s.workspace_id
 AND r.calendar_id = s.calendar_id
 AND r.step_id = s.id
WHERE c.workspace_id = $2
  AND (c.id = $3 OR c.type = $4)
ORDER BY s.position, r.position, r.id;

-- name: ListActiveCalendars :many
SELECT
    c.id,
    c.workspace_id,
    c.type,
    c.mode,
    c.is_active,
    c.start_at,
    c.end_at,
    c.deleted_at,
    l.locale,
    l.title,
    l.description
FROM calendar_definition c
LEFT JOIN calendar_localization l
  ON l.workspace_id = c.workspace_id
 AND l.calendar_id = c.id
 AND l.locale = $1
WHERE c.workspace_id = $2
ORDER BY c.created_at DESC, c.id;

-- name: GetRecordBundleForUpdate :many
SELECT
    c.id,
    c.workspace_id,
    c.type,
    c.mode,
    c.interval_type,
    c.interval_unit,
    c.interval_count,
    c.reset_after_intervals,
    c.end_behavior,
    c.timezone,
    c.hide_future_rewards,
    c.is_active,
    c.start_at,
    c.end_at,
    c.deleted_at,
    c.created_at,
    c.updated_at,
    p.current_position,
    p.claim_count,
    p.last_claim_position,
    p.last_claim_at,
    p.next_claim_at,
    p.is_completed,
    p.reset_count,
    p.last_was_reset,
    o.id AS operation_row_id,
    o.operation_id AS existing_operation_id,
    o.granted AS operation_granted,
    o.status AS operation_status,
    o.position AS operation_position,
    COALESCE(o.rewards_snapshot, '[]'::jsonb) AS operation_rewards_snapshot,
    o.current_position AS operation_current_position,
    o.claim_count AS operation_claim_count,
    o.last_claim_position AS operation_last_claim_position,
    o.last_claim_at AS operation_last_claim_at,
    o.next_claim_at AS operation_next_claim_at,
    o.is_completed AS operation_is_completed,
    o.reset_count AS operation_reset_count,
    o.was_reset AS operation_was_reset,
    o.occurred_at AS operation_occurred_at,
    s.id AS step_id,
    s.position AS step_position,
    r.id AS reward_id,
    r.item_key AS reward_item_key,
    r.reward_type AS reward_type,
    r.item_count AS reward_item_count,
    r.scale AS reward_scale,
    r.duration_unit AS reward_duration_unit,
    r.position AS reward_position
FROM calendar_definition c
LEFT JOIN calendar_progress p
  ON p.workspace_id = c.workspace_id
 AND p.calendar_id = c.id
 AND p.app_id = $1
 AND p.platform_id = $2
 AND p.platform_user_id = $3
LEFT JOIN calendar_operation o
  ON o.workspace_id = c.workspace_id
 AND o.calendar_id = c.id
 AND o.app_id = $4
 AND o.platform_id = $5
 AND o.platform_user_id = $6
 AND o.operation_id = $7
LEFT JOIN calendar_step s
  ON s.workspace_id = c.workspace_id
 AND s.calendar_id = c.id
LEFT JOIN calendar_reward r
  ON r.workspace_id = s.workspace_id
 AND r.calendar_id = s.calendar_id
 AND r.step_id = s.id
WHERE c.workspace_id = $8
  AND (c.id = $9 OR c.type = $10)
ORDER BY s.position, r.position, r.id
FOR UPDATE OF c;

-- name: EnsureProgressForUpdate :exec
INSERT INTO calendar_progress (
    workspace_id,
    calendar_id,
    app_id,
    platform_id,
    platform_user_id
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, calendar_id, app_id, platform_id, platform_user_id) DO NOTHING;

-- name: LockProgressForUpdate :one
SELECT 1::int
FROM calendar_progress
WHERE workspace_id = $1
  AND calendar_id = $2
  AND app_id = $3
  AND platform_id = $4
  AND platform_user_id = $5
FOR UPDATE;

-- name: CreateOperation :one
INSERT INTO calendar_operation (
    workspace_id, calendar_id, app_id, platform_id, platform_user_id,
    operation_id, granted, status, position, rewards_snapshot,
    current_position, claim_count, last_claim_position, last_claim_at,
    next_claim_at, is_completed, reset_count, was_reset, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
RETURNING id;

-- name: UpsertProgress :exec
INSERT INTO calendar_progress (
    workspace_id, calendar_id, app_id, platform_id, platform_user_id,
    current_position, claim_count, last_claim_position, last_claim_at,
    next_claim_at, is_completed, reset_count, last_was_reset
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (workspace_id, calendar_id, app_id, platform_id, platform_user_id) DO UPDATE SET
    current_position = EXCLUDED.current_position,
    claim_count = EXCLUDED.claim_count,
    last_claim_position = EXCLUDED.last_claim_position,
    last_claim_at = EXCLUDED.last_claim_at,
    next_claim_at = EXCLUDED.next_claim_at,
    is_completed = EXCLUDED.is_completed,
    reset_count = EXCLUDED.reset_count,
    last_was_reset = EXCLUDED.last_was_reset,
    updated_at = now();

-- name: GetProgress :one
SELECT *
FROM calendar_progress
WHERE workspace_id = $1
  AND calendar_id = $2
  AND app_id = $3
  AND platform_id = $4
  AND platform_user_id = $5
LIMIT 1;

-- name: AdminListOperations :many
SELECT *
FROM calendar_operation
WHERE workspace_id = $1 AND calendar_id = $2
ORDER BY occurred_at DESC, id DESC
LIMIT $3 OFFSET $4;

-- name: AdminGetStats :one
SELECT
    COUNT(*)::bigint AS operation_count,
    COUNT(*) FILTER (WHERE granted)::bigint AS grant_count,
    COUNT(DISTINCT (app_id, platform_id, platform_user_id))::bigint AS unique_users
FROM calendar_operation
WHERE workspace_id = $1 AND calendar_id = $2;

-- name: AdminListDailyStats :many
SELECT *
FROM calendar_stats_daily
WHERE workspace_id = $1
  AND calendar_id = $2
  AND stats_date >= $3
  AND stats_date <= $4
ORDER BY stats_date;

-- name: RefreshDailyStats :exec
INSERT INTO calendar_stats_daily (
    workspace_id, calendar_id, stats_date,
    operation_count, grant_count, unique_users
)
SELECT
    o.workspace_id,
    o.calendar_id,
    o.occurred_at::date,
    COUNT(*)::bigint,
    COUNT(*) FILTER (WHERE o.granted)::bigint,
    COUNT(DISTINCT (o.app_id, o.platform_id, o.platform_user_id))::bigint
FROM calendar_operation o
WHERE o.workspace_id = sqlc.arg(refresh_workspace_id)
  AND o.occurred_at >= $1 AND o.occurred_at < $2
GROUP BY o.workspace_id, o.calendar_id, o.occurred_at::date
ON CONFLICT (workspace_id, calendar_id, stats_date) DO UPDATE SET
    operation_count = EXCLUDED.operation_count,
    grant_count = EXCLUDED.grant_count,
    unique_users = EXCLUDED.unique_users,
    updated_at = now();

-- name: ListImportCalendarTypes :many
SELECT type, id
FROM calendar_definition
WHERE workspace_id = $1;

-- name: ListImportStepIDs :many
SELECT calendar_id, position, id
FROM calendar_step
WHERE workspace_id = $1;
