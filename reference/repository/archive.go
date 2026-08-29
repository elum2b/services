package repository

import (
	"context"
	"fmt"
)

// ExportArchiveData returns only current, non-deleted resources and links.
// Media objects are intentionally read by the archive handler, not the DB.
func (r *Repository) ExportArchiveData(ctx context.Context, workspaceID string) ([]ExportResource, []ExportResourceLink, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return nil, nil, err
	}

	rows, err := r.executor.QueryContext(ctx, `
SELECT key, resource_type, payload, is_active, format, content_type,
       source_sha256, media_version, source_size, width, height, placeholder_ref
FROM reference_resource
WHERE workspace_id = $1 AND deleted_at IS NULL
ORDER BY key`, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	resources := make([]ExportResource, 0)
	for rows.Next() {
		var value ExportResource
		if err := rows.Scan(&value.Key, &value.Type, &value.Payload, &value.IsActive,
			&value.Format, &value.ContentType, &value.SHA256, &value.MediaVersion,
			&value.Size, &value.Width, &value.Height, &value.PlaceholderRef); err != nil {
			return nil, nil, err
		}
		resources = append(resources, value)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	links, err := r.executor.QueryContext(ctx, `
SELECT ir.item_key, ir.resource_key, ir.position
FROM reference_item_resource ir
JOIN reference_item i ON i.workspace_id = ir.workspace_id AND i.key = ir.item_key
JOIN reference_resource r ON r.workspace_id = ir.workspace_id AND r.key = ir.resource_key
WHERE ir.workspace_id = $1 AND i.deleted_at IS NULL AND r.deleted_at IS NULL
ORDER BY ir.item_key, ir.position`, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	defer links.Close()

	result := make([]ExportResourceLink, 0)
	for links.Next() {
		var link ExportResourceLink
		if err := links.Scan(&link.ItemKey, &link.ResourceKey, &link.Position); err != nil {
			return nil, nil, err
		}
		result = append(result, link)
	}
	return resources, result, links.Err()
}

// ImportArchiveResources persists media-backed resource metadata and links.
// Callers must write media to storage before calling this method.
func (r *Repository) ImportArchiveResources(ctx context.Context, workspaceID string, resources []Resource, links []ExportResourceLink, strategy string) error {
	if err := requireWorkspace(workspaceID); err != nil {
		return err
	}
	if len(resources) == 0 && len(links) == 0 {
		return nil
	}

	err := r.withWorkspaceMutation(ctx, workspaceID, func(tx *Repository) error {
		for _, value := range resources {
			var exists bool
			if err := tx.executor.QueryRowContext(ctx,
				"SELECT EXISTS (SELECT 1 FROM reference_resource WHERE workspace_id = $1 AND key = $2)", workspaceID, value.Key).Scan(&exists); err != nil {
				return err
			}
			if exists && strategy == ImportConflictFail {
				return fmt.Errorf("import conflict: resource %s", value.Key)
			}
			if exists && strategy == ImportConflictSkip {
				continue
			}
			if exists {
				if _, err := tx.executor.ExecContext(ctx, `UPDATE reference_resource_media_version
SET retired_at = now() WHERE workspace_id = $1 AND resource_key = $2 AND retired_at IS NULL`, workspaceID, value.Key); err != nil {
					return err
				}
			}
			if _, err := tx.executor.ExecContext(ctx, `
INSERT INTO reference_resource (workspace_id, key, resource_type, payload, is_active, format, content_type, source_size, source_sha256, media_version, width, height, original_ref, preview_61_ref, preview_128_ref, preview_256_ref, preview_512_ref, placeholder_ref)
VALUES ($1::varchar,$2::varchar,$3::varchar,$4::jsonb,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
ON CONFLICT (workspace_id, key) DO UPDATE SET resource_type=EXCLUDED.resource_type, payload=EXCLUDED.payload, is_active=EXCLUDED.is_active, deleted_at=NULL, format=EXCLUDED.format, content_type=EXCLUDED.content_type, source_size=EXCLUDED.source_size, source_sha256=EXCLUDED.source_sha256, media_version=EXCLUDED.media_version, width=EXCLUDED.width, height=EXCLUDED.height, original_ref=EXCLUDED.original_ref, preview_61_ref=EXCLUDED.preview_61_ref, preview_128_ref=EXCLUDED.preview_128_ref, preview_256_ref=EXCLUDED.preview_256_ref, preview_512_ref=EXCLUDED.preview_512_ref, placeholder_ref=EXCLUDED.placeholder_ref, updated_at=now()`,
				workspaceID, value.Key, value.Type, value.Payload, value.IsActive, value.Format, value.ContentType, value.Size, value.SHA256, value.MediaVersion, value.Width, value.Height, value.OriginalRef, value.Preview61Ref, value.Preview128Ref, value.Preview256Ref, value.Preview512Ref, value.PlaceholderRef); err != nil {
				return err
			}
			if _, err := tx.executor.ExecContext(ctx, `INSERT INTO reference_resource_media_version (workspace_id, resource_key, media_version)
VALUES ($1, $2, $3) ON CONFLICT (workspace_id, resource_key, media_version) DO UPDATE SET retired_at = NULL`, workspaceID, value.Key, value.MediaVersion); err != nil {
				return err
			}
		}

		if strategy == ImportConflictUpdate && len(links) > 0 {
			keys := make([]string, 0, len(links))
			for _, link := range links {
				keys = append(keys, link.ItemKey)
			}
			if _, err := tx.executor.ExecContext(ctx, `DELETE FROM reference_item_resource WHERE workspace_id = $1 AND item_key = ANY($2::text[])`, workspaceID, keys); err != nil {
				return err
			}
		}
		for _, link := range links {
			if _, err := tx.executor.ExecContext(ctx, `INSERT INTO reference_item_resource (workspace_id, item_key, resource_key, position)
SELECT $1::varchar, $2::varchar, $3::varchar, $4 WHERE EXISTS (SELECT 1 FROM reference_item WHERE workspace_id=$1::varchar AND key=$2::varchar AND deleted_at IS NULL) AND EXISTS (SELECT 1 FROM reference_resource WHERE workspace_id=$1::varchar AND key=$3::varchar AND deleted_at IS NULL)
ON CONFLICT (workspace_id, item_key, resource_key) DO UPDATE SET position=EXCLUDED.position`, workspaceID, link.ItemKey, link.ResourceKey, link.Position); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return r.bumpReferenceCacheVersions(workspaceID, "resource_get", "resource_list", "resource_item_list", referenceCacheGet, referenceCacheResolve, referenceCacheList)
}

func (r *Repository) ArchiveResourceConflicts(ctx context.Context, workspaceID string, resources []ExportResource) ([]string, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return nil, err
	}
	conflicts := make([]string, 0)
	for _, resource := range resources {
		var exists bool
		if err := r.executor.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM reference_resource WHERE workspace_id = $1 AND key = $2)", workspaceID, resource.Key).Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			conflicts = append(conflicts, resource.Key)
		}
	}
	return conflicts, nil
}
