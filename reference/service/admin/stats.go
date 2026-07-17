package admin

import "context"

func (a *Admin) GetStats(ctx context.Context, workspaceID string) (StatsModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	value, err := a.repository.GetStats(mergedCtx, workspaceID)
	if err != nil {
		return StatsModel{}, err
	}
	return StatsModel{
		ItemsTotal: value.ItemsTotal, ItemsNotDeleted: value.ItemsNotDeleted,
		ActiveItems: value.ActiveItems, DeletedItems: value.DeletedItems,
		QuantityItems: value.QuantityItems, DurationItems: value.DurationItems,
	}, nil
}
