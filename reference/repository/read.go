package repository

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	refsqlc "github.com/elum2b/services/reference/sqlc"
)

func (r *Repository) Get(ctx context.Context, workspaceID, key, locale string) (Item, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return Item{}, err
	}
	cacheKey := r.referenceCacheKey(referenceCacheGet, workspaceID, key, locale)
	item, err := sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key: cacheKey, Timeout: r.timeout, CacheVersionScope: referenceCacheScope(referenceCacheGet, workspaceID),
		CacheL1Delay: r.cacheL1, CacheL2Delay: r.cacheL2,
	}, func(ctx context.Context) (Item, error) {
		rows, err := r.q.GetItemBundle(ctx, refsqlc.GetItemBundleParams{
			Locale:      locale,
			WorkspaceID: workspaceID,
			Key:         key,
		})
		if err != nil {
			return Item{}, err
		}
		if len(rows) == 0 {
			return Item{}, sql.ErrNoRows
		}
		return mapGetRow(rows[0]), nil
	})
	return item, mapNoRows(err)
}

func (r *Repository) Resolve(ctx context.Context, workspaceID string, keys []string, locale string) ([]Item, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return []Item{}, nil
	}
	cacheKeys := append([]string(nil), keys...)
	sort.Strings(cacheKeys)
	cacheKey := r.referenceCacheKey(referenceCacheResolve, workspaceID, locale, strings.Join(cacheKeys, "\x1f"))
	items, err := sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key: cacheKey, Timeout: r.timeout, CacheVersionScope: referenceCacheScope(referenceCacheResolve, workspaceID),
		CacheL1Delay: r.cacheL1, CacheL2Delay: r.cacheL2,
	}, func(ctx context.Context) ([]Item, error) {
		rows, err := r.q.ResolveItemBundles(ctx, refsqlc.ResolveItemBundlesParams{
			Locale:      locale,
			WorkspaceID: workspaceID,
			Column3:     cacheKeys,
		})
		if err != nil {
			return nil, err
		}
		result := make([]Item, 0, len(rows))
		for _, row := range rows {
			result = append(result, mapResolveRow(row))
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	ordered := make([]Item, 0, len(keys))
	for _, key := range keys {
		index := sort.Search(len(items), func(index int) bool {
			return items[index].Key >= key
		})
		if index < len(items) && items[index].Key == key {
			ordered = append(ordered, items[index])
		}
	}
	return ordered, nil
}

func (r *Repository) List(ctx context.Context, workspaceID, locale string, limit, offset int32) ([]Item, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return nil, err
	}
	cacheKey := r.referenceCacheKey(referenceCacheList, workspaceID, locale, limit, offset)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key: cacheKey, Timeout: r.timeout, CacheVersionScope: referenceCacheScope(referenceCacheList, workspaceID),
		CacheL1Delay: r.cacheL1, CacheL2Delay: r.cacheL2,
	}, func(ctx context.Context) ([]Item, error) {
		rows, err := r.q.ListItemBundles(ctx, refsqlc.ListItemBundlesParams{
			Locale:      locale,
			WorkspaceID: workspaceID,
			Limit:       limit,
			Offset:      offset,
		})
		if err != nil {
			return nil, err
		}
		result := make([]Item, 0, len(rows))
		for _, row := range rows {
			result = append(result, mapListRow(row))
		}
		return result, nil
	})
}

func mapGetRow(row refsqlc.GetItemBundleRow) Item {
	return Item{
		WorkspaceID: row.WorkspaceID,
		Key:         row.Key,
		Type:        row.ItemType,
		Payload:     row.Payload,
		IsActive:    row.IsActive,
		DeletedAt:   sqlwrap.NullTimePtr(row.DeletedAt),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		Localization: nullableLocalization(
			row.WorkspaceID, row.Key, row.Locale, row.Title, row.Description,
		),
	}
}

func mapResolveRow(row refsqlc.ResolveItemBundlesRow) Item {
	return Item{
		WorkspaceID: row.WorkspaceID,
		Key:         row.Key,
		Type:        row.ItemType,
		Payload:     row.Payload,
		IsActive:    row.IsActive,
		DeletedAt:   sqlwrap.NullTimePtr(row.DeletedAt),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		Localization: nullableLocalization(
			row.WorkspaceID, row.Key, row.Locale, row.Title, row.Description,
		),
	}
}

func mapListRow(row refsqlc.ListItemBundlesRow) Item {
	return Item{
		WorkspaceID: row.WorkspaceID,
		Key:         row.Key,
		Type:        row.ItemType,
		Payload:     row.Payload,
		IsActive:    row.IsActive,
		DeletedAt:   sqlwrap.NullTimePtr(row.DeletedAt),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		Localization: nullableLocalization(
			row.WorkspaceID, row.Key, row.Locale, row.Title, row.Description,
		),
	}
}

func nullableLocalization(
	workspaceID, key string,
	locale, title, description sql.NullString,
) *Localization {
	if !locale.Valid {
		return nil
	}
	return &Localization{
		WorkspaceID: workspaceID,
		ItemKey:     key,
		Locale:      locale.String,
		Title:       title.String,
		Description: description.String,
	}
}
