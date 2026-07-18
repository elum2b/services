-- name: AdminUpsertGroup :exec
INSERT INTO task_group (workspace_id, key, position, is_active)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, key) DO UPDATE SET
    position = EXCLUDED.position,
    is_active = EXCLUDED.is_active,
    deleted_at = NULL,
    updated_at = now();

-- name: AdminUpsertGroupLocalization :exec
INSERT INTO task_group_localization (workspace_id, group_key, locale, title, description)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, group_key, locale) DO UPDATE SET
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    updated_at = now();

-- name: AdminUpsertSequence :exec
INSERT INTO task_sequence (workspace_id, key, position, is_active)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, key) DO UPDATE SET
    position = EXCLUDED.position,
    is_active = EXCLUDED.is_active,
    deleted_at = NULL,
    updated_at = now();

-- name: AdminCreateTask :one
INSERT INTO task_definition (
    workspace_id, key, group_key, sequence_key, sequence_position, task_kind,
    action_key, action_kind, claim_mode, start_mode, target_count, reset_unit,
    reset_every, position, payload, target, integration_kind, integration_provider,
    integration_payload, image_url, is_visible, is_active,
    start_at, end_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)
RETURNING id;

-- name: AdminUpdateTask :execrows
UPDATE task_definition
SET group_key = $1, sequence_key = $2, sequence_position = $3, task_kind = $4, action_key = $5,
    action_kind = $6, claim_mode = $7, start_mode = $8, target_count = $9, reset_unit = $10,
    reset_every = $11, position = $12, payload = $13, target = $14, integration_kind = $15,
    integration_provider = $16, integration_payload = $17, image_url = $18,
    is_visible = $19, is_active = $20, start_at = $21, end_at = $22
WHERE workspace_id = $23 AND id = $24 AND deleted_at IS NULL;

-- name: AdminDeleteTask :execrows
UPDATE task_definition
SET deleted_at = now(), is_active = false, is_visible = false
WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: AdminGetTask :one
SELECT id, workspace_id, key, group_key, sequence_key, sequence_position,
       task_kind, action_key, action_kind, claim_mode, start_mode, target_count, reset_unit,
       reset_every, position, payload, target, integration_kind, integration_provider,
       integration_payload, image_url, is_visible, is_active,
       start_at, end_at, deleted_at, branch_sort_key, created_at, updated_at
FROM task_definition
WHERE workspace_id = $1 AND id = $2
LIMIT 1;

-- name: AdminGetTaskByKey :one
SELECT id, workspace_id, key, group_key, sequence_key, sequence_position,
       task_kind, action_key, action_kind, claim_mode, start_mode, target_count, reset_unit,
       reset_every, position, payload, target, integration_kind, integration_provider,
       integration_payload, image_url, is_visible, is_active,
       start_at, end_at, deleted_at, branch_sort_key, created_at, updated_at
FROM task_definition
WHERE workspace_id = $1 AND key = $2 AND deleted_at IS NULL
LIMIT 1;

-- name: AdminListGroups :many
SELECT workspace_id, key, position, is_active, deleted_at, created_at, updated_at
FROM task_group
WHERE workspace_id = $1 AND deleted_at IS NULL
ORDER BY position, key;

-- name: AdminListGroupLocalizations :many
SELECT workspace_id, group_key, locale, title, description, created_at, updated_at
FROM task_group_localization
WHERE workspace_id = $1
ORDER BY group_key, locale;

-- name: AdminListSequences :many
SELECT workspace_id, key, position, is_active, deleted_at, created_at, updated_at
FROM task_sequence
WHERE workspace_id = $1 AND deleted_at IS NULL
ORDER BY position, key;

-- name: AdminListTaskLocalizations :many
SELECT workspace_id, task_id, locale, title, description, created_at, updated_at
FROM task_localization
WHERE workspace_id = $1
ORDER BY task_id, locale;

-- name: AdminListAllRewards :many
SELECT id, workspace_id, task_id, reward_key, reward_type, quantity, scale, duration_unit, position, created_at, updated_at
FROM task_reward
WHERE workspace_id = $1
ORDER BY task_id, position, id;

-- name: AdminListPartnerRewardRules :many
SELECT workspace_id, provider, group_key, external_type, reward_key,
       reward_type, quantity, scale, duration_unit, position, is_enabled, created_at, updated_at
FROM task_partner_reward_rule
WHERE workspace_id = $1
ORDER BY group_key, provider, external_type, position, reward_key;

-- name: AdminListTasks :many
SELECT id, workspace_id, key, group_key, sequence_key, sequence_position,
       task_kind, action_key, action_kind, claim_mode, start_mode, target_count, reset_unit,
       reset_every, position, payload, target, integration_kind, integration_provider,
       integration_payload, image_url, is_visible, is_active,
       start_at, end_at, deleted_at, branch_sort_key, created_at, updated_at
FROM task_definition
WHERE workspace_id = $1 AND deleted_at IS NULL
ORDER BY position, id
LIMIT $2 OFFSET $3;

-- name: AdminListTasksByGroup :many
SELECT id, workspace_id, key, group_key, sequence_key, sequence_position,
       task_kind, action_key, action_kind, claim_mode, start_mode, target_count, reset_unit,
       reset_every, position, payload, target, integration_kind, integration_provider,
       integration_payload, image_url, is_visible, is_active,
       start_at, end_at, deleted_at, branch_sort_key, created_at, updated_at
FROM task_definition
WHERE workspace_id = $1 AND group_key = $2 AND deleted_at IS NULL
ORDER BY position, id
LIMIT $3 OFFSET $4;

-- name: ExportListTasks :many
SELECT id, workspace_id, key, group_key, sequence_key, sequence_position,
       task_kind, action_key, action_kind, claim_mode, start_mode, target_count, reset_unit,
       reset_every, position, payload, target, integration_kind, integration_provider,
       integration_payload, image_url, is_visible, is_active,
       start_at, end_at, deleted_at, branch_sort_key, created_at, updated_at
FROM task_definition
WHERE workspace_id = $1 AND deleted_at IS NULL
ORDER BY group_key, position, id;

-- name: AdminUpsertTaskLocalization :exec
INSERT INTO task_localization (workspace_id, task_id, locale, title, description)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, task_id, locale) DO UPDATE SET
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    updated_at = now();

