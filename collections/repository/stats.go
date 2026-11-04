package repository

import (
	"context"
	"time"

	"github.com/elum2b/services/collections/model"
	cq "github.com/elum2b/services/collections/sqlc"

	services "github.com/elum2b/services"

	json "github.com/goccy/go-json"
)

func (r *Repository) ListJournal(ctx context.Context, p model.JournalParams) ([]model.JournalEntry, error) {
	if err := services.ValidateWorkspaceID(p.WorkspaceID); err != nil {
		return nil, err
	}
	limit, offset := page(p.Page)
	args := cq.ListJournalParams{
		WorkspaceID: p.WorkspaceID,
		Limit:       limit,
		Offset:      offset,
	}
	if p.Identity != nil {
		if err := validateIdentity(*p.Identity); err != nil {
			return nil, err
		}
		if p.Identity.WorkspaceID != p.WorkspaceID {
			return nil, ErrForbidden
		}
		args.AppID = p.Identity.AppID
		args.PlatformID = p.Identity.PlatformID
		args.PlatformUserID = p.Identity.PlatformUserID
	}
	rows, err := r.q.ListJournal(ctx, args)
	if err != nil {
		return nil, err
	}
	out := make([]model.JournalEntry, 0, len(rows))
	for _, row := range rows {
		var receipt model.Receipt
		if err := json.Unmarshal(row.Result, &receipt); err != nil {
			return nil, err
		}
		out = append(out, model.JournalEntry{
			Receipt: receipt,
			Identity: services.Identity{
				WorkspaceID:    row.WorkspaceID,
				AppID:          row.AppID,
				PlatformID:     row.PlatformID,
				PlatformUserID: row.PlatformUserID,
			},
			ActorID: row.ActorID,
			Reason:  row.Reason,
		})
	}
	return out, nil
}
func (r *Repository) GetStats(ctx context.Context, p model.StatsParams) (model.Stats, error) {
	if err := services.ValidateWorkspaceID(p.WorkspaceID); err != nil {
		return model.Stats{}, err
	}
	if p.CollectionKey != "" && !validKey(p.CollectionKey) {
		return model.Stats{}, ErrInvalid
	}
	row, err := r.q.GetStats(ctx, cq.GetStatsParams{
		WorkspaceID:   p.WorkspaceID,
		CollectionKey: p.CollectionKey,
	})
	return model.Stats{
		StoredOwned:    row.StoredOwned,
		AvailableOwned: row.AvailableOwned,
		Grants:         row.Grants,
		Revokes:        row.Revokes,
	}, err
}
func validateRange(p model.DailyParams) error {
	if err := services.ValidateWorkspaceID(p.WorkspaceID); err != nil {
		return err
	}
	if p.From.IsZero() || p.Until.IsZero() || p.From.After(p.Until) {
		return ErrInvalid
	}
	return nil
}
func (r *Repository) RefreshDailyStats(ctx context.Context, p model.DailyParams) error {
	if err := validateRange(p); err != nil {
		return err
	}
	from := p.From.UTC().Truncate(24 * time.Hour)
	until := p.Until.UTC().Truncate(24 * time.Hour)
	if p.Until.After(until) || !until.After(from) {
		until = until.Add(24 * time.Hour)
	}
	return r.q.RefreshDailyStats(ctx, cq.RefreshDailyStatsParams{
		WorkspaceID:  p.WorkspaceID,
		OccurredAt:   from,
		OccurredAt_2: until,
	})
}
func (r *Repository) ListDailyStats(ctx context.Context, p model.DailyParams) ([]model.DailyStats, error) {
	if err := validateRange(p); err != nil {
		return nil, err
	}
	limit, offset := page(p.Page)
	rows, err := r.q.ListDailyStats(ctx, cq.ListDailyStatsParams{
		WorkspaceID: p.WorkspaceID,
		StatsDate:   p.From,
		StatsDate_2: p.Until,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]model.DailyStats, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.DailyStats{
			Date:    row.StatsDate,
			Grants:  row.Grants,
			Revokes: row.Revokes,
		})
	}
	return out, nil
}
