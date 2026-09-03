package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	json "github.com/goccy/go-json"
	"github.com/jackc/pgx/v5/pgconn"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	refsqlc "github.com/elum2b/services/reference/sqlc"
)

type Resource struct {
	WorkspaceID, Key, Type                                                                 string
	Payload                                                                                json.RawMessage
	IsActive                                                                               bool
	DeletedAt                                                                              *time.Time
	Format, ContentType, SHA256, MediaVersion                                              string
	Size                                                                                   int64
	Width, Height                                                                          int
	OriginalRef, Preview61Ref, Preview128Ref, Preview256Ref, Preview512Ref, PlaceholderRef string
	CreatedAt, UpdatedAt                                                                   time.Time
}

// ResourceSave writes media and populates its object references. Its result is
// true only when the media write must be compensated after transaction failure.
type ResourceSave func(context.Context, *Resource) (bool, error)

func mapResource(row refsqlc.ReferenceResource) Resource {
	return Resource{
		WorkspaceID:    row.WorkspaceID,
		Key:            row.Key,
		Type:           row.ResourceType,
		Payload:        row.Payload,
		IsActive:       row.IsActive,
		DeletedAt:      sqlwrap.NullTimePtr(row.DeletedAt),
		Format:         row.Format,
		ContentType:    row.ContentType,
		SHA256:         row.SourceSha256,
		MediaVersion:   row.MediaVersion,
		Size:           row.SourceSize,
		Width:          int(row.Width),
		Height:         int(row.Height),
		OriginalRef:    row.OriginalRef,
		Preview61Ref:   row.Preview61Ref,
		Preview128Ref:  row.Preview128Ref,
		Preview256Ref:  row.Preview256Ref,
		Preview512Ref:  row.Preview512Ref,
		PlaceholderRef: row.PlaceholderRef,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func (r *Repository) CreateResource(ctx context.Context, value Resource) error {
	_, err := r.CreateResourceWithSave(ctx, value, nil)
	return err
}

func (r *Repository) CreateResourceWithSave(
	ctx context.Context,
	value Resource,
	save ResourceSave,
) (bool, error) {
	if err := requireWorkspace(value.WorkspaceID); err != nil {
		return false, err
	}

	written := false
	initial := value

	if save != nil {
		clearResourceObjectRefs(&initial)
	}

	err := r.withWorkspaceMutation(
		ctx,
		value.WorkspaceID,
		func(tx *Repository) error {
			if err := tx.q.ResourceCreate(
				ctx,
				resourceParams(initial),
			); err != nil {
				return err
			}

			if err := tx.q.ResourceMediaVersionCreate(
				ctx,
				refsqlc.ResourceMediaVersionCreateParams{
					WorkspaceID:  initial.WorkspaceID,
					ResourceKey:  initial.Key,
					MediaVersion: initial.MediaVersion,
				},
			); err != nil {
				return err
			}

			if save == nil {
				return nil
			}

			var err error

			written, err = save(ctx, &value)
			if err != nil {
				return err
			}

			return tx.updateResourceObjectRefs(ctx, value)
		},
	)
	if err != nil {
		return written, err
	}

	return false, r.bumpReferenceCacheVersions(
		value.WorkspaceID,
		"resource_get",
		"resource_list",
		"resource_item_list",
	)
}

func (r *Repository) UpdateResource(
	ctx context.Context,
	value Resource,
) (int64, error) {
	rows, _, err := r.UpdateResourceWithSave(ctx, value, nil)
	return rows, err
}

func (r *Repository) UpdateResourceWithSave(
	ctx context.Context,
	value Resource,
	save ResourceSave,
) (int64, bool, error) {
	if err := requireWorkspace(value.WorkspaceID); err != nil {
		return 0, false, err
	}

	var rows int64

	written := false
	initial := value

	if save != nil {
		clearResourceObjectRefs(&initial)
	}

	err := r.withWorkspaceMutation(
		ctx,
		value.WorkspaceID,
		func(tx *Repository) error {
			_, e := tx.q.ResourceGet(ctx, refsqlc.ResourceGetParams{
				WorkspaceID: value.WorkspaceID,
				Key:         value.Key,
			})
			if e != nil {
				if errors.Is(e, sql.ErrNoRows) {
					return nil
				}

				return e
			}

			rows, e = tx.q.ResourceUpdate(ctx, resourceUpdateParams(initial))
			if e != nil || rows == 0 {
				return e
			}

			if e = tx.q.ResourceMediaVersionRetireActive(
				ctx,
				refsqlc.ResourceMediaVersionRetireActiveParams{
					WorkspaceID: value.WorkspaceID,
					ResourceKey: value.Key,
				},
			); e != nil {
				return e
			}

			e = tx.q.ResourceMediaVersionCreate(
				ctx,
				refsqlc.ResourceMediaVersionCreateParams{
					WorkspaceID:  value.WorkspaceID,
					ResourceKey:  value.Key,
					MediaVersion: value.MediaVersion,
				},
			)

			if e != nil || save == nil {
				return e
			}

			written, e = save(ctx, &value)
			if e != nil {
				return e
			}

			return tx.updateResourceObjectRefs(ctx, value)
		},
	)

	if err != nil || rows == 0 {
		return rows, written, err
	}

	return rows, false, r.bumpReferenceCacheVersions(
		value.WorkspaceID,
		"resource_get",
		"resource_list",
		"resource_item_list",
		referenceCacheGet,
		referenceCacheResolve,
		referenceCacheList,
	)
}

const updateResourceObjectRefs = `
UPDATE reference_resource
SET original_ref = $1, preview_61_ref = $2, preview_128_ref = $3,
    preview_256_ref = $4, preview_512_ref = $5, placeholder_ref = $6
WHERE workspace_id = $7 AND key = $8 AND deleted_at IS NULL
`

func (r *Repository) updateResourceObjectRefs(
	ctx context.Context,
	value Resource,
) error {
	_, err := r.executor.ExecContext(
		ctx,
		updateResourceObjectRefs,
		value.OriginalRef,
		value.Preview61Ref,
		value.Preview128Ref,
		value.Preview256Ref,
		value.Preview512Ref,
		value.PlaceholderRef,
		value.WorkspaceID,
		value.Key,
	)

	return err
}

func clearResourceObjectRefs(value *Resource) {
	value.OriginalRef = ""
	value.Preview61Ref = ""
	value.Preview128Ref = ""
	value.Preview256Ref = ""
	value.Preview512Ref = ""
	value.PlaceholderRef = ""
}

func (r *Repository) GetResource(
	ctx context.Context,
	workspaceID, key string,
) (Resource, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return Resource{}, err
	}

	resource, err := sqlwrap.Query(
		ctx,
		r.db,
		sqlwrap.Params{
			Key: r.referenceCacheKey(
				"resource_get",
				workspaceID,
				key,
			),
			Timeout:           r.timeout,
			CacheVersionScope: referenceCacheScope("resource_get", workspaceID),
			CacheL1Delay:      r.cacheL1,
			CacheL2Delay:      r.cacheL2,
		},
		func(ctx context.Context) (Resource, error) {
			row, e := r.q.ResourceGet(
				ctx,
				refsqlc.ResourceGetParams{WorkspaceID: workspaceID, Key: key},
			)
			if e != nil {
				return Resource{}, e
			}

			return mapResource(row), nil
		},
	)

	return resource, mapNoRows(err)
}

func (r *Repository) ListResources(
	ctx context.Context,
	workspaceID string,
	limit, offset int32,
) ([]Resource, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return nil, err
	}

	rows, e := r.q.ResourceList(
		ctx,
		refsqlc.ResourceListParams{
			WorkspaceID: workspaceID,
			Limit:       limit,
			Offset:      offset,
		},
	)
	if e != nil {
		return nil, e
	}

	out := make([]Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapResource(row))
	}

	return out, nil
}

