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

-- name: ResourceCreate :exec
INSERT INTO reference_resource (
    workspace_id, key, resource_type, payload, is_active, format, content_type,
    source_size, source_sha256, media_version, width, height, original_ref, preview_61_ref,
    preview_128_ref, preview_256_ref, preview_512_ref, placeholder_ref
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18);

-- name: ResourceMediaVersionCreate :exec
INSERT INTO reference_resource_media_version (workspace_id, resource_key, media_version)
VALUES ($1, $2, $3)
ON CONFLICT (workspace_id, resource_key, media_version) DO UPDATE SET retired_at = NULL;

-- name: ResourceMediaVersionRetireActive :exec
UPDATE reference_resource_media_version
SET retired_at = now()
WHERE workspace_id = $1 AND resource_key = $2 AND retired_at IS NULL;

-- name: ResourceUpdate :execrows
UPDATE reference_resource
SET resource_type = $1, payload = $2, is_active = $3, format = $4,
    content_type = $5, source_size = $6, source_sha256 = $7, media_version = $8, width = $9,
    height = $10, original_ref = $11, preview_61_ref = $12,
    preview_128_ref = $13, preview_256_ref = $14, preview_512_ref = $15,
    placeholder_ref = $16, updated_at = now()
WHERE workspace_id = $17 AND key = $18 AND deleted_at IS NULL;

-- name: ResourceSoftDelete :execrows
UPDATE reference_resource
SET is_active = FALSE, deleted_at = now(), updated_at = now()
WHERE workspace_id = $1 AND key = $2 AND deleted_at IS NULL;

-- name: ResourceListGarbageMediaVersions :many
SELECT workspace_id, resource_key, media_version
FROM reference_resource_media_version
WHERE retired_at <= $1
ORDER BY retired_at
LIMIT $2;

-- name: ResourceDeleteMediaVersion :execrows
DELETE FROM reference_resource_media_version
WHERE workspace_id = $1 AND resource_key = $2 AND media_version = $3 AND retired_at <= $4;

-- name: ResourcePurgeDeleted :execrows
WITH removed_links AS (
    DELETE FROM reference_item_resource
    WHERE workspace_id = $1 AND resource_key = $2
)
DELETE FROM reference_resource
WHERE reference_resource.workspace_id = $1
  AND reference_resource.key = $2
  AND reference_resource.deleted_at IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM reference_resource_media_version
      WHERE workspace_id = $1 AND resource_key = $2
  );

-- name: ResourceGet :one
SELECT * FROM reference_resource
WHERE workspace_id = $1 AND key = $2 AND deleted_at IS NULL;

-- name: ResourceList :many
SELECT * FROM reference_resource
WHERE workspace_id = $1 AND deleted_at IS NULL
ORDER BY key LIMIT $2 OFFSET $3;

-- name: ResourceAttach :execrows
INSERT INTO reference_item_resource (workspace_id, item_key, resource_key, position)
SELECT sqlc.arg(workspace_id)::varchar, sqlc.arg(item_key)::varchar, sqlc.arg(resource_key)::varchar, sqlc.arg(position)
WHERE EXISTS (SELECT 1 FROM reference_item WHERE workspace_id = sqlc.arg(workspace_id)::varchar AND key = sqlc.arg(item_key)::varchar AND deleted_at IS NULL)
  AND EXISTS (SELECT 1 FROM reference_resource WHERE workspace_id = sqlc.arg(workspace_id)::varchar AND key = sqlc.arg(resource_key)::varchar AND deleted_at IS NULL)
ON CONFLICT (workspace_id, item_key, resource_key) DO UPDATE SET position = EXCLUDED.position;

-- name: ResourceDetach :execrows
DELETE FROM reference_item_resource
WHERE workspace_id = $1 AND item_key = $2 AND resource_key = $3;

-- name: ResourceListItemResources :many
SELECT r.*, ir.item_key, ir.position
FROM reference_item_resource ir
JOIN reference_resource r ON r.workspace_id = ir.workspace_id AND r.key = ir.resource_key
WHERE ir.workspace_id = $1 AND ir.item_key = $2 AND r.deleted_at IS NULL
ORDER BY ir.position;

-- name: ResourceListActiveForItems :many
SELECT r.*, ir.item_key, ir.position
FROM reference_item_resource ir
JOIN reference_resource r ON r.workspace_id = ir.workspace_id AND r.key = ir.resource_key
WHERE ir.workspace_id = $1 AND ir.item_key = ANY($2::text[])
  AND r.deleted_at IS NULL AND r.is_active = TRUE
ORDER BY ir.item_key, ir.position;
