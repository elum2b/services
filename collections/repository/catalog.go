package repository

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/elum2b/services/collections/model"
	cq "github.com/elum2b/services/collections/sqlc"

	services "github.com/elum2b/services"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"

	json "github.com/goccy/go-json"
)

func validateCollection(c model.Collection) error {
	if !validKey(c.Key) || len(c.Localization) > 100 || len(c.Entries) > model.MaxCatalogEntries {
		return ErrInvalid
	}
	for locale, t := range c.Localization {
		if len(locale) > 32 || !validKey(locale) || len(t.Title) > 4096 || len(t.Description) > 65536 || !utf8.ValidString(t.Title) || !utf8.ValidString(t.Description) || strings.ContainsRune(t.Title, 0) || strings.ContainsRune(t.Description, 0) {
			return ErrInvalid
		}
	}
	seen := map[string]bool{}
	for _, e := range c.Entries {
		if !validKey(e.ItemKey) || seen[e.ItemKey] {
			return ErrInvalid
		}
		seen[e.ItemKey] = true
	}
	return nil
}
func saveDefinition(ctx context.Context, q *cq.Queries, w string, c model.Collection, create bool) error {
	if create {
		count, err := q.CreateCollection(ctx, cq.CreateCollectionParams{
			WorkspaceID: w,
			Key:         c.Key,
			Position:    c.Position,
			Active:      c.Active,
			Archived:    c.Archived,
		})
		if err != nil {
			return err
		}
		if count == 0 {
			return ErrConflict
		}
	} else {
		if err := q.SaveCollection(ctx, cq.SaveCollectionParams{
			WorkspaceID: w,
			Key:         c.Key,
			Position:    c.Position,
			Active:      c.Active,
			Archived:    c.Archived,
		}); err != nil {
			return err
		}
	}
	if err := q.ClearLocalizations(ctx, cq.ClearLocalizationsParams{
		WorkspaceID:   w,
		CollectionKey: c.Key,
	}); err != nil {
		return err
	}
	locales := make([]string, 0, len(c.Localization))
	for locale := range c.Localization {
		locales = append(locales, locale)
	}
	sort.Strings(locales)
	for _, locale := range locales {
		t := c.Localization[locale]
		if err := q.PutLocalization(ctx, cq.PutLocalizationParams{
			WorkspaceID:   w,
			CollectionKey: c.Key,
			Locale:        locale,
			Title:         t.Title,
			Description:   t.Description,
		}); err != nil {
			return err
		}
	}
	return nil
}
func (r *Repository) catalogWrite(ctx context.Context, w, actor, method string, payload any, fn func(context.Context, *cq.Queries) error) error {
	if err := services.ValidateWorkspaceID(w); err != nil {
		return err
	}
	if !validKey(actor) {
		return ErrInvalid
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	err = r.transaction(ctx, r.options.QueryTimeout, false, func(ctx context.Context, q *cq.Queries, _ *sql.Tx) error {
		if err := q.LockCatalog(ctx, w); err != nil {
			return err
		}
		if err := fn(ctx, q); err != nil {
			return DomainError(err)
		}
		if err := q.BumpCatalogVersion(ctx, w); err != nil {
			return err
		}
		return q.CatalogAudit(ctx, cq.CatalogAuditParams{
			WorkspaceID: w,
			ActorID:     actor,
			Method:      method,
			Payload:     raw,
		})
	})
	if err == nil {
		r.invalidate(w)
	}
	return err
}
func (r *Repository) CreateCollection(ctx context.Context, p model.SaveCollectionParams) error {
	if err := validateCollection(p.Collection); err != nil {
		return err
	}
	if len(p.Collection.Entries) > 0 {
		return ErrInvalid
	}
	return r.catalogWrite(ctx, p.WorkspaceID, p.ActorID, "create_collection", p, func(ctx context.Context, q *cq.Queries) error {
		return saveDefinition(ctx, q, p.WorkspaceID, p.Collection, true)
	})
}
func (r *Repository) UpdateCollection(ctx context.Context, p model.SaveCollectionParams) error {
	if err := validateCollection(p.Collection); err != nil {
		return err
	}
	if len(p.Collection.Entries) > 0 {
		return ErrInvalid
	}
	return r.catalogWrite(ctx, p.WorkspaceID, p.ActorID, "update_collection", p, func(ctx context.Context, q *cq.Queries) error {
		if _, err := q.GetCollection(ctx, cq.GetCollectionParams{
			WorkspaceID: p.WorkspaceID,
			Key:         p.Collection.Key,
		}); err != nil {
			return err
		}
		return saveDefinition(ctx, q, p.WorkspaceID, p.Collection, false)
	})
}
func (r *Repository) ArchiveCollection(ctx context.Context, p model.ArchiveCollectionParams) error {
	if !validKey(p.Key) {
		return ErrInvalid
	}
	return r.catalogWrite(ctx, p.WorkspaceID, p.ActorID, "archive_collection", p, func(ctx context.Context, q *cq.Queries) error {
		n, err := q.ArchiveCollection(ctx, cq.ArchiveCollectionParams{
			WorkspaceID: p.WorkspaceID,
			Key:         p.Key,
		})
		if err == nil && n == 0 {
			return ErrNotFound
		}
		return err
	})
}
func (r *Repository) AddEntry(ctx context.Context, p model.EntryParams) error {
	if !validKey(p.CollectionKey) || !validKey(p.Entry.ItemKey) {
		return ErrInvalid
	}
	return r.catalogWrite(ctx, p.WorkspaceID, p.ActorID, "add_entry", p, func(ctx context.Context, q *cq.Queries) error {
		if _, err := q.GetCollection(ctx, cq.GetCollectionParams{
			WorkspaceID: p.WorkspaceID,
			Key:         p.CollectionKey,
		}); err != nil {
			return err
		}
		count, err := q.CountOtherEntries(ctx, cq.CountOtherEntriesParams{WorkspaceID: p.WorkspaceID, CollectionKey: p.CollectionKey, ItemKey: p.Entry.ItemKey})
		if err != nil {
			return err
		}
		if count >= model.MaxCatalogEntries {
			return ErrInvalid
		}
		return q.PutEntry(ctx, cq.PutEntryParams{
			WorkspaceID:   p.WorkspaceID,
			CollectionKey: p.CollectionKey,
			ItemKey:       p.Entry.ItemKey,
			Position:      p.Entry.Position,
			Active:        p.Entry.Active,
		})
	})
}
func (r *Repository) RemoveEntry(ctx context.Context, p model.EntryParams) error {
	if !validKey(p.CollectionKey) || !validKey(p.Entry.ItemKey) {
		return ErrInvalid
	}
	return r.catalogWrite(ctx, p.WorkspaceID, p.ActorID, "remove_entry", p, func(ctx context.Context, q *cq.Queries) error {
		n, err := q.RemoveEntry(ctx, cq.RemoveEntryParams{
			WorkspaceID:   p.WorkspaceID,
			CollectionKey: p.CollectionKey,
			ItemKey:       p.Entry.ItemKey,
		})
		if err == nil && n == 0 {
			return ErrNotFound
		}
		return err
	})
}
func (r *Repository) ReorderEntries(ctx context.Context, p model.ReorderEntriesParams) error {
	if !validKey(p.CollectionKey) || len(p.ItemKeys) > model.MaxCatalogEntries {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, k := range p.ItemKeys {
		if !validKey(k) || seen[k] {
			return ErrInvalid
		}
		seen[k] = true
	}
	return r.catalogWrite(ctx, p.WorkspaceID, p.ActorID, "reorder_entries", p, func(ctx context.Context, q *cq.Queries) error {
		if _, err := q.GetCollection(ctx, cq.GetCollectionParams{
			WorkspaceID: p.WorkspaceID,
			Key:         p.CollectionKey,
		}); err != nil {
			return err
		}
		count, err := q.CountEntries(ctx, cq.CountEntriesParams{WorkspaceID: p.WorkspaceID, CollectionKey: p.CollectionKey})
		if err != nil {
			return err
		}
		if count != int64(len(p.ItemKeys)) {
			return ErrConflict
		}
		updated, err := q.ReorderEntries(ctx, cq.ReorderEntriesParams{WorkspaceID: p.WorkspaceID, CollectionKey: p.CollectionKey, ItemKeys: p.ItemKeys})
		if err != nil {
			return err
		}
		if updated != count {
			return ErrConflict
		}
		return nil
	})
}
func loadCatalog(ctx context.Context, q *cq.Queries, w string, limit, offset int32, withEntries bool) ([]model.Collection, error) {
	rows, err := q.ListCollections(ctx, cq.ListCollectionsParams{
		WorkspaceID: w,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]model.Collection, 0, len(rows))
	keys := make([]string, 0, len(rows))
	index := map[string]int{}
	for _, row := range rows {
		index[row.Key] = len(out)
		keys = append(keys, row.Key)
		out = append(out, model.Collection{
			Key:          row.Key,
			Position:     row.Position,
			Active:       row.Active,
			Archived:     row.Archived,
			Localization: map[string]model.Text{},
			Entries:      []model.Entry{},
		})
	}
	if len(keys) == 0 {
		return out, nil
	}
	texts, err := q.ListLocalizations(ctx, cq.ListLocalizationsParams{
		WorkspaceID: w,
		Keys:        keys,
	})
	if err != nil {
		return nil, err
	}
	for _, t := range texts {
		out[index[t.CollectionKey]].Localization[t.Locale] = model.Text{
			Title:       t.Title,
			Description: t.Description,
		}
	}
	if !withEntries {
		return out, nil
	}
	entries, err := q.ListEntries(ctx, cq.ListEntriesParams{
		WorkspaceID: w,
		Keys:        keys,
	})
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		i := index[e.CollectionKey]
		out[i].Entries = append(out[i].Entries, model.Entry{
			ItemKey:  e.ItemKey,
			Position: e.Position,
			Active:   e.Active,
		})
	}
	return out, nil
}
func (r *Repository) ListCatalog(ctx context.Context, p model.CatalogParams) ([]model.Collection, error) {
	if err := services.ValidateWorkspaceID(p.WorkspaceID); err != nil {
		return nil, err
	}
	limit, offset := page(p.Page)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               sqlwrap.CreateKey("collections", "catalog", p.WorkspaceID, limit, offset),
		CacheVersionScope: []any{"collections", "catalog", p.WorkspaceID},
		CacheL1Delay:      r.options.CacheL1Delay,
		CacheL2Delay:      r.options.CacheL2Delay,
	}, func(ctx context.Context) ([]model.Collection, error) {
		var out []model.Collection
		err := r.transaction(ctx, r.options.QueryTimeout, true, func(ctx context.Context, q *cq.Queries, _ *sql.Tx) error {
			var err error
			out, err = loadCatalog(ctx, q, p.WorkspaceID, limit, offset, false)
			return err
		})
		return out, err
	})
}
func (r *Repository) GetCatalogCollection(ctx context.Context, p model.CatalogParams) (model.Collection, error) {
	if err := services.ValidateWorkspaceID(p.WorkspaceID); err != nil {
		return model.Collection{}, err
	}
	if !validKey(p.Key) {
		return model.Collection{}, ErrInvalid
	}
	var out model.Collection
	err := r.transaction(ctx, r.options.QueryTimeout, true, func(ctx context.Context, q *cq.Queries, _ *sql.Tx) error {
		c, err := q.GetCollection(ctx, cq.GetCollectionParams{
			WorkspaceID: p.WorkspaceID,
			Key:         p.Key,
		})
		if err != nil {
			return DomainError(err)
		}
		out = model.Collection{
			Key:          c.Key,
			Position:     c.Position,
			Active:       c.Active,
			Archived:     c.Archived,
			Localization: map[string]model.Text{},
			Entries:      []model.Entry{},
		}
		texts, err := q.ListLocalizations(ctx, cq.ListLocalizationsParams{
			WorkspaceID: p.WorkspaceID,
			Keys:        []string{p.Key},
		})
		if err != nil {
			return err
		}
		for _, t := range texts {
			out.Localization[t.Locale] = model.Text{
				Title:       t.Title,
				Description: t.Description,
			}
		}
		limit, offset := page(p.Page)
		entries, err := q.ListEntryPage(ctx, cq.ListEntryPageParams{WorkspaceID: p.WorkspaceID, CollectionKey: p.Key, Limit: limit, Offset: offset})
		if err != nil {
			return err
		}
		for _, e := range entries {
			out.Entries = append(out.Entries, model.Entry{
				ItemKey:  e.ItemKey,
				Position: e.Position,
				Active:   e.Active,
			})
		}
		return nil
	})
	return out, err
}