-- name: AdminUpsertReward :exec
INSERT INTO task_reward (
    workspace_id, task_id, reward_key, reward_type, quantity, scale, duration_unit, position
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (workspace_id, task_id, reward_key) DO UPDATE SET
    reward_type = EXCLUDED.reward_type,
    quantity = EXCLUDED.quantity,
    scale = EXCLUDED.scale,
    duration_unit = EXCLUDED.duration_unit,
    position = EXCLUDED.position,
    updated_at = now();

-- name: AdminDeleteReward :execrows
DELETE FROM task_reward
WHERE workspace_id = $1 AND task_id = $2 AND reward_key = $3;

-- name: AdminUpsertComplexCondition :exec
INSERT INTO task_complex_condition (
    workspace_id, parent_task_id, condition_task_id, required_status, position, is_required
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id, parent_task_id, condition_task_id) DO UPDATE SET
    required_status = EXCLUDED.required_status,
    position = EXCLUDED.position,
    is_required = EXCLUDED.is_required,
    updated_at = now();

-- name: AdminDeleteComplexCondition :execrows
DELETE FROM task_complex_condition
WHERE workspace_id = $1
  AND parent_task_id = $2
  AND condition_task_id = $3;

-- name: AdminListComplexConditions :many
SELECT workspace_id, parent_task_id, condition_task_id, required_status, position, is_required, created_at, updated_at
FROM task_complex_condition
WHERE workspace_id = $1
ORDER BY parent_task_id, position, condition_task_id;

-- name: ListComplexParentIDsForConditionTasks :many
SELECT DISTINCT parent_task_id
FROM task_complex_condition
WHERE workspace_id = $1
  AND condition_task_id = ANY($2::bigint[])
  AND is_required = true
ORDER BY parent_task_id;

-- name: ListComplexConditionProgressForParent :many
SELECT
    parent.id AS parent_id,
    parent.task_kind AS parent_task_kind,
    parent.target_count AS parent_target_count,
    parent.reset_unit AS parent_reset_unit,
    parent.reset_every AS parent_reset_every,
    parent.start_at AS parent_start_at,
    parent.end_at AS parent_end_at,
    c.parent_task_id,
    c.condition_task_id,
    c.required_status,
    c.position,
    c.is_required,
    p.id AS progress_id,
    p.progress,
    p.status,
    p.period_start_at,
    p.period_end_at,
    p.ready_at,
    p.claimed_at,
    p.operation_id,
    COALESCE(p.rewards_snapshot, '[]'::jsonb) AS rewards_snapshot
FROM task_complex_condition c
JOIN task_definition parent
  ON parent.workspace_id = c.workspace_id
 AND parent.id = c.parent_task_id
 AND parent.is_active = true
 AND parent.deleted_at IS NULL
JOIN task_definition t
  ON t.workspace_id = c.workspace_id
 AND t.id = c.condition_task_id
 AND t.is_active = true
 AND t.deleted_at IS NULL
LEFT JOIN task_progress p
  ON p.workspace_id = c.workspace_id
 AND p.task_id = c.condition_task_id
 AND p.app_id = $1
 AND p.platform_id = $2
 AND p.platform_user_id = $3
 AND p.period_start_at <= $4
 AND p.period_end_at > $5
WHERE c.workspace_id = $6
  AND c.parent_task_id = $7
  AND c.is_required = true
ORDER BY c.position, c.condition_task_id;

-- name: ListActiveComplexConditions :many
SELECT c.parent_task_id, c.condition_task_id, c.required_status, c.position, c.is_required
FROM task_complex_condition c
JOIN task_definition parent
  ON parent.workspace_id = c.workspace_id
 AND parent.id = c.parent_task_id
 AND parent.is_active = true
 AND parent.deleted_at IS NULL
JOIN task_definition child
  ON child.workspace_id = c.workspace_id
 AND child.id = c.condition_task_id
 AND child.is_active = true
 AND child.deleted_at IS NULL
WHERE c.workspace_id = $1
  AND c.is_required = true
ORDER BY c.parent_task_id, c.position, c.condition_task_id;

-- name: ListRecordTasks :many
SELECT t.id, t.workspace_id, t.key, t.group_key, t.sequence_key, t.sequence_position,
       t.task_kind, t.action_key, t.action_kind, t.claim_mode, t.start_mode, t.target_count, t.reset_unit,
       t.reset_every, t.payload, t.target, t.branch_sort_key, t.position
FROM task_definition t 
WHERE t.workspace_id = $1
  AND t.action_key = $2
  AND t.sequence_key IS NULL
  AND t.is_active = true
  AND t.deleted_at IS NULL
  AND (t.start_at IS NULL OR t.start_at <= $3)
  AND (t.end_at IS NULL OR t.end_at > $4)
UNION ALL
SELECT t.id, t.workspace_id, t.key, t.group_key, t.sequence_key, t.sequence_position,
       t.task_kind, t.action_key, t.action_kind, t.claim_mode, t.start_mode, t.target_count, t.reset_unit,
       t.reset_every, t.payload, t.target, t.branch_sort_key, t.position
FROM task_sequence_state s
JOIN task_definition t
  ON t.workspace_id = s.workspace_id AND t.id = s.current_task_id
WHERE s.workspace_id = $5
  AND s.app_id = $6
  AND s.platform_id = $7
  AND s.platform_user_id = $8
  AND s.status = 'active'
  AND t.action_key = $9
  AND t.is_active = true
  AND t.deleted_at IS NULL
  AND (t.start_at IS NULL OR t.start_at <= $10)
  AND (t.end_at IS NULL OR t.end_at > $11)
UNION ALL
SELECT t.id, t.workspace_id, t.key, t.group_key, t.sequence_key, t.sequence_position,
       t.task_kind, t.action_key, t.action_kind, t.claim_mode, t.start_mode, t.target_count, t.reset_unit,
       t.reset_every, t.payload, t.target, t.branch_sort_key, t.position
FROM task_definition t 
LEFT JOIN task_sequence_state s
  ON s.workspace_id = t.workspace_id
 AND s.sequence_key = t.sequence_key
 AND s.app_id = $12
 AND s.platform_id = $13
 AND s.platform_user_id = $14
WHERE t.workspace_id = $15
  AND t.action_key = $16
  AND t.sequence_key IS NOT NULL
  AND t.sequence_position = 1
  AND s.sequence_key IS NULL
  AND t.is_active = true
  AND t.deleted_at IS NULL
  AND (t.start_at IS NULL OR t.start_at <= $17)
  AND (t.end_at IS NULL OR t.end_at > $18)
ORDER BY branch_sort_key, sequence_position, position, id;

-- name: ListRecordCatalog :many
SELECT t.id, t.workspace_id, t.key, t.group_key, t.sequence_key, t.sequence_position,
       t.task_kind, t.action_key, t.action_kind, t.claim_mode, t.start_mode, t.target_count, t.reset_unit,
       t.reset_every, t.payload, t.target, t.position, t.start_at, t.end_at,
       COALESCE(
           (
               SELECT jsonb_agg(
                   jsonb_strip_nulls(
                       jsonb_build_object(
                           'key', reward.reward_key,
                           'type', reward.reward_type,
                           'quantity', reward.quantity,
                           'scale', reward.scale,
                           'unit', reward.duration_unit
                       )
                   )
                   ORDER BY reward.position, reward.id
               )
               FROM task_reward AS reward
               WHERE reward.workspace_id = t.workspace_id
                 AND reward.task_id = t.id
           ),
           '[]'::jsonb
       )::text AS rewards
FROM task_definition t 
WHERE t.workspace_id = $1
  AND t.action_key = $2
  AND t.action_kind IN ('app_action', 'amount_action', 'advertisement_view')
  AND t.is_active = true
  AND t.deleted_at IS NULL
ORDER BY t.branch_sort_key, t.sequence_position, t.position, t.id;

-- name: GetNextSequenceTaskID :one
SELECT id
FROM task_definition
WHERE workspace_id = $1
  AND sequence_key = $2
  AND sequence_position > $3
  AND deleted_at IS NULL
ORDER BY sequence_position, id
LIMIT 1;

-- name: ListSequenceStatesForUser :many
SELECT sequence_key, current_task_id
FROM task_sequence_state
WHERE workspace_id = $1
  AND app_id = $2
  AND platform_id = $3
  AND platform_user_id = $4
  AND status = 'active';

-- name: LockTaskUser :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(
        'tasks:user:' || sqlc.arg(workspace_id)::text || ':' ||
        sqlc.arg(app_id)::bigint::text || ':' ||
        sqlc.arg(platform_id)::bigint::text || ':' ||
        sqlc.arg(platform_user_id)::text,
        0
    )
);