func (r *Repository) SoftDeleteResource(
	ctx context.Context,
	workspaceID, key string,
) (int64, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return 0, err
	}

	var rows int64

	e := r.withWorkspaceMutation(ctx, workspaceID, func(tx *Repository) error {
		var err error

		rows, err = tx.q.ResourceSoftDelete(
			ctx,
			refsqlc.ResourceSoftDeleteParams{
				WorkspaceID: workspaceID,
				Key:         key,
			},
		)
		if err != nil || rows == 0 {
			return err
		}

		return tx.q.ResourceMediaVersionRetireActive(
			ctx,
			refsqlc.ResourceMediaVersionRetireActiveParams{
				WorkspaceID: workspaceID,
				ResourceKey: key,
			},
		)
	})
	if e != nil {
		return rows, e
	}

	_ = r.bumpReferenceCacheVersions(
		workspaceID,
		"resource_get",
		"resource_list",
		"resource_item_list",
		referenceCacheGet,
		referenceCacheResolve,
		referenceCacheList,
	)

	return rows, nil
}

type GarbageMediaVersion struct {
	WorkspaceID, ResourceKey, MediaVersion string
}

func (r *Repository) ListGarbageMediaVersions(
	ctx context.Context,
	before time.Time,
	limit int32,
) ([]GarbageMediaVersion, error) {
	if limit <= 0 {
		return []GarbageMediaVersion{}, nil
	}

	rows, err := r.q.ResourceListGarbageMediaVersions(
		ctx,
		refsqlc.ResourceListGarbageMediaVersionsParams{
			RetiredAt: sql.NullTime{Time: before, Valid: true},
			Limit:     limit,
		},
	)
	if err != nil {
		return nil, err
	}

	resources := make([]GarbageMediaVersion, 0, len(rows))
	for _, row := range rows {
		resources = append(resources, GarbageMediaVersion{
			WorkspaceID:  row.WorkspaceID,
			ResourceKey:  row.ResourceKey,
			MediaVersion: row.MediaVersion,
		})
	}

	return resources, nil
}

