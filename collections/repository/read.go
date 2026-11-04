package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"github.com/elum2b/services/collections/model"
	cq "github.com/elum2b/services/collections/sqlc"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"

	json "github.com/goccy/go-json"
)

func hasItems(ctx context.Context, q *cq.Queries, p model.HasItemsParams) ([]model.ItemState, error) {
	i, err := Subject(p.ReadParams)
	if err != nil {
		return nil, err
	}
	if len(p.Items) == 0 || len(p.Items) > model.MaxBatch {
		return nil, ErrInvalid
	}
	keys, items := []string{}, []string{}
	seen := map[model.ItemRef]bool{}
	for _, ref := range p.Items {
		if !validKey(ref.CollectionKey) || !validKey(ref.ItemKey) || seen[ref] {
			return nil, ErrInvalid
		}
		seen[ref] = true
		keys = append(keys, ref.CollectionKey)
		items = append(items, ref.ItemKey)
	}
	rows, err := q.HasItems(ctx, cq.HasItemsParams{
		WorkspaceID:    i.WorkspaceID,
		AppID:          i.AppID,
		PlatformID:     i.PlatformID,
		PlatformUserID: i.PlatformUserID,
		CollectionKeys: keys,
		ItemKeys:       items,
	})
	if err != nil {
		return nil, err
	}
	states := map[model.ItemRef]model.ItemState{}
	for _, row := range rows {
		state := model.ItemState{
			ItemRef: model.ItemRef{
				CollectionKey: row.CollectionKey,
				ItemKey:       row.ItemKey,
			},
			Owned:    row.Owned,
			Revision: row.Revision,
		}
		if row.ChangedAt.Valid {
			t := row.ChangedAt.Time
			state.ChangedAt = &t
		}
		states[state.ItemRef] = state
	}
	out := make([]model.ItemState, 0, len(p.Items))
	for _, ref := range p.Items {
		out = append(out, states[ref])
	}
	return out, nil
}
func (r *Repository) HasItems(ctx context.Context, p model.HasItemsParams) ([]model.ItemState, error) {
	return hasItems(ctx, r.q, p)
}
func (r *Repository) HasItem(ctx context.Context, p model.HasItemParams) (model.ItemState, error) {
	out, err := r.HasItems(ctx, model.HasItemsParams{
		ReadParams: p.ReadParams,
		Items:      []model.ItemRef{p.ItemRef},
	})
	if err != nil {
		return model.ItemState{}, err
	}
	return out[0], nil
}
func (r *Repository) ListOwned(ctx context.Context, p model.ListOwnedParams) ([]model.ItemState, error) {
	i, err := Subject(p.ReadParams)
	if err != nil {
		return nil, err
	}
	if len(p.CollectionKeys) > model.MaxBatch {
		return nil, ErrInvalid
	}
	for _, key := range p.CollectionKeys {
		if !validKey(key) {
			return nil, ErrInvalid
		}
	}
	limit, offset := page(p.Page)
	rows, err := r.q.ListOwned(ctx, cq.ListOwnedParams{
		WorkspaceID:    i.WorkspaceID,
		AppID:          i.AppID,
		PlatformID:     i.PlatformID,
		PlatformUserID: i.PlatformUserID,
		Keys:           p.CollectionKeys,
		Limit:          limit,
		Offset:         offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]model.ItemState, 0, len(rows))
	for _, row := range rows {
		t := row.ChangedAt
		out = append(out, model.ItemState{
			ItemRef: model.ItemRef{
				CollectionKey: row.CollectionKey,
				ItemKey:       row.ItemKey,
			},
			Owned:     true,
			Revision:  row.Revision,
			ChangedAt: &t,
		})
	}
	return out, nil
}
func (r *Repository) ListCollections(ctx context.Context, p model.ReadParams) ([]model.CollectionSummary, error) {
	i, err := Subject(p)
	if err != nil {
		return nil, err
	}
	limit, offset := page(p.Page)
	var out []model.CollectionSummary
	err = r.transaction(ctx, r.options.QueryTimeout, true, func(ctx context.Context, q *cq.Queries, _ *sql.Tx) error {
		rows, err := q.ListPublishedCollections(ctx, cq.ListPublishedCollectionsParams{
			WorkspaceID: i.WorkspaceID,
			Limit:       limit,
			Offset:      offset,
		})
		if err != nil {
			return err
		}
		keys := []string{}
		for _, c := range rows {
			keys = append(keys, c.Key)
		}
		texts, err := q.ListUserLocalizations(ctx, cq.ListUserLocalizationsParams{
			WorkspaceID: i.WorkspaceID,
			Keys:        keys,
			Locale:      p.Locale,
		})
		if err != nil {
			return err
		}
		localized := map[string]model.Text{}
		for _, t := range texts {
			if t.Locale == p.Locale {
				localized[t.CollectionKey] = model.Text{
					Title:       t.Title,
					Description: t.Description,
				}
			}
		}
		out = make([]model.CollectionSummary, 0, len(rows))
		for _, c := range rows {
			out = append(out, model.CollectionSummary{
				Key:      c.Key,
				Position: c.Position,
				Text:     localized[c.Key],
			})
		}
		return nil
	})
	return out, err
}

