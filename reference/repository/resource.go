package repository

import (
	"context"
	"database/sql"
	"time"

	json "github.com/goccy/go-json"

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
	if err := requireWorkspace(value.WorkspaceID); err != nil {
		return err
	}

	err := r.withWorkspaceMutation(
		ctx,
		value.WorkspaceID,
		func(tx *Repository) error {
			if err := tx.q.ResourceCreate(
				ctx,
				resourceParams(value),
			); err != nil {
				return err
			}

			return tx.q.ResourceMediaVersionCreate(
				ctx,
				refsqlc.ResourceMediaVersionCreateParams{
					WorkspaceID:  value.WorkspaceID,
					ResourceKey:  value.Key,
					MediaVersion: value.MediaVersion,
				},
			)
		},
	)
	if err != nil {
		return err
	}

	return r.bumpReferenceCacheVersions(
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
	if err := requireWorkspace(value.WorkspaceID); err != nil {
		return 0, err
	}

	var rows int64

	err := r.withWorkspaceMutation(
		ctx,
		value.WorkspaceID,
		func(tx *Repository) error {
			var e error

			rows, e = tx.q.ResourceUpdate(ctx, resourceUpdateParams(value))
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

			return e
		},
	)

	if err != nil || rows == 0 {
		return rows, err
	}

	return rows, r.bumpReferenceCacheVersions(
		value.WorkspaceID,
		"resource_get",
		"resource_list",
		"resource_item_list",
		referenceCacheGet,
		referenceCacheResolve,
		referenceCacheList,
	)
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