func (r *Repository) DeleteMediaVersion(
	ctx context.Context,
	value GarbageMediaVersion,
	before time.Time,
) (int64, error) {
	if err := requireWorkspace(value.WorkspaceID); err != nil {
		return 0, err
	}

	return r.q.ResourceDeleteMediaVersion(
		ctx,
		refsqlc.ResourceDeleteMediaVersionParams{
			WorkspaceID:  value.WorkspaceID,
			ResourceKey:  value.ResourceKey,
			MediaVersion: value.MediaVersion,
			RetiredAt:    sql.NullTime{Time: before, Valid: true},
		},
	)
}

func (r *Repository) PurgeDeletedResource(
	ctx context.Context,
	workspaceID, key string,
) (int64, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return 0, err
	}

	rows, err := r.q.ResourcePurgeDeleted(
		ctx,
		refsqlc.ResourcePurgeDeletedParams{
			WorkspaceID: workspaceID,
			Key:         key,
		},
	)
	if err != nil || rows == 0 {
		return rows, err
	}

	return rows, r.bumpReferenceCacheVersions(
		workspaceID,
		"resource_get",
		"resource_list",
		"resource_item_list",
		referenceCacheGet,
		referenceCacheResolve,
		referenceCacheList,
	)
}

func (r *Repository) AttachResource(
	ctx context.Context,
	workspaceID, itemKey, resourceKey string,
	position int32,
) error {
	if err := requireWorkspace(workspaceID); err != nil {
		return err
	}

	if position < 0 {
		return ErrResourcePositionInvalid
	}

	rows, err := r.q.ResourceAttach(
		ctx,
		refsqlc.ResourceAttachParams{
			WorkspaceID: workspaceID,
			ItemKey:     itemKey,
			ResourceKey: resourceKey,
			Position:    position,
		},
	)
	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrResourcePositionConflict
		}

		return err
	}

	if rows == 0 {
		return ErrItemNotFound
	}

	return r.bumpReferenceCacheVersions(
		workspaceID,
		"resource_item_list",
		referenceCacheGet,
		referenceCacheResolve,
		referenceCacheList,
	)
}

// InsertResourceAfter attaches an unattached resource after another resource.
// An empty anchor places the resource first.
func (r *Repository) InsertResourceAfter(
	ctx context.Context,
	workspaceID, itemKey, resourceKey, afterResourceKey string,
) error {
	return r.orderResourceAfter(
		ctx, workspaceID, itemKey, resourceKey, afterResourceKey, false,
	)
}

// MoveResourceAfter moves an attached resource after another resource. An empty
// anchor places the resource first.
func (r *Repository) MoveResourceAfter(
	ctx context.Context,
	workspaceID, itemKey, resourceKey, afterResourceKey string,
) error {
	return r.orderResourceAfter(
		ctx, workspaceID, itemKey, resourceKey, afterResourceKey, true,
	)
}