// catalogVersion binds the persistent revision to the exact resource and page.
func catalogVersion(w string, p model.GetCollectionParams, limit, offset int32, revision int64) string {

	raw, _ := json.Marshal([]any{w, p.CollectionKey, p.Locale, limit, offset, revision})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])

}
func (r *Repository) GetCollection(ctx context.Context, p model.GetCollectionParams) (model.CollectionView, error) {

	i, err := Subject(p.ReadParams)
	if err != nil {
		return model.CollectionView{}, err
	}
	if !validKey(p.CollectionKey) {
		return model.CollectionView{}, ErrInvalid
	}
	if p.KnownVersion != "" {
		if len(p.KnownVersion) != 64 {
			return model.CollectionView{}, ErrInvalid
		}
		if _, err := hex.DecodeString(p.KnownVersion); err != nil {
			return model.CollectionView{}, ErrInvalid
		}
	}

	limit, offset := page(p.Page)
	// Check publication together with the durable revision, even for conditional reads.
	header, err := r.q.GetPublishedCollection(ctx, cq.GetPublishedCollectionParams{
		WorkspaceID: i.WorkspaceID,
		Key:         p.CollectionKey,
	})
	if err != nil {
		return model.CollectionView{}, DomainError(err)
	}
	version := catalogVersion(i.WorkspaceID, p, limit, offset, header.Revision)
	if p.KnownVersion == version {
		return model.CollectionView{Key: p.CollectionKey, Version: version, NotModified: true}, nil
	}

	out, err := sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:          sqlwrap.CreateKey("collections", "published_collection", version),
		CacheL1Delay: r.options.CatalogCacheTTL,
		CacheL2Delay: r.options.CatalogCacheTTL,
	}, func(ctx context.Context) (model.CollectionView, error) {
		var out model.CollectionView
		err := r.transaction(ctx, r.options.QueryTimeout, true, func(ctx context.Context, q *cq.Queries, _ *sql.Tx) error {
			c, err := q.GetPublishedCollection(ctx, cq.GetPublishedCollectionParams{
				WorkspaceID: i.WorkspaceID,
				Key:         p.CollectionKey,
			})
			if err != nil {
				return DomainError(err)
			}
			out = model.CollectionView{
				Key:      c.Key,
				Position: c.Position,
				Version:  catalogVersion(i.WorkspaceID, p, limit, offset, c.Revision),
				Entries:  []model.Entry{},
			}
			texts, err := q.ListUserLocalizations(ctx, cq.ListUserLocalizationsParams{
				WorkspaceID: i.WorkspaceID,
				Keys:        []string{c.Key},
				Locale:      p.Locale,
			})
			if err != nil {
				return err
			}
			for _, t := range texts {
				out.Text = model.Text{Title: t.Title, Description: t.Description}
			}
			entries, err := q.ListPublishedEntries(ctx, cq.ListPublishedEntriesParams{
				WorkspaceID:   i.WorkspaceID,
				CollectionKey: c.Key,
				Limit:         limit,
				Offset:        offset,
			})
			if err != nil {
				return err
			}
			for _, e := range entries {
				out.Entries = append(out.Entries, model.Entry{
					ItemKey:  e.ItemKey,
					Position: e.Position,
					Active:   true,
				})
			}
			return nil
		})
		return out, err
	})
	if err != nil {
		return model.CollectionView{}, err
	}

	// Cache values may be shared Go slices: detach the public response from L1.
	out.Entries = append([]model.Entry(nil), out.Entries...)
	if p.KnownVersion == out.Version {
		return model.CollectionView{Key: out.Key, Version: out.Version, NotModified: true}, nil
	}
	return out, nil

}