-- name: GetSequenceState :one
SELECT current_task_id, status
FROM task_sequence_state
WHERE workspace_id = $1
  AND sequence_key = $2
  AND app_id = $3
  AND platform_id = $4
  AND platform_user_id = $5;

-- name: UpsertSequenceState :exec
INSERT INTO task_sequence_state (
    workspace_id, sequence_key, app_id, platform_id, platform_user_id,
    current_task_id, status
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (workspace_id, sequence_key, app_id, platform_id, platform_user_id) DO UPDATE SET
    current_task_id = EXCLUDED.current_task_id,
    status = EXCLUDED.status,
    updated_at = now();

-- name: ListCurrentProgressForTasksForUpdate :many
SELECT id, workspace_id, task_id, app_id, platform_id, platform_user_id,
       period_start_at, period_end_at, progress, status, ready_at, claimed_at,
       operation_id, COALESCE(rewards_snapshot, '[]'::jsonb) AS rewards_snapshot, created_at, updated_at
FROM task_progress
WHERE workspace_id = $1
  AND app_id = $2
  AND platform_id = $3
  AND platform_user_id = $4
  AND period_start_at <= $5
  AND period_end_at > $6
  AND task_id = ANY(sqlc.arg(task_ids)::bigint[])
ORDER BY task_id, id
FOR UPDATE;

-- name: GetCurrentProgressForUpdate :one
SELECT id, workspace_id, task_id, app_id, platform_id, platform_user_id,
       period_start_at, period_end_at, progress, status, ready_at, claimed_at,
       operation_id, COALESCE(rewards_snapshot, '[]'::jsonb) AS rewards_snapshot, created_at, updated_at
FROM task_progress
WHERE workspace_id = $1
  AND task_id = $2
  AND app_id = $3
  AND platform_id = $4
  AND platform_user_id = $5
  AND period_start_at <= $6
  AND period_end_at > $7
LIMIT 1
FOR UPDATE;

-- name: ListCurrentProgressForUser :many
SELECT id, workspace_id, task_id, app_id, platform_id, platform_user_id,
       period_start_at, period_end_at, progress, status, ready_at, claimed_at,
       operation_id, COALESCE(rewards_snapshot, '[]'::jsonb) AS rewards_snapshot, created_at, updated_at
FROM task_progress
WHERE workspace_id = $1
  AND app_id = $2
  AND platform_id = $3
  AND platform_user_id = $4
  AND period_start_at <= $5
  AND period_end_at > $6;

-- name: EnsureProgress :one
INSERT INTO task_progress (
    workspace_id, task_id, app_id, platform_id, platform_user_id,
    period_start_at, period_end_at, rewards_snapshot
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (workspace_id, task_id, app_id, platform_id, platform_user_id, period_start_at) DO UPDATE SET
    period_end_at = EXCLUDED.period_end_at,
    updated_at = now()
RETURNING id;

-- name: GetStartTaskByID :one
SELECT id, workspace_id, key, group_key, sequence_key, sequence_position,
       task_kind, action_key, action_kind, claim_mode, start_mode, target_count,
       reset_unit, reset_every, payload, target, integration_kind, integration_provider,
       integration_payload, image_url, start_at, end_at
FROM task_definition
WHERE workspace_id = $1 AND id = $2 AND is_active = true AND deleted_at IS NULL
LIMIT 1;

-- name: GetStartTaskByKey :one
SELECT id, workspace_id, key, group_key, sequence_key, sequence_position,
       task_kind, action_key, action_kind, claim_mode, start_mode, target_count,
       reset_unit, reset_every, payload, target, integration_kind, integration_provider,
       integration_payload, image_url, start_at, end_at
FROM task_definition
WHERE workspace_id = $1 AND key = $2 AND is_active = true AND deleted_at IS NULL
LIMIT 1;

-- name: UpsertProgress :execrows
INSERT INTO task_progress (
    workspace_id, task_id, app_id, platform_id, platform_user_id,
    period_start_at, period_end_at, progress, status, ready_at, rewards_snapshot
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (workspace_id, task_id, app_id, platform_id, platform_user_id, period_start_at) DO UPDATE SET
    period_end_at = EXCLUDED.period_end_at,
    progress = EXCLUDED.progress,
    status = EXCLUDED.status,
    ready_at = EXCLUDED.ready_at,
    rewards_snapshot = COALESCE(task_progress.rewards_snapshot, EXCLUDED.rewards_snapshot),
    updated_at = now();

-- name: UpdateProgress :execrows
UPDATE task_progress
SET progress = $1, status = $2, ready_at = $3, claimed_at = $4,
    operation_id = $5, rewards_snapshot = $6
WHERE id = $7;

-- name: ClaimProgressWithOperation :execrows
WITH reserved_operation AS (
    INSERT INTO task_reward_operation (
        workspace_id,
        operation_id,
        source_kind,
        source_id
    ) VALUES (
        sqlc.arg(workspace_id)::varchar,
        sqlc.arg(operation_id)::varchar,
        'task_progress',
        sqlc.arg(progress_id)::bigint
    )
    ON CONFLICT (workspace_id, operation_id) DO NOTHING
    RETURNING 1
)
UPDATE task_progress
SET progress = sqlc.arg(progress)::bigint,
    status = 'claimed',
    ready_at = sqlc.narg(ready_at)::timestamptz,
    claimed_at = sqlc.arg(claimed_at)::timestamptz,
    operation_id = sqlc.arg(operation_id)::varchar,
    rewards_snapshot = sqlc.arg(rewards_snapshot)::jsonb,
    updated_at = now()
WHERE id = sqlc.arg(progress_id)::bigint
  AND workspace_id = sqlc.arg(workspace_id)::varchar
  AND status IN ('open', 'ready')
  AND EXISTS (SELECT 1 FROM reserved_operation);

-- name: InsertProgressEvent :execrows
INSERT INTO task_progress_event (
    workspace_id, app_id, platform_id, platform_user_id,
    source, external_event_key, action_key, amount, payload
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, sqlc.narg(payload))
ON CONFLICT (workspace_id, source, external_event_key, app_id, platform_id, platform_user_id) DO NOTHING;

-- name: CountProgressEventsByExternalKey :one
SELECT COUNT(*)
FROM task_progress_event
WHERE workspace_id = $1
  AND app_id = $2
  AND platform_id = $3
  AND platform_user_id = $4
  AND source = $5
  AND external_event_key = $6;

-- name: ListRewards :many
SELECT id, workspace_id, task_id, reward_key, reward_type, quantity, scale, duration_unit, position, created_at, updated_at
FROM task_reward
WHERE workspace_id = $1 AND task_id = $2
ORDER BY position, id;

-- name: ListRewardsCatalog :many
SELECT reward_key, reward_type, quantity, scale, duration_unit
FROM task_reward
WHERE workspace_id = $1 AND task_id = $2
ORDER BY position, id;

-- name: GetClaimCatalogByID :many
SELECT t.id, t.workspace_id, t.key, t.group_key, t.sequence_key, t.sequence_position,
       t.task_kind, t.action_key, t.action_kind, t.claim_mode, t.start_mode, t.target_count,
       t.payload, t.target, t.integration_kind, t.integration_provider, t.integration_payload, t.image_url,
       r.id AS reward_id, r.reward_key, r.reward_type, r.quantity AS reward_quantity, r.scale AS reward_scale, r.duration_unit
FROM task_definition t
LEFT JOIN task_reward r ON r.workspace_id = t.workspace_id AND r.task_id = t.id
WHERE t.workspace_id = $1 AND t.id = $2
ORDER BY r.position, r.id;

-- name: GetClaimCatalogByKey :many
SELECT t.id, t.workspace_id, t.key, t.group_key, t.sequence_key, t.sequence_position,
       t.task_kind, t.action_key, t.action_kind, t.claim_mode, t.start_mode, t.target_count,
       t.payload, t.target, t.integration_kind, t.integration_provider, t.integration_payload, t.image_url,
       r.id AS reward_id, r.reward_key, r.reward_type, r.quantity AS reward_quantity, r.scale AS reward_scale, r.duration_unit
FROM task_definition t
LEFT JOIN task_reward r ON r.workspace_id = t.workspace_id AND r.task_id = t.id
WHERE t.workspace_id = $1 AND t.key = $2
ORDER BY r.position, r.id;

-- name: GetIntegrationCheckTaskByID :one
SELECT t.id, t.workspace_id, t.key, t.group_key, t.sequence_key, t.sequence_position,
       t.task_kind, t.action_key, t.action_kind, t.claim_mode, t.start_mode, t.target_count,
       t.reset_unit, t.reset_every, t.payload, t.target, t.integration_kind, t.integration_provider,
       t.integration_payload, t.image_url, t.start_at, t.end_at
FROM task_definition t
WHERE t.workspace_id = $1 AND t.id = $2 AND t.is_active = true AND t.deleted_at IS NULL;

-- name: GetIntegrationCheckTaskByKey :one
SELECT t.id, t.workspace_id, t.key, t.group_key, t.sequence_key, t.sequence_position,
       t.task_kind, t.action_key, t.action_kind, t.claim_mode, t.start_mode, t.target_count,
       t.reset_unit, t.reset_every, t.payload, t.target, t.integration_kind, t.integration_provider,
       t.integration_payload, t.image_url, t.start_at, t.end_at
FROM task_definition t
WHERE t.workspace_id = $1 AND t.key = $2 AND t.is_active = true AND t.deleted_at IS NULL;

-- name: GetClaimBundleByIDForUpdate :many
SELECT t.id, t.workspace_id, t.key, t.group_key, t.sequence_key, t.sequence_position,
       t.task_kind, t.action_key, t.action_kind, t.claim_mode, t.start_mode, t.target_count,
       t.payload, t.target, t.integration_kind, t.integration_provider, t.integration_payload, t.image_url,
       p.id AS progress_id, p.progress, p.status, p.period_start_at, p.period_end_at,
    p.ready_at, p.claimed_at, p.operation_id, COALESCE(p.rewards_snapshot, '[]'::jsonb) AS rewards_snapshot,
       r.id AS reward_id, r.reward_key, r.reward_type,
       r.quantity AS reward_quantity, r.scale AS reward_scale, r.duration_unit, r.position AS reward_position
FROM task_definition t
LEFT JOIN task_progress p
  ON p.workspace_id = t.workspace_id AND p.task_id = t.id
 AND p.app_id = $1 AND p.platform_id = $2 AND p.platform_user_id = $3
 AND p.period_start_at <= $4 AND p.period_end_at > $5
LEFT JOIN task_reward r ON r.workspace_id = t.workspace_id AND r.task_id = t.id
WHERE t.workspace_id = $6 AND t.id = $7
ORDER BY r.position, r.id
FOR UPDATE;

-- name: GetClaimBundleByKeyForUpdate :many
SELECT t.id, t.workspace_id, t.key, t.group_key, t.sequence_key, t.sequence_position,
       t.task_kind, t.action_key, t.action_kind, t.claim_mode, t.start_mode, t.target_count,
       t.payload, t.target, t.integration_kind, t.integration_provider, t.integration_payload, t.image_url,
       p.id AS progress_id, p.progress, p.status, p.period_start_at, p.period_end_at,
    p.ready_at, p.claimed_at, p.operation_id, COALESCE(p.rewards_snapshot, '[]'::jsonb) AS rewards_snapshot,
       r.id AS reward_id, r.reward_key, r.reward_type,
       r.quantity AS reward_quantity, r.scale AS reward_scale, r.duration_unit, r.position AS reward_position
FROM task_definition t
LEFT JOIN task_progress p
  ON p.workspace_id = t.workspace_id AND p.task_id = t.id
 AND p.app_id = $1 AND p.platform_id = $2 AND p.platform_user_id = $3
 AND p.period_start_at <= $4 AND p.period_end_at > $5
LEFT JOIN task_reward r ON r.workspace_id = t.workspace_id AND r.task_id = t.id
WHERE t.workspace_id = $6 AND t.key = $7
ORDER BY r.position, r.id
FOR UPDATE;

-- name: ListActiveTaskBundles :many
SELECT t.id, t.key, t.group_key,
       t.task_kind, t.action_key, t.action_kind, t.claim_mode, t.start_mode, t.target_count,
       t.payload, t.target, t.image_url, t.start_at, t.end_at,
       gl.locale AS group_locale, gl.title AS group_title, gl.description AS group_description,
       l.locale, l.title, l.description,
       r.id AS reward_id, r.reward_key, r.reward_type,
       r.quantity AS reward_quantity, r.scale AS reward_scale, r.duration_unit
FROM task_definition t 
JOIN task_group g ON g.workspace_id = t.workspace_id AND g.key = t.group_key
LEFT JOIN task_group_localization gl ON gl.workspace_id = t.workspace_id AND gl.group_key = t.group_key AND gl.locale = $1
LEFT JOIN task_localization l ON l.workspace_id = t.workspace_id AND l.task_id = t.id AND l.locale = $2
LEFT JOIN task_reward r ON r.workspace_id = t.workspace_id AND r.task_id = t.id
WHERE t.workspace_id = $3 AND t.is_visible = true AND t.is_active = true
  AND ($4 = '' OR t.group_key = $5)
  AND g.is_active = true AND g.deleted_at IS NULL
  AND t.deleted_at IS NULL
ORDER BY t.position, t.id, r.position, r.id;

-- name: AdminGetTaskStats :one
SELECT
    definitions.tasks_total,
    definitions.active_tasks,
    definitions.visible_tasks,
    progress.progress_total,
    progress.open_progress,
    progress.ready_progress,
    progress.claimed_progress,
    events.progress_created,
    events.progress_amount,
    events.ready_count,
    events.claimed_count,
    events.manual_claimed_count,
    events.auto_claimed_count,
    events.unique_participants,
    events.unique_claimers
FROM (
    SELECT
        COUNT(*) AS tasks_total,
        COUNT(*) FILTER (
            WHERE is_active = true
            AND deleted_at IS NULL
            AND (start_at IS NULL OR start_at <= now())
            AND (end_at IS NULL OR end_at > now())
        ) AS active_tasks,
        COUNT(*) FILTER (
            WHERE is_visible = true
            AND is_active = true
            AND deleted_at IS NULL
            AND (start_at IS NULL OR start_at <= now())
            AND (end_at IS NULL OR end_at > now())
        ) AS visible_tasks
    FROM task_definition stats_definitions
    WHERE stats_definitions.workspace_id = $1
) definitions
CROSS JOIN (
    SELECT
        COUNT(*) AS progress_total,
        COUNT(*) FILTER (WHERE status = 'open') AS open_progress,
        COUNT(*) FILTER (WHERE status = 'ready') AS ready_progress,
        COUNT(*) FILTER (WHERE status = 'claimed') AS claimed_progress
    FROM task_progress stats_progress
    WHERE stats_progress.workspace_id = $2
) progress
CROSS JOIN (
    SELECT
        COUNT(*) FILTER (WHERE event_type = 'progress_created') AS progress_created,
        COALESCE(SUM(amount) FILTER (WHERE event_type = 'progress_added'), 0)::bigint AS progress_amount,
        COUNT(*) FILTER (WHERE event_type = 'ready') AS ready_count,
        COUNT(*) FILTER (WHERE event_type = 'claimed') AS claimed_count,
        COUNT(*) FILTER (WHERE event_type = 'claimed' AND claim_mode = 'manual') AS manual_claimed_count,
        COUNT(*) FILTER (WHERE event_type = 'claimed' AND claim_mode = 'auto') AS auto_claimed_count,
        COUNT(DISTINCT (app_id, platform_id, platform_user_id)) AS unique_participants,
        COUNT(DISTINCT (app_id, platform_id, platform_user_id)) FILTER (WHERE event_type = 'claimed') AS unique_claimers
    FROM task_stats_event stats_events
    WHERE stats_events.workspace_id = $3
) events;

-- name: AdminGetSingleTaskStats :one
SELECT
    definition.id AS task_id,
    progress.progress_total,
    progress.open_progress,
    progress.ready_progress,
    progress.claimed_progress,
    events.progress_created,
    events.progress_amount,
    events.ready_count,
    events.claimed_count,
    events.manual_claimed_count,
    events.auto_claimed_count,
    events.unique_participants,
    events.unique_claimers
FROM task_definition definition
CROSS JOIN (
    SELECT
        COUNT(*) AS progress_total,
        COUNT(*) FILTER (WHERE status = 'open') AS open_progress,
        COUNT(*) FILTER (WHERE status = 'ready') AS ready_progress,
        COUNT(*) FILTER (WHERE status = 'claimed') AS claimed_progress
    FROM task_progress single_progress
    WHERE single_progress.workspace_id = $1 AND single_progress.task_id = $2
) progress
CROSS JOIN (
    SELECT
        COUNT(*) FILTER (WHERE event_type = 'progress_created') AS progress_created,
        COALESCE(SUM(amount) FILTER (WHERE event_type = 'progress_added'), 0)::bigint AS progress_amount,
        COUNT(*) FILTER (WHERE event_type = 'ready') AS ready_count,
        COUNT(*) FILTER (WHERE event_type = 'claimed') AS claimed_count,
        COUNT(*) FILTER (WHERE event_type = 'claimed' AND claim_mode = 'manual') AS manual_claimed_count,
        COUNT(*) FILTER (WHERE event_type = 'claimed' AND claim_mode = 'auto') AS auto_claimed_count,
        COUNT(DISTINCT (app_id, platform_id, platform_user_id)) AS unique_participants,
        COUNT(DISTINCT (app_id, platform_id, platform_user_id)) FILTER (WHERE event_type = 'claimed') AS unique_claimers
    FROM task_stats_event single_events
    WHERE single_events.workspace_id = $3 AND single_events.task_id = $4
) events
WHERE definition.workspace_id = $5 AND definition.id = $6
LIMIT 1;

-- name: AdminListTaskDailyStats :many
SELECT
    workspace_id,
    task_id,
    stats_date,
    progress_created,
    progress_amount,
    ready_count,
    claimed_count,
    manual_claimed_count,
    auto_claimed_count,
    unique_participants,
    unique_claimers,
    updated_at
FROM task_stats_daily stored_stats
WHERE stored_stats.workspace_id = $1
  AND stored_stats.task_id = $2
  AND stored_stats.stats_date >= $3
  AND stored_stats.stats_date <= $4
  AND stored_stats.stats_date < CURRENT_DATE
UNION ALL
SELECT
    $5 AS workspace_id,
    $6 AS task_id,
    CURRENT_DATE AS stats_date,
    COUNT(*) FILTER (WHERE event_type = 'progress_created') AS progress_created,
    COALESCE(SUM(amount) FILTER (WHERE event_type = 'progress_added'), 0)::bigint AS progress_amount,
    COUNT(*) FILTER (WHERE event_type = 'ready') AS ready_count,
    COUNT(*) FILTER (WHERE event_type = 'claimed') AS claimed_count,
    COUNT(*) FILTER (WHERE event_type = 'claimed' AND claim_mode = 'manual') AS manual_claimed_count,
    COUNT(*) FILTER (WHERE event_type = 'claimed' AND claim_mode = 'auto') AS auto_claimed_count,
    COUNT(DISTINCT (app_id, platform_id, platform_user_id)) AS unique_participants,
    COUNT(DISTINCT (app_id, platform_id, platform_user_id)) FILTER (WHERE event_type = 'claimed') AS unique_claimers,
    now() AS updated_at
FROM task_stats_event current_events
WHERE current_events.workspace_id = $7
  AND current_events.task_id = $8
  AND current_events.occurred_at >= CURRENT_DATE
  AND current_events.occurred_at < CURRENT_DATE + INTERVAL '1 day'
  AND CURRENT_DATE >= $9
  AND CURRENT_DATE <= $10
ORDER BY stats_date;

-- name: AdminListTaskDailyOverview :many
SELECT
    workspace_id,
    stats_date,
    tasks_total,
    active_tasks,
    visible_tasks,
    progress_created,
    progress_amount,
    ready_count,
    claimed_count,
    manual_claimed_count,
    auto_claimed_count,
    unique_participants,
    unique_claimers,
    updated_at
FROM task_stats_daily_overview stored_overview
WHERE stored_overview.workspace_id = $1
  AND stored_overview.stats_date >= $2
  AND stored_overview.stats_date <= $3
  AND stored_overview.stats_date < CURRENT_DATE
UNION ALL
SELECT
    $4 AS workspace_id,
    CURRENT_DATE AS stats_date,
    definitions.tasks_total,
    definitions.active_tasks,
    definitions.visible_tasks,
    events.progress_created,
    events.progress_amount,
    events.ready_count,
    events.claimed_count,
    events.manual_claimed_count,
    events.auto_claimed_count,
    events.unique_participants,
    events.unique_claimers,
    now() AS updated_at
FROM (
    SELECT
        COUNT(*) AS tasks_total,
        COUNT(*) FILTER (
            WHERE is_active = true
            AND deleted_at IS NULL
            AND (start_at IS NULL OR start_at <= now())
            AND (end_at IS NULL OR end_at > now())
        ) AS active_tasks,
        COUNT(*) FILTER (
            WHERE is_visible = true
            AND is_active = true
            AND deleted_at IS NULL
            AND (start_at IS NULL OR start_at <= now())
            AND (end_at IS NULL OR end_at > now())
        ) AS visible_tasks
    FROM task_definition current_definitions
    WHERE current_definitions.workspace_id = $5
) definitions
CROSS JOIN (
    SELECT
        COUNT(*) FILTER (WHERE event_type = 'progress_created') AS progress_created,
        COALESCE(SUM(amount) FILTER (WHERE event_type = 'progress_added'), 0)::bigint AS progress_amount,
        COUNT(*) FILTER (WHERE event_type = 'ready') AS ready_count,
        COUNT(*) FILTER (WHERE event_type = 'claimed') AS claimed_count,
        COUNT(*) FILTER (WHERE event_type = 'claimed' AND claim_mode = 'manual') AS manual_claimed_count,
        COUNT(*) FILTER (WHERE event_type = 'claimed' AND claim_mode = 'auto') AS auto_claimed_count,
        COUNT(DISTINCT (app_id, platform_id, platform_user_id)) AS unique_participants,
        COUNT(DISTINCT (app_id, platform_id, platform_user_id)) FILTER (WHERE event_type = 'claimed') AS unique_claimers
    FROM task_stats_event current_overview_events
    WHERE current_overview_events.workspace_id = $6
      AND current_overview_events.occurred_at >= CURRENT_DATE
      AND current_overview_events.occurred_at < CURRENT_DATE + INTERVAL '1 day'
) events
WHERE CURRENT_DATE >= $7
  AND CURRENT_DATE <= $8
ORDER BY stats_date;

-- name: RefreshTaskDailyStats :exec
INSERT INTO task_stats_daily (
    workspace_id,
    task_id,
    stats_date,
    progress_created,
    progress_amount,
    ready_count,
    claimed_count,
    manual_claimed_count,
    auto_claimed_count,
    unique_participants,
    unique_claimers
)
SELECT
    e.workspace_id,
    e.task_id,
    e.occurred_at::date,
    COUNT(*) FILTER (WHERE e.event_type = 'progress_created'),
    COALESCE(SUM(e.amount) FILTER (WHERE e.event_type = 'progress_added'), 0)::bigint,
    COUNT(*) FILTER (WHERE e.event_type = 'ready'),
    COUNT(*) FILTER (WHERE e.event_type = 'claimed'),
    COUNT(*) FILTER (WHERE e.event_type = 'claimed' AND e.claim_mode = 'manual'),
    COUNT(*) FILTER (WHERE e.event_type = 'claimed' AND e.claim_mode = 'auto'),
    COUNT(DISTINCT (e.app_id, e.platform_id, e.platform_user_id)),
    COUNT(DISTINCT (e.app_id, e.platform_id, e.platform_user_id)) FILTER (WHERE e.event_type = 'claimed')
FROM task_stats_event e
WHERE e.workspace_id = sqlc.arg(refresh_workspace_id)
  AND e.occurred_at >= $1 AND e.occurred_at < $2
GROUP BY e.workspace_id, e.task_id, e.occurred_at::date
ON CONFLICT (workspace_id, task_id, stats_date) DO UPDATE SET
    progress_created = EXCLUDED.progress_created,
    progress_amount = EXCLUDED.progress_amount,
    ready_count = EXCLUDED.ready_count,
    claimed_count = EXCLUDED.claimed_count,
    manual_claimed_count = EXCLUDED.manual_claimed_count,
    auto_claimed_count = EXCLUDED.auto_claimed_count,
    unique_participants = EXCLUDED.unique_participants,
    unique_claimers = EXCLUDED.unique_claimers,
    updated_at = now();

-- name: RefreshTaskDailyOverview :exec
INSERT INTO task_stats_daily_overview (
    workspace_id,
    stats_date,
    tasks_total,
    active_tasks,
    visible_tasks,
    progress_created,
    progress_amount,
    ready_count,
    claimed_count,
    manual_claimed_count,
    auto_claimed_count,
    unique_participants,
    unique_claimers
)
SELECT
    event_rows.workspace_id,
    event_rows.stats_date,
    definitions.tasks_total,
    definitions.active_tasks,
    definitions.visible_tasks,
    event_rows.progress_created,
    event_rows.progress_amount,
    event_rows.ready_count,
    event_rows.claimed_count,
    event_rows.manual_claimed_count,
    event_rows.auto_claimed_count,
    event_rows.unique_participants,
    event_rows.unique_claimers
FROM (
    SELECT
        workspace_id,
        occurred_at::date AS stats_date,
        COUNT(*) FILTER (WHERE event_type = 'progress_created') AS progress_created,
        COALESCE(SUM(amount) FILTER (WHERE event_type = 'progress_added'), 0)::bigint AS progress_amount,
        COUNT(*) FILTER (WHERE event_type = 'ready') AS ready_count,
        COUNT(*) FILTER (WHERE event_type = 'claimed') AS claimed_count,
        COUNT(*) FILTER (WHERE event_type = 'claimed' AND claim_mode = 'manual') AS manual_claimed_count,
        COUNT(*) FILTER (WHERE event_type = 'claimed' AND claim_mode = 'auto') AS auto_claimed_count,
        COUNT(DISTINCT (app_id, platform_id, platform_user_id)) AS unique_participants,
        COUNT(DISTINCT (app_id, platform_id, platform_user_id)) FILTER (WHERE event_type = 'claimed') AS unique_claimers
    FROM task_stats_event refresh_events
    WHERE refresh_events.workspace_id = sqlc.arg(refresh_workspace_id)
      AND refresh_events.occurred_at >= $1 AND refresh_events.occurred_at < $2
    GROUP BY refresh_events.workspace_id, refresh_events.occurred_at::date
) event_rows
JOIN (
    SELECT
        workspace_id,
        COUNT(*) AS tasks_total,
        COUNT(*) FILTER (
            WHERE is_active = true
            AND deleted_at IS NULL
            AND (start_at IS NULL OR start_at <= now())
            AND (end_at IS NULL OR end_at > now())
        ) AS active_tasks,
        COUNT(*) FILTER (
            WHERE is_visible = true
            AND is_active = true
            AND deleted_at IS NULL
            AND (start_at IS NULL OR start_at <= now())
            AND (end_at IS NULL OR end_at > now())
        ) AS visible_tasks
    FROM task_definition
    WHERE workspace_id = sqlc.arg(refresh_workspace_id)
    GROUP BY workspace_id
) definitions ON definitions.workspace_id = event_rows.workspace_id
ON CONFLICT (workspace_id, stats_date) DO UPDATE SET
    tasks_total = EXCLUDED.tasks_total,
    active_tasks = EXCLUDED.active_tasks,
    visible_tasks = EXCLUDED.visible_tasks,
    progress_created = EXCLUDED.progress_created,
    progress_amount = EXCLUDED.progress_amount,
    ready_count = EXCLUDED.ready_count,
    claimed_count = EXCLUDED.claimed_count,
    manual_claimed_count = EXCLUDED.manual_claimed_count,
    auto_claimed_count = EXCLUDED.auto_claimed_count,
    unique_participants = EXCLUDED.unique_participants,
    unique_claimers = EXCLUDED.unique_claimers,
    updated_at = now();

-- name: AdminUpsertPartnerConfig :exec
INSERT INTO task_partner_config (
    workspace_id, provider, group_key, platform, is_enabled, secret, webhook_secret, target, settings
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (workspace_id, provider, group_key, platform) DO UPDATE SET
    is_enabled = EXCLUDED.is_enabled,
    secret = EXCLUDED.secret,
    webhook_secret = EXCLUDED.webhook_secret,
    target = EXCLUDED.target,
    settings = EXCLUDED.settings,
    updated_at = now();

-- name: AdminGetPartnerConfig :one
SELECT workspace_id, provider, group_key, platform, is_enabled, secret, webhook_secret, target, settings, created_at, updated_at
FROM task_partner_config
WHERE workspace_id = $1 AND provider = $2 AND group_key = $3 AND platform = $4
LIMIT 1;

-- name: GetPartnerConfigByWebhookSecret :one
SELECT workspace_id, provider, group_key, platform, is_enabled, secret, webhook_secret, target, settings, created_at, updated_at
FROM task_partner_config
WHERE workspace_id = $1 AND webhook_secret = $2
LIMIT 1;

-- name: AdminListPartnerConfigs :many
SELECT workspace_id, provider, group_key, platform, is_enabled, secret, webhook_secret, target, settings, created_at, updated_at
FROM task_partner_config
WHERE workspace_id = $1
ORDER BY provider, group_key, platform;

-- name: ListAllPartnerConfigs :many
SELECT workspace_id, provider, group_key, platform, is_enabled, secret, webhook_secret, target, settings, created_at, updated_at
FROM task_partner_config
ORDER BY workspace_id, provider, group_key, platform;

-- name: AdminUpsertPartnerScript :exec
INSERT INTO task_partner_script (
    provider, is_enabled, version, source
) VALUES ($1, $2, $3, $4)
ON CONFLICT (provider) DO UPDATE SET
    is_enabled = EXCLUDED.is_enabled,
    version = EXCLUDED.version,
    source = EXCLUDED.source,
    updated_at = now();

-- name: AdminGetPartnerScript :one
SELECT provider, is_enabled, version, source, created_at, updated_at
FROM task_partner_script
WHERE provider = $1
LIMIT 1;

-- name: AdminListPartnerScripts :many
SELECT provider, is_enabled, version, source, created_at, updated_at
FROM task_partner_script
ORDER BY provider;

-- name: GetEnabledPartnerScript :one
SELECT provider, is_enabled, version, source, created_at, updated_at
FROM task_partner_script
WHERE provider = $1 AND is_enabled = true
LIMIT 1;

-- name: AdminUpsertPartnerRewardRule :exec
INSERT INTO task_partner_reward_rule (
    workspace_id, provider, group_key, external_type, reward_key,
    reward_type, quantity, scale, duration_unit, position, is_enabled
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (workspace_id, provider, group_key, external_type, reward_key) DO UPDATE SET
    reward_type = EXCLUDED.reward_type,
    quantity = EXCLUDED.quantity,
    scale = EXCLUDED.scale,
    duration_unit = EXCLUDED.duration_unit,
    position = EXCLUDED.position,
    is_enabled = EXCLUDED.is_enabled,
    updated_at = now();

-- name: AdminDeletePartnerRewardRule :execrows
DELETE FROM task_partner_reward_rule
WHERE workspace_id = $1 AND provider = $2 AND group_key = $3 AND external_type = $4 AND reward_key = $5;

-- name: ListPartnerRewardRules :many
SELECT workspace_id, provider, group_key, external_type, reward_key,
       reward_type, quantity, scale, duration_unit, position, is_enabled, created_at, updated_at
FROM task_partner_reward_rule
WHERE workspace_id = $1
  AND provider = $2
  AND group_key = $3
  AND external_type IN ($4, '*')
  AND is_enabled = true
ORDER BY CASE WHEN external_type = $5 THEN 0 ELSE 1 END, position, reward_key;

-- name: CreatePartnerIssue :one
INSERT INTO task_partner_issue (
    workspace_id, provider, group_key, platform, external_id, external_type, external_click_id, start_mode, issue_key,
    app_id, platform_id, platform_user_id, public_payload, private_payload, rewards_snapshot,
    status, issued_at, started_at, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, 'issued', $16, NULL, $17)
ON CONFLICT (workspace_id, issue_key) DO UPDATE SET
    public_payload = EXCLUDED.public_payload,
    private_payload = EXCLUDED.private_payload,
    expires_at = EXCLUDED.expires_at,
    updated_at = now()
RETURNING id;

-- name: GetPartnerIssueByID :one
SELECT id, workspace_id, provider, group_key, platform, external_id, external_type, external_click_id, start_mode, issue_key,
       app_id, platform_id, platform_user_id, public_payload, private_payload, rewards_snapshot,
       status, issued_at, started_at, completed_at, claimed_at, expires_at, created_at, updated_at
FROM task_partner_issue
WHERE workspace_id = $1 AND id = $2
LIMIT 1;

-- name: GetPartnerIssueByIDForUpdate :one
SELECT id, workspace_id, provider, group_key, platform, external_id, external_type, external_click_id, start_mode, issue_key,
       app_id, platform_id, platform_user_id, public_payload, private_payload, rewards_snapshot,
       status, issued_at, started_at, completed_at, claimed_at, expires_at, created_at, updated_at
FROM task_partner_issue
WHERE workspace_id = $1 AND id = $2
LIMIT 1
FOR UPDATE;

-- name: ListPartnerIssuesForUser :many
SELECT id, workspace_id, provider, group_key, platform, external_id, external_type, external_click_id, start_mode, issue_key,
       app_id, platform_id, platform_user_id, public_payload, private_payload, rewards_snapshot,
       status, issued_at, started_at, completed_at, claimed_at, expires_at, created_at, updated_at
FROM task_partner_issue
WHERE workspace_id = $1
  AND provider = $2
  AND group_key = $3
  AND platform = $4
  AND app_id = $5
  AND platform_id = $6
  AND platform_user_id = $7
  AND status IN ('issued', 'completed')
  AND (status = 'completed' OR expires_at IS NULL OR expires_at > $8)
ORDER BY issued_at DESC, id DESC;

-- name: GetPartnerIssueByExternalClickID :one
SELECT id, workspace_id, provider, group_key, platform, external_id, external_type, external_click_id, start_mode, issue_key,
       app_id, platform_id, platform_user_id, public_payload, private_payload, rewards_snapshot,
       status, issued_at, started_at, completed_at, claimed_at, expires_at, created_at, updated_at
FROM task_partner_issue
WHERE workspace_id = $1 AND provider = $2 AND external_click_id = $3
LIMIT 1;

-- name: GetPartnerIssuesByExternalUser :many
SELECT id, workspace_id, provider, group_key, platform, external_id, external_type, external_click_id, start_mode, issue_key,
       app_id, platform_id, platform_user_id, public_payload, private_payload, rewards_snapshot,
       status, issued_at, started_at, completed_at, claimed_at, expires_at, created_at, updated_at
FROM task_partner_issue
WHERE workspace_id = $1
  AND provider = $2
  AND group_key = $3
  AND platform = $4
  AND external_id = $5
  AND platform_user_id = $6
  AND (sqlc.arg(app_id)::bigint = 0 OR app_id = sqlc.arg(app_id)::bigint)
  AND (sqlc.arg(platform_id)::bigint = 0 OR platform_id = sqlc.arg(platform_id)::bigint)
ORDER BY issued_at DESC, id DESC
LIMIT 2;

-- name: GetPartnerIssuesByPrivatePayloadUser :many
SELECT id, workspace_id, provider, group_key, platform, external_id, external_type, external_click_id, start_mode, issue_key,
       app_id, platform_id, platform_user_id, public_payload, private_payload, rewards_snapshot,
       status, issued_at, started_at, completed_at, claimed_at, expires_at, created_at, updated_at
FROM task_partner_issue
WHERE workspace_id = $1
  AND provider = $2
  AND group_key = $3
  AND platform = $4
  AND private_payload @> jsonb_build_object(sqlc.arg(lookup_key)::text, sqlc.arg(lookup_value)::text)
  AND platform_user_id = $5
  AND (sqlc.arg(app_id)::bigint = 0 OR app_id = sqlc.arg(app_id)::bigint)
  AND (sqlc.arg(platform_id)::bigint = 0 OR platform_id = sqlc.arg(platform_id)::bigint)
ORDER BY issued_at DESC, id DESC
LIMIT 2;

-- name: AcquirePartnerIssueStartLease :one
INSERT INTO task_partner_issue_start_lease (
    workspace_id,
    issue_id,
    lease_token,
    lease_until
)
SELECT
    sqlc.arg(workspace_id)::varchar,
    sqlc.arg(issue_id)::bigint,
    sqlc.arg(lease_token)::varchar,
    now() + (
        sqlc.arg(lease_duration_milliseconds)::bigint * INTERVAL '1 millisecond'
    )
FROM task_partner_issue AS issue
WHERE issue.workspace_id = sqlc.arg(workspace_id)::varchar
  AND issue.id = sqlc.arg(issue_id)::bigint
  AND issue.status = 'issued'
  AND issue.started_at IS NULL
FOR UPDATE OF issue
ON CONFLICT (workspace_id, issue_id) DO UPDATE SET
    lease_token = EXCLUDED.lease_token,
    lease_until = EXCLUDED.lease_until,
    updated_at = now()
WHERE task_partner_issue_start_lease.lease_until <= now()
RETURNING lease_token;

-- name: RenewPartnerIssueStartLease :execrows
UPDATE task_partner_issue_start_lease
SET lease_until = now() + (
        sqlc.arg(lease_duration_milliseconds)::bigint * INTERVAL '1 millisecond'
    ),
    updated_at = now()
WHERE workspace_id = sqlc.arg(workspace_id)::varchar
  AND issue_id = sqlc.arg(issue_id)::bigint
  AND lease_token = sqlc.arg(lease_token)::varchar
  AND lease_until > now();

-- name: HasActivePartnerIssueStartLease :one
SELECT EXISTS (
    SELECT 1
    FROM task_partner_issue_start_lease
    WHERE workspace_id = $1
      AND issue_id = $2
      AND lease_until > now()
);

-- name: ReleasePartnerIssueStartLease :execrows
DELETE FROM task_partner_issue_start_lease
WHERE workspace_id = $1
  AND issue_id = $2
  AND lease_token = $3;

-- name: UpdatePartnerIssueStart :execrows
UPDATE task_partner_issue AS issue
SET external_click_id = COALESCE(
        NULLIF(sqlc.arg(external_click_id)::varchar, ''),
        issue.external_click_id
    ),
    started_at = now(),
    public_payload = sqlc.arg(public_payload)::jsonb,
    private_payload = sqlc.arg(private_payload)::jsonb,
    updated_at = now()
WHERE issue.workspace_id = sqlc.arg(workspace_id)::varchar
  AND issue.id = sqlc.arg(issue_id)::bigint
  AND issue.status IN ('issued', 'completed')
  AND issue.started_at IS NULL
  AND EXISTS (
      SELECT 1
      FROM task_partner_issue_start_lease AS lease
      WHERE lease.workspace_id = sqlc.arg(workspace_id)::varchar
        AND lease.issue_id = sqlc.arg(issue_id)::bigint
        AND lease.lease_token = sqlc.arg(lease_token)::varchar
        AND lease.lease_until > now()
  );

-- name: CompletePartnerIssue :execrows
UPDATE task_partner_issue
SET status = 'completed', completed_at = $1, updated_at = now()
WHERE workspace_id = $2 AND id = $3 AND status = 'issued';

-- name: ExpirePartnerIssue :execrows
UPDATE task_partner_issue
SET status = 'expired', updated_at = now()
WHERE workspace_id = $1 AND id = $2 AND status IN ('issued', 'completed');

-- name: ClaimPartnerIssue :execrows
UPDATE task_partner_issue
SET status = 'claimed', claimed_at = $1, updated_at = now()
WHERE workspace_id = $2 AND id = $3 AND status = 'completed';

-- name: RevokePartnerIssue :execrows
UPDATE task_partner_issue
SET status = CASE
        WHEN status = 'claimed' THEN 'revoked_after_claim'
        ELSE 'revoked'
    END,
    updated_at = now()
WHERE workspace_id = $1
  AND id = $2
  AND status IN ('issued', 'completed', 'claimed');

-- name: InsertPartnerRewardGrant :execrows
INSERT INTO task_partner_reward_grant (
    workspace_id, issue_id, provider, group_key, external_type,
    app_id, platform_id, platform_user_id, operation_id, reward_snapshot, claimed_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (workspace_id, issue_id) DO NOTHING;

-- name: ReserveRewardOperation :execrows
INSERT INTO task_reward_operation (
    workspace_id,
    operation_id,
    source_kind,
    source_id
)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, operation_id) DO NOTHING;

-- name: GetPartnerRewardGrantByIssue :one
SELECT id, workspace_id, issue_id, provider, group_key, external_type,
       app_id, platform_id, platform_user_id, operation_id, reward_snapshot, claimed_at, created_at
FROM task_partner_reward_grant
WHERE workspace_id = $1 AND issue_id = $2
LIMIT 1;

-- name: InsertPartnerStatsEvent :execrows
INSERT INTO task_partner_stats_event (
    workspace_id, provider, group_key, external_type, issue_id, external_id,
    app_id, platform_id, platform_user_id, event_type, event_key, status, payload, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (workspace_id, event_key) DO NOTHING;

-- name: InsertPartnerStatsUniqueUser :execrows
INSERT INTO task_partner_stats_unique_user (
    workspace_id, stats_date, provider, group_key, external_type, event_type,
    app_id, platform_id, platform_user_id
) VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (
    workspace_id, stats_date, provider, group_key, external_type,
    event_type, app_id, platform_id, platform_user_id
) DO NOTHING;

-- name: IncrementPartnerStatsDaily :exec
INSERT INTO task_partner_stats_daily (
    workspace_id, stats_date, provider, group_key, external_type,
    issued_count, completed_count, claimed_count, revoked_count, revoked_after_claim_count,
    failed_count, fake_count, expired_count,
    unique_issued_users, unique_completed_users, unique_claimers
) VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
ON CONFLICT (workspace_id, stats_date, provider, group_key, external_type) DO UPDATE SET
    issued_count = task_partner_stats_daily.issued_count + EXCLUDED.issued_count,
    completed_count = task_partner_stats_daily.completed_count + EXCLUDED.completed_count,
    claimed_count = task_partner_stats_daily.claimed_count + EXCLUDED.claimed_count,
    revoked_count = task_partner_stats_daily.revoked_count + EXCLUDED.revoked_count,
    revoked_after_claim_count = task_partner_stats_daily.revoked_after_claim_count + EXCLUDED.revoked_after_claim_count,
    failed_count = task_partner_stats_daily.failed_count + EXCLUDED.failed_count,
    fake_count = task_partner_stats_daily.fake_count + EXCLUDED.fake_count,
    expired_count = task_partner_stats_daily.expired_count + EXCLUDED.expired_count,
    unique_issued_users = task_partner_stats_daily.unique_issued_users + EXCLUDED.unique_issued_users,
    unique_completed_users = task_partner_stats_daily.unique_completed_users + EXCLUDED.unique_completed_users,
    unique_claimers = task_partner_stats_daily.unique_claimers + EXCLUDED.unique_claimers,
    updated_at = now();

-- name: AdminListPartnerDailyStats :many
SELECT workspace_id, stats_date, provider, group_key, external_type,
       issued_count, completed_count, claimed_count, revoked_count, revoked_after_claim_count,
       failed_count, fake_count, expired_count,
       unique_issued_users, unique_completed_users, unique_claimers, updated_at
FROM task_partner_stats_daily
WHERE workspace_id = $1
  AND stats_date >= $2
  AND stats_date < $3
  AND ($4 = '' OR provider = $5)
  AND ($6 = '' OR group_key = $7)
ORDER BY stats_date, provider, group_key, external_type;
