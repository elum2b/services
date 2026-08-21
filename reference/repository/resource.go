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
	Format, ContentType, SHA256                                                            string
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
		func(tx *Repository) error { return tx.q.ResourceCreate(ctx, resourceParams(value)) },
	)
	if err != nil {
		return err
	}

	return r.bumpReferenceCacheVersions(
		value.WorkspaceID,
		"resource_get",
		"resource_list",
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

	return sqlwrap.Query(
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
}

func (r *Repository) ListResources(
	ctx context.Context,
	workspaceID string,
	only bool,
	limit, offset int32,
) ([]Resource, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return nil, err
	}

	rows, e := r.q.ResourceList(
		ctx,
		refsqlc.ResourceListParams{
			WorkspaceID: workspaceID,
			Column2:     only,
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

	rows, e := r.q.ResourceSoftDelete(
		ctx,
		refsqlc.ResourceSoftDeleteParams{WorkspaceID: workspaceID, Key: key},
	)
	if e != nil {
		return rows, e
	}

	_ = r.bumpReferenceCacheVersions(
		workspaceID,
		"resource_get",
		"resource_list",
		referenceCacheGet,
		referenceCacheResolve,
		referenceCacheList,
	)

	return rows, nil
}

func (r *Repository) AttachResource(
	ctx context.Context,
	workspaceID, itemKey, resourceKey string,
	position int32,
) error {
	if err := requireWorkspace(workspaceID); err != nil {
		return err
	}

	if e := r.q.ResourceAttach(
		ctx,
		refsqlc.ResourceAttachParams{
			WorkspaceID: workspaceID,
			ItemKey:     itemKey,
			ResourceKey: resourceKey,
			Position:    position,
		},
	); e != nil {
		return e
	}

	return r.bumpReferenceCacheVersions(
		workspaceID,
		referenceCacheGet,
		referenceCacheResolve,
		referenceCacheList,
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
