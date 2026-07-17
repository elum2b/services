-- name: AdminCreateItem :exec
INSERT INTO reference_item (
    workspace_id, key, item_type, payload, is_active
) VALUES ($1, $2, $3, $4, $5);

-- name: AdminUpdateItem :execrows
UPDATE reference_item
SET payload = $1,
    is_active = $2,
    updated_at = now()
WHERE workspace_id = $3
  AND key = $4
  AND deleted_at IS NULL;

-- name: AdminDangerousChangeType :execrows
UPDATE reference_item
SET item_type = $1
WHERE workspace_id = $2
  AND key = $3
  AND item_type = $4
  AND deleted_at IS NULL;

-- name: AdminSoftDeleteItem :execrows
UPDATE reference_item
SET is_active = FALSE,
    deleted_at = now(),
    updated_at = now()
WHERE workspace_id = $1
  AND key = $2
  AND deleted_at IS NULL;

-- name: AdminRestoreItem :execrows
UPDATE reference_item
SET is_active = $1,
    deleted_at = NULL,
    updated_at = now()
WHERE workspace_id = $2
  AND key = $3
  AND deleted_at IS NOT NULL;

-- name: GetItemBundle :many
SELECT
    i.workspace_id,
    i.key,
    i.item_type::text AS item_type,
    i.payload,
    i.is_active,
    i.deleted_at,
    i.created_at,
    i.updated_at,
    l.locale,
    l.title,
    l.description
FROM reference_item i
LEFT JOIN reference_localization l
  ON l.workspace_id = i.workspace_id
 AND l.item_key = i.key
 AND l.locale = $1
WHERE i.workspace_id = $2
  AND i.key = $3
  AND i.deleted_at IS NULL
  AND i.is_active = TRUE
LIMIT 1;

-- name: ResolveItemBundles :many
SELECT
    i.workspace_id,
    i.key,
    i.item_type::text AS item_type,
    i.payload,
    i.is_active,
    i.deleted_at,
    i.created_at,
    i.updated_at,
    l.locale,
    l.title,
    l.description
FROM reference_item i
JOIN unnest($3::text[]) WITH ORDINALITY AS requested(key, position)
  ON requested.key = i.key
LEFT JOIN reference_localization l
  ON l.workspace_id = i.workspace_id
 AND l.item_key = i.key
 AND l.locale = $1
WHERE i.workspace_id = $2
  AND i.deleted_at IS NULL
  AND i.is_active = TRUE
ORDER BY requested.position;

-- name: ListItemBundles :many
SELECT
    i.workspace_id,
    i.key,
    i.item_type::text AS item_type,
    i.payload,
    i.is_active,
    i.deleted_at,
    i.created_at,
    i.updated_at,
    l.locale,
    l.title,
    l.description
FROM reference_item i
LEFT JOIN reference_localization l
  ON l.workspace_id = i.workspace_id
 AND l.item_key = i.key
 AND l.locale = $1
WHERE i.workspace_id = $2
  AND i.deleted_at IS NULL
  AND i.is_active = TRUE
ORDER BY i.key
LIMIT $3 OFFSET $4;

-- name: AdminGetItemBundle :many
SELECT
    i.workspace_id,
    i.key,
    i.item_type::text AS item_type,
    i.payload,
    i.is_active,
    i.deleted_at,
    i.created_at,
    i.updated_at,
    l.locale,
    l.title,
    l.description
FROM reference_item i
LEFT JOIN reference_localization l
  ON l.workspace_id = i.workspace_id
 AND l.item_key = i.key
WHERE i.workspace_id = $1
  AND i.key = $2
ORDER BY l.locale;

-- name: AdminListItems :many
SELECT
    workspace_id,
    key,
    item_type::text AS item_type,
    payload,
    is_active,
    deleted_at,
    created_at,
    updated_at
FROM reference_item
WHERE workspace_id = $1
  AND ($2 = '' OR item_type = $3)
  AND ($4 = FALSE OR deleted_at IS NULL)
ORDER BY key
LIMIT $5 OFFSET $6;

-- name: AdminUpsertLocalization :exec
INSERT INTO reference_localization (
    workspace_id, item_key, locale, title, description
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, item_key, locale) DO UPDATE SET
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    updated_at = now();

-- name: AdminGetLocalization :one
SELECT
    workspace_id, item_key, locale, title, description, created_at, updated_at
FROM reference_localization
WHERE workspace_id = $1
  AND item_key = $2
  AND locale = $3
LIMIT 1;

-- name: AdminListLocalizations :many
SELECT
    workspace_id, item_key, locale, title, description, created_at, updated_at
FROM reference_localization
WHERE workspace_id = $1
  AND item_key = $2
ORDER BY locale;

-- name: AdminDeleteLocalization :execrows
DELETE FROM reference_localization
WHERE workspace_id = $1
  AND item_key = $2
  AND locale = $3;

-- name: AdminGetStats :one
SELECT
    COUNT(*)::bigint AS items_total,
    COUNT(*) FILTER (WHERE deleted_at IS NULL)::bigint AS items_not_deleted,
    COUNT(*) FILTER (WHERE deleted_at IS NULL AND is_active = TRUE)::bigint AS active_items,
    COUNT(*) FILTER (WHERE deleted_at IS NOT NULL)::bigint AS deleted_items,
    COUNT(*) FILTER (WHERE deleted_at IS NULL AND item_type = 'quantity')::bigint AS quantity_items,
    COUNT(*) FILTER (WHERE deleted_at IS NULL AND item_type = 'duration')::bigint AS duration_items
FROM reference_item
WHERE workspace_id = $1;

-- name: ListExportItems :many
SELECT
    workspace_id,
    key,
    item_type::text AS item_type,
    payload,
    is_active,
    deleted_at,
    created_at,
    updated_at
FROM reference_item
WHERE workspace_id = $1
  AND ($2 = FALSE OR deleted_at IS NULL)
ORDER BY key;

-- name: ListExportLocalizations :many
SELECT
    workspace_id,
    item_key,
    locale,
    title,
    description,
    created_at,
    updated_at
FROM reference_localization
WHERE workspace_id = $1
ORDER BY item_key, locale;

-- name: ListImportItemKeys :many
SELECT key
FROM reference_item
WHERE workspace_id = $1;
