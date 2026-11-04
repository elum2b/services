package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/elum2b/services/collections/model"
	cq "github.com/elum2b/services/collections/sqlc"

	services "github.com/elum2b/services"
	batch "github.com/elum2b/services/internal/utils/importexport"
	"github.com/elum2b/services/internal/utils/importexport/jobs"

	json "github.com/goccy/go-json"
)

func validatePackage(ctx context.Context, p model.ExportPackage) error {
	if p.Format != model.ExportFormat || p.Service != "collections" {
		return ErrInvalid
	}
	seen := map[string]bool{}
	records := len(p.Collections)
	for n, c := range p.Collections {
		if err := validateCollection(c); err != nil {
			return fmt.Errorf("collections[%d]: %w", n, err)
		}
		if seen[c.Key] {
			return fmt.Errorf("collections[%d].key: %w", n, ErrInvalid)
		}
		seen[c.Key] = true
		records += len(c.Entries) + len(c.Localization)
	}
	if records > 100000 {
		return ErrInvalid
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if len(raw) > 32<<20 {
		return ErrInvalid
	}
	if err := jobs.ValidateJSONShape(ctx, bytes.NewReader(raw), 100000); err != nil {
		return fmt.Errorf("manifest: %w", ErrInvalid)
	}
	return nil
}
func (r *Repository) Export(ctx context.Context, w string, _ model.ExportRequest) (model.ExportPackage, error) {
	if err := services.ValidateWorkspaceID(w); err != nil {
		return model.ExportPackage{}, err
	}
	out := model.ExportPackage{
		Format:  model.ExportFormat,
		Service: "collections",
	}
	err := r.transaction(ctx, 5*time.Minute, true, func(ctx context.Context, q *cq.Queries, _ *sql.Tx) error {
		footprint, err := q.ExportFootprint(ctx, w)
		if err != nil {
			return err
		}
		if footprint.Records > 100000 || footprint.Bytes > 32<<20 {
			return ErrInvalid
		}
		out.Collections, err = loadCatalog(ctx, q, w, 100001, 0, true)
		return err
	})
	if err == nil {
		err = validatePackage(ctx, out)
	}
	return out, err
}
func (r *Repository) PreviewImport(ctx context.Context, w string, p model.ExportPackage) (model.ImportPreview, error) {
	if err := services.ValidateWorkspaceID(w); err != nil {
		return model.ImportPreview{}, err
	}
	if err := validatePackage(ctx, p); err != nil {
		return model.ImportPreview{}, err
	}
	keys, err := r.q.ImportKeys(ctx, w)
	if err != nil {
		return model.ImportPreview{}, err
	}
	seen := map[string]bool{}
	for _, key := range keys {
		seen[key] = true
	}
	out := model.ImportPreview{Conflicts: []string{}}
	for _, c := range p.Collections {
		if seen[c.Key] {
			out.Conflicts = append(out.Conflicts, c.Key)
		}
	}
	return out, nil
}
func (r *Repository) Import(ctx context.Context, w string, p model.ImportRequest) (model.ImportResult, error) {
	return r.ImportJob(ctx, w, 0, p)
}
func (r *Repository) ImportJob(ctx context.Context, w string, jobID int64, p model.ImportRequest) (model.ImportResult, error) {
	if !validKey(p.ActorID) {
		return model.ImportResult{}, ErrInvalid
	}
	if err := services.ValidateWorkspaceID(w); err != nil {
		return model.ImportResult{}, err
	}
	if err := validatePackage(ctx, p.Package); err != nil {
		return model.ImportResult{}, err
	}
	packageJSON, err := json.Marshal(p.Package)
	if err != nil {
		return model.ImportResult{}, err
	}
	packageHash := sha256.Sum256(packageJSON)
	strategy := model.ConflictStrategy(p.ConflictStrategy)
	if strategy == "" {
		strategy = model.ConflictUpdate
	}
	if strategy != model.ConflictUpdate && strategy != model.ConflictSkip && strategy != model.ConflictFail {
		return model.ImportResult{}, ErrInvalid
	}
	var out model.ImportResult
	err = r.transaction(ctx, 5*time.Minute, false, func(ctx context.Context, q *cq.Queries, tx *sql.Tx) error {
		if err := q.LockCatalog(ctx, w); err != nil {
			return err
		}
		if jobID != 0 {
			apply, err := jobs.ClaimImportReceipt(ctx, tx, jobID, "collections", w)
			if err != nil || !apply {
				return err
			}
		}
		keys, err := q.ImportKeys(ctx, w)
		if err != nil {
			return err
		}
		existing := map[string]bool{}
		for _, k := range keys {
			existing[k] = true
		}
		var definitions, texts, entries [][]any
		var replaced []string
		for _, c := range p.Package.Collections {
			if existing[c.Key] {
				if strategy == model.ConflictFail {
					return ErrConflict
				}
				if strategy == model.ConflictSkip {
					out.Skipped++
					continue
				}
			}
			replaced = append(replaced, c.Key)
			out.Imported++
			definitions = append(definitions, []any{w, c.Key, c.Position, c.Active, c.Archived})
			for locale, t := range c.Localization {
				texts = append(texts, []any{w, c.Key, locale, t.Title, t.Description})
			}
			for _, e := range c.Entries {
				entries = append(entries, []any{w, c.Key, e.ItemKey, e.Position, e.Active, false})
			}
		}
		if err := importBulk(ctx, tx, "collections_collection", []string{"workspace_id", "key", "position", "active", "archived"}, definitions, "(workspace_id,key) DO UPDATE SET position=EXCLUDED.position,active=EXCLUDED.active,archived=EXCLUDED.archived"); err != nil {
			return err
		}
		if err := q.ClearImportLocalizations(ctx, cq.ClearImportLocalizationsParams{
			WorkspaceID: w,
			Keys:        replaced,
		}); err != nil {
			return err
		}
		if err := q.ClearImportEntries(ctx, cq.ClearImportEntriesParams{
			WorkspaceID: w,
			Keys:        replaced,
		}); err != nil {
			return err
		}
		if err := importBulk(ctx, tx, "collections_localization", []string{"workspace_id", "collection_key", "locale", "title", "description"}, texts, "(workspace_id,collection_key,locale) DO UPDATE SET title=EXCLUDED.title,description=EXCLUDED.description"); err != nil {
			return err
		}
		if err := importBulk(ctx, tx, "collections_entry", []string{"workspace_id", "collection_key", "item_key", "position", "active", "deleted"}, entries, "(workspace_id,collection_key,item_key) DO UPDATE SET position=EXCLUDED.position,active=EXCLUDED.active,deleted=false"); err != nil {
			return err
		}
		metadata, err := json.Marshal(struct {
			JobID       int64              `json:"job_id,omitempty"`
			PackageHash string             `json:"package_hash"`
			Result      model.ImportResult `json:"result"`
		}{JobID: jobID, PackageHash: fmt.Sprintf("%x", packageHash), Result: out})
		if err != nil {
			return err
		}
		if out.Imported > 0 {
			if err := q.BumpCatalogVersion(ctx, w); err != nil {
				return err
			}
		}
		return q.CatalogAudit(ctx, cq.CatalogAuditParams{WorkspaceID: w, ActorID: p.ActorID, Method: "import_catalog", Payload: metadata})
	})
	if err != nil {
		return model.ImportResult{}, err
	}
	r.invalidate(w)
	return out, nil
}

// Names and conflict clauses come only from the fixed call sites above.
func importBulk(ctx context.Context, tx *sql.Tx, table string, columns []string, rows [][]any, conflict string) error {
	return batch.ForEachBatch(len(rows), len(columns), batch.DefaultBatchLimits, func(start, end int) error {
		var sql strings.Builder
		fmt.Fprintf(&sql, "INSERT INTO %s (%s) VALUES ", table, strings.Join(columns, ","))
		args := []any{}
		for n := start; n < end; n++ {
			if n > start {
				sql.WriteByte(',')
			}
			sql.WriteByte('(')
			for col, value := range rows[n] {
				if col > 0 {
					sql.WriteByte(',')
				}
				args = append(args, value)
				fmt.Fprintf(&sql, "$%d", len(args))
			}
			sql.WriteByte(')')
		}
		sql.WriteString(" ON CONFLICT " + conflict)
		_, err := tx.ExecContext(ctx, sql.String(), args...)
		return err
	})
}