func (r *Repository) orderResourceAfter(
	ctx context.Context,
	workspaceID, itemKey, resourceKey, afterResourceKey string,
	move bool,
) error {
	if err := requireWorkspace(workspaceID); err != nil {
		return err
	}

	changed := false
	err := r.withWorkspaceMutation(
		ctx,
		workspaceID,
		func(tx *Repository) error {
			var exists bool

			if err := tx.executor.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM reference_item
    WHERE workspace_id = $1 AND key = $2 AND deleted_at IS NULL
)`, workspaceID, itemKey).Scan(&exists); err != nil {
				return err
			}

			if !exists {
				return ErrItemNotFound
			}

			if err := tx.executor.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM reference_resource
    WHERE workspace_id = $1 AND key = $2 AND deleted_at IS NULL
)`, workspaceID, resourceKey).Scan(&exists); err != nil {
				return err
			}

			if !exists {
				return ErrResourceNotFound
			}

			rows, err := tx.executor.QueryContext(ctx, `
SELECT resource_key, position
FROM reference_item_resource
WHERE workspace_id = $1 AND item_key = $2
ORDER BY position
FOR UPDATE`, workspaceID, itemKey)
			if err != nil {
				return err
			}
			defer rows.Close()

			type attachment struct {
				key      string
				position int32
			}

			attachments := []attachment{}

			for rows.Next() {
				var attachment attachment

				if err := rows.Scan(
					&attachment.key,
					&attachment.position,
				); err != nil {
					return err
				}

				if attachment.position < 0 {
					return ErrResourcePositionInvalid
				}

				attachments = append(attachments, attachment)
			}

			if err := rows.Err(); err != nil {
				return err
			}

			source := -1
			anchor := -1

			for i, attachment := range attachments {
				if attachment.key == resourceKey {
					source = i
				}

				if afterResourceKey != "" &&
					attachment.key == afterResourceKey {
					anchor = i
				}
			}

			if move && source == -1 {
				return ErrResourceAttachmentNotFound
			}

			if !move && source != -1 {
				return ErrResourceAlreadyAttached
			}

			if afterResourceKey != "" && anchor == -1 {
				return ErrResourceAnchorNotFound
			}

			if move && resourceKey == afterResourceKey {
				return nil
			}

			ordered := make([]string, 0, len(attachments)+1)
			for _, attachment := range attachments {
				if attachment.key != resourceKey {
					ordered = append(ordered, attachment.key)
				}
			}

			insertAt := 0

			if afterResourceKey != "" {
				for i, key := range ordered {
					if key == afterResourceKey {
						insertAt = i + 1
						break
					}
				}
			}

			ordered = append(ordered, "")
			copy(ordered[insertAt+1:], ordered[insertAt:])

			ordered[insertAt] = resourceKey

			maxPosition := int32(0)
			for _, attachment := range attachments {
				if attachment.position > maxPosition {
					maxPosition = attachment.position
				}
			}

			if maxPosition > math.MaxInt32-int32(len(attachments))-1 {
				return fmt.Errorf(
					"%w: temporary position overflow",
					ErrResourcePositionInvalid,
				)
			}

			offset := maxPosition + int32(len(attachments)) + 1
			if _, err := tx.executor.ExecContext(ctx, `
UPDATE reference_item_resource
SET position = position + $1
WHERE workspace_id = $2 AND item_key = $3`, offset, workspaceID, itemKey); err != nil {
				return err
			}

			if !move {
				if _, err := tx.executor.ExecContext(ctx, `
INSERT INTO reference_item_resource (workspace_id, item_key, resource_key, position)
VALUES ($1, $2, $3, $4)`, workspaceID, itemKey, resourceKey, offset+int32(len(attachments))); err != nil {
					return err
				}
			}

			for position, key := range ordered {
				if _, err := tx.executor.ExecContext(ctx, `
UPDATE reference_item_resource
SET position = $1
WHERE workspace_id = $2 AND item_key = $3 AND resource_key = $4`, position, workspaceID, itemKey, key); err != nil {
					return err
				}
			}

			changed = true

			return nil
		},
	)

	if err != nil || !changed {
		return err
	}

	return r.bumpReferenceCacheVersions(
		workspaceID, "resource_item_list", referenceCacheGet,
		referenceCacheResolve, referenceCacheList,
	)
}

