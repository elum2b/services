package repository

import (
	"bytes"
	"context"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
	"time"

	serviceerrors "github.com/elum2b/services/errors"
	json "github.com/goccy/go-json"
)

const (
	DefaultArchiveTimeout  = 5 * time.Minute
	DefaultArchiveMaxRows  = int64(100000)
	DefaultArchiveMaxBytes = int64(32 << 20)
)

var ErrArchiveBudget = serviceerrors.New(serviceerrors.CodeInvalidFields, "cpa archive exceeds configured size or record limit")

func (r *Repository) archiveRepository() *Repository {
	copy := *r
	if copy.archiveTimeout <= 0 {
		copy.archiveTimeout = DefaultArchiveTimeout
	}
	if copy.archiveMaxRows <= 0 {
		copy.archiveMaxRows = DefaultArchiveMaxRows
	}
	if copy.archiveMaxBytes <= 0 {
		copy.archiveMaxBytes = DefaultArchiveMaxBytes
	}
	copy.timeout = copy.archiveTimeout
	return &copy
}

func (r *Repository) validateArchiveBudget(ctx context.Context, pkg ExportPackage) error {
	count := int64(len(pkg.Offers) + len(pkg.Codes) + len(pkg.Assignments))
	for _, offer := range pkg.Offers {
		count += int64(len(offer.Localization) + len(offer.Rewards))
	}
	for _, a := range pkg.Assignments {
		count += int64(len(a.Events))
	}
	if count > r.archiveMaxRows {
		return ErrArchiveBudget
	}
	raw, err := json.Marshal(pkg)
	if err != nil {
		return err
	}
	if int64(len(raw)) > r.archiveMaxBytes {
		return ErrArchiveBudget
	}
	if err := jobs.ValidateJSONShape(ctx, bytes.NewReader(raw), r.archiveMaxRows); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrArchiveBudget
	}
	return nil
}
