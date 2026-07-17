-- name: AdminCreatePromo :one
INSERT INTO promo_offer (
    workspace_id, code, code_normalized, payload, target, max_activations,
    is_active, start_at, end_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id;

-- name: AdminUpdatePromo :execrows
UPDATE promo_offer
SET code = $1,
    code_normalized = $2,
    payload = $3,
    target = $4,
    max_activations = $5,
    is_active = $6,
    start_at = $7,
    end_at = $8,
    updated_at = now()
WHERE workspace_id = $9 AND id = $10 AND deleted_at IS NULL;

-- name: AdminGetPromo :one
SELECT *
FROM promo_offer
WHERE workspace_id = $1 AND id = $2
LIMIT 1;

-- name: AdminListPromos :many
SELECT *
FROM promo_offer
WHERE workspace_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: ListExportPromos :many
SELECT *
FROM promo_offer
WHERE workspace_id = $1
ORDER BY created_at DESC, id DESC;

-- name: ListExportLocalizations :many
SELECT *
FROM promo_localization
WHERE workspace_id = $1
ORDER BY promo_id, locale;

-- name: ListExportRewards :many
SELECT *
FROM promo_reward
WHERE workspace_id = $1
ORDER BY promo_id, id;

-- name: ListImportPromoCodes :many
SELECT code_normalized
FROM promo_offer
WHERE workspace_id = $1
  AND deleted_at IS NULL
ORDER BY code_normalized;

-- name: ListImportPromoIDs :many
SELECT id, code_normalized
FROM promo_offer
WHERE workspace_id = $1
  AND deleted_at IS NULL
ORDER BY code_normalized;

-- name: AdminSoftDeletePromo :execrows
UPDATE promo_offer
SET deleted_at = now(), is_active = FALSE, updated_at = now()
WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: GetApplyBundleForUpdate :many
SELECT
    o.id,
    o.workspace_id,
    o.code,
    o.payload,
    o.target,
    o.max_activations,
    o.activation_count,
    o.is_active,
    o.start_at,
    o.end_at,
    o.deleted_at,
    o.created_at,
    o.updated_at,
    l.locale AS localization_locale,
    l.title AS localization_title,
    l.description AS localization_description,
    a.id AS redemption_id,
    a.app_id AS redemption_app_id,
    a.platform_id AS redemption_platform_id,
    a.platform_user_id AS redemption_platform_user_id,
    a.redeemed_at AS redemption_redeemed_at,
    a.reward_snapshot AS redemption_reward_snapshot,
    r.id AS reward_id,
    r.reward_key,
    r.reward_type,
    r.quantity AS reward_quantity,
    r.scale AS reward_scale,
    r.duration_unit
FROM promo_offer o
LEFT JOIN promo_localization l
  ON l.workspace_id = o.workspace_id
 AND l.promo_id = o.id
 AND l.locale = $1
LEFT JOIN promo_redemption a
  ON a.workspace_id = o.workspace_id
 AND a.promo_id = o.id
 AND a.app_id = $2
 AND a.platform_id = $3
 AND a.platform_user_id = $4
LEFT JOIN promo_reward r
  ON r.workspace_id = o.workspace_id
 AND r.promo_id = o.id
WHERE o.workspace_id = $5
  AND o.code_normalized = $6
ORDER BY r.id
FOR UPDATE OF o;

-- name: AdminUpsertLocalization :exec
INSERT INTO promo_localization (
    workspace_id, promo_id, locale, title, description
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, promo_id, locale) DO UPDATE SET
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    updated_at = now();

-- name: AdminListLocalizations :many
SELECT *
FROM promo_localization
WHERE workspace_id = $1 AND promo_id = $2
ORDER BY locale;

-- name: AdminGetLocalization :one
SELECT *
FROM promo_localization
WHERE workspace_id = $1 AND promo_id = $2 AND locale = $3
LIMIT 1;

-- name: AdminDeleteLocalization :execrows
DELETE FROM promo_localization
WHERE workspace_id = $1 AND promo_id = $2 AND locale = $3;

-- name: AdminUpsertReward :exec
INSERT INTO promo_reward (
    workspace_id, promo_id, reward_key, reward_type, quantity, scale, duration_unit
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (workspace_id, promo_id, reward_key) DO UPDATE SET
    reward_type = EXCLUDED.reward_type,
    quantity = EXCLUDED.quantity,
    scale = EXCLUDED.scale,
    duration_unit = EXCLUDED.duration_unit,
    updated_at = now();

-- name: AdminGetReward :one
SELECT *
FROM promo_reward
WHERE workspace_id = $1 AND promo_id = $2 AND reward_key = $3
LIMIT 1;

-- name: ListRewards :many
SELECT *
FROM promo_reward
WHERE workspace_id = $1 AND promo_id = $2
ORDER BY id;

-- name: AdminDeleteReward :execrows
DELETE FROM promo_reward
WHERE workspace_id = $1 AND promo_id = $2 AND reward_key = $3;

-- name: GetRedemption :one
SELECT *
FROM promo_redemption
WHERE workspace_id = $1
  AND promo_id = $2
  AND app_id = $3
  AND platform_id = $4
  AND platform_user_id = $5
LIMIT 1;

-- name: CreateRedemption :one
WITH inserted AS (
    INSERT INTO promo_redemption (
        workspace_id, promo_id, app_id, platform_id, platform_user_id,
        reward_snapshot
    ) VALUES ($1, $2, $3, $4, $5, $6)
    RETURNING
        id,
        workspace_id,
        promo_id,
        app_id,
        platform_id,
        platform_user_id,
        reward_snapshot,
        redeemed_at
),
updated_offer AS (
    UPDATE promo_offer o
    SET activation_count = activation_count + 1,
        updated_at = now()
    FROM inserted i
    WHERE o.workspace_id = i.workspace_id
      AND o.id = i.promo_id
    RETURNING o.code
),
created_event AS (
    INSERT INTO promo_redemption_event (
        workspace_id, promo_id, redemption_id
    )
    SELECT workspace_id, promo_id, id
    FROM inserted
    RETURNING redemption_id
),
created_callback AS (
    INSERT INTO promo_clb_event (
        workspace_id,
        source_service,
        event_type,
        event_key,
        idempotency_key,
        payload,
        payload_content_type,
        next_attempt_at
    )
    SELECT
        i.workspace_id,
        'promo',
        'promo.applied',
        'promo.applied:' || i.id::text,
        'promo.applied:' || i.id::text,
        jsonb_build_object(
            'redemption_id', i.id,
            'workspace_id', i.workspace_id,
            'promo_id', i.promo_id,
            'code', u.code,
            'app_id', i.app_id,
            'platform_id', i.platform_id,
            'platform_user_id', i.platform_user_id,
            'rewards', i.reward_snapshot
        )::text::bytea,
        'application/json',
        now()
    FROM inserted i
    CROSS JOIN updated_offer u
    RETURNING id
)
SELECT id, redeemed_at
FROM inserted;

-- name: AdminListRedemptions :many
SELECT *
FROM promo_redemption
WHERE workspace_id = $1
  AND promo_id = $2
ORDER BY redeemed_at DESC, id DESC
LIMIT $3 OFFSET $4;

-- name: AdminGetStats :one
SELECT
    activation_count,
    max_activations,
    CASE
        WHEN max_activations = 0 THEN -1
        ELSE max_activations - activation_count
    END::bigint AS remaining_activations
FROM promo_offer
WHERE workspace_id = $1 AND id = $2
LIMIT 1;

-- name: AdminListDailyStats :many
SELECT *
FROM promo_stats_daily
WHERE workspace_id = $1
  AND promo_id = $2
  AND stats_date >= $3
  AND stats_date <= $4
ORDER BY stats_date;

-- name: RefreshDailyStats :exec
INSERT INTO promo_stats_daily (
    workspace_id, promo_id, stats_date, redemption_count, unique_users
)
SELECT
    e.workspace_id,
    e.promo_id,
    e.occurred_at::date,
    COUNT(*),
    COUNT(*)
FROM promo_redemption_event e
WHERE e.workspace_id = sqlc.arg(workspace_id)
  AND e.occurred_at >= $1 AND e.occurred_at < $2
GROUP BY e.workspace_id, e.promo_id, e.occurred_at::date
ON CONFLICT (workspace_id, promo_id, stats_date) DO UPDATE SET
    redemption_count = EXCLUDED.redemption_count,
    unique_users = EXCLUDED.unique_users,
    updated_at = now();
