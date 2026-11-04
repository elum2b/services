package admin

import (
	"context"

	"github.com/elum2b/services/collections/model"
)

func (s *Admin) GetStats(ctx context.Context, p model.StatsParams) (model.Stats, error) {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.GetStats(ctx, p)
}

func (s *Admin) RefreshDailyStats(ctx context.Context, p model.DailyParams) error {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.RefreshDailyStats(ctx, p)
}

func (s *Admin) ListDailyStats(ctx context.Context, p model.DailyParams) ([]model.DailyStats, error) {
	ctx, cancel := s.withContext(ctx)
	defer cancel()
	return s.repository.ListDailyStats(ctx, p)
}