func (r *Repository) DetachResource(
	ctx context.Context,
	workspaceID, itemKey, resourceKey string,
) (int64, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return 0, err
	}

	rows, err := r.q.ResourceDetach(ctx, refsqlc.ResourceDetachParams{
		WorkspaceID: workspaceID, ItemKey: itemKey, ResourceKey: resourceKey,
	})
	if err != nil {
		return rows, err
	}

	return rows, r.bumpReferenceCacheVersions(
		workspaceID,
		"resource_item_list",
		referenceCacheGet,
		referenceCacheResolve,
		referenceCacheList,
	)
}

func (r *Repository) ListItemResources(
	ctx context.Context,
	workspaceID, itemKey string,
) ([]Resource, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return nil, err
	}

	return sqlwrap.Query(
		ctx,
		r.db,
		sqlwrap.Params{
			Key: r.referenceCacheKey(
				"resource_item_list",
				workspaceID,
				itemKey,
			),
			Timeout: r.timeout,
			CacheVersionScope: referenceCacheScope(
				"resource_item_list",
				workspaceID,
			),
			CacheL1Delay: r.cacheL1,
			CacheL2Delay: r.cacheL2,
		},
		func(ctx context.Context) ([]Resource, error) {
			rows, err := r.q.ResourceListItemResources(
				ctx,
				refsqlc.ResourceListItemResourcesParams{
					WorkspaceID: workspaceID,
					ItemKey:     itemKey,
				},
			)
			if err != nil {
				return nil, err
			}

			result := make([]Resource, 0, len(rows))
			for _, row := range rows {
				result = append(result, Resource{
					WorkspaceID:    row.WorkspaceID,
					Key:            row.Key,
					Type:           row.ResourceType,
					Payload:        row.Payload,
					IsActive:       row.IsActive,
					DeletedAt:      sqlwrap.NullTimePtr(row.DeletedAt),
					Format:         row.Format,
					ContentType:    row.ContentType,
					SHA256:         row.SourceSha256,
					MediaVersion:   row.MediaVersion,
					Size:           row.SourceSize,
					Width:          int(row.Width),
					Height:         int(row.Height),
					OriginalRef:    row.OriginalRef,
					Preview61Ref:   row.Preview61Ref,
					Preview128Ref:  row.Preview128Ref,
					Preview256Ref:  row.Preview256Ref,
					Preview512Ref:  row.Preview512Ref,
					PlaceholderRef: row.PlaceholderRef,
					CreatedAt:      row.CreatedAt,
					UpdatedAt:      row.UpdatedAt,
				})
			}

			return result, nil
		},
	)
}
func resourceParams(v Resource) refsqlc.ResourceCreateParams {
	return refsqlc.ResourceCreateParams{
		WorkspaceID:    v.WorkspaceID,
		Key:            v.Key,
		ResourceType:   v.Type,
		Payload:        v.Payload,
		IsActive:       v.IsActive,
		Format:         v.Format,
		ContentType:    v.ContentType,
		SourceSize:     v.Size,
		SourceSha256:   v.SHA256,
		MediaVersion:   v.MediaVersion,
		Width:          int32(v.Width),
		Height:         int32(v.Height),
		OriginalRef:    v.OriginalRef,
		Preview61Ref:   v.Preview61Ref,
		Preview128Ref:  v.Preview128Ref,
		Preview256Ref:  v.Preview256Ref,
		Preview512Ref:  v.Preview512Ref,
		PlaceholderRef: v.PlaceholderRef,
	}
}
func resourceUpdateParams(v Resource) refsqlc.ResourceUpdateParams {
	p := resourceParams(v)

	return refsqlc.ResourceUpdateParams{
		ResourceType:   p.ResourceType,
		Payload:        p.Payload,
		IsActive:       p.IsActive,
		Format:         p.Format,
		ContentType:    p.ContentType,
		SourceSize:     p.SourceSize,
		SourceSha256:   p.SourceSha256,
		MediaVersion:   p.MediaVersion,
		Width:          p.Width,
		Height:         p.Height,
		OriginalRef:    p.OriginalRef,
		Preview61Ref:   p.Preview61Ref,
		Preview128Ref:  p.Preview128Ref,
		Preview256Ref:  p.Preview256Ref,
		Preview512Ref:  p.Preview512Ref,
		PlaceholderRef: p.PlaceholderRef,
		WorkspaceID:    p.WorkspaceID,
		Key:            p.Key,
	}
}

var _ = sql.ErrNoRows
