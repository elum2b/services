package repository

import (
	"context"
	"database/sql"
	"time"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	tasksqlc "github.com/elum2b/services/tasks/sqlc"
)

type nextSequenceTask struct {
	ID     uint64
	Exists bool
}

func (r *Repository) listRecordCatalog(ctx context.Context, workspaceID, actionKey string) ([]Task, error) {
	key := recordCatalogCacheKey(workspaceID, actionKey)
	out, err := repositoryQuery[[]Task](ctx, r, sqlwrap.Params{
		Key:               key,
		CacheL1Delay:      r.cacheL1Delay,
		CacheL2Delay:      r.cacheL2Delay,
		CacheVersionScope: taskCatalogCacheScope(workspaceID),
	}, func(ctx context.Context) ([]Task, error) {
		rows, err := r.q.ListRecordCatalog(ctx, tasksqlc.ListRecordCatalogParams{
			WorkspaceID: workspaceID,
			ActionKey:   actionKey,
		})
		if err != nil {
			return nil, err
		}
		tasks := make([]Task, 0, len(rows))
		for _, row := range rows {
			tasks = append(tasks, mapRecordCatalogTask(row))
		}
		return tasks, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) claimCatalogByID(ctx context.Context, workspaceID string, id uint64) (Task, error) {
	key := claimCatalogByIDCacheKey(workspaceID, id)
	out, err := repositoryQuery[Task](ctx, r, sqlwrap.Params{
		Key:               key,
		CacheL1Delay:      r.cacheL1Delay,
		CacheL2Delay:      r.cacheL2Delay,
		CacheVersionScope: taskCatalogCacheScope(workspaceID),
	}, func(ctx context.Context) (Task, error) {
		rows, err := r.q.GetClaimCatalogByID(ctx, tasksqlc.GetClaimCatalogByIDParams{
			WorkspaceID: workspaceID,
			ID:          int64(id),
		})
		if err != nil {
			return Task{}, err
		}
		if len(rows) == 0 {
			return Task{}, sql.ErrNoRows
		}
		return mapClaimCatalogTaskByID(rows), nil
	})
	if err != nil {
		return Task{}, err
	}
	return out, nil
}

func (r *Repository) claimCatalogByKey(ctx context.Context, workspaceID, taskKey string) (Task, error) {
	key := claimCatalogByKeyCacheKey(workspaceID, taskKey)
	out, err := repositoryQuery[Task](ctx, r, sqlwrap.Params{
		Key:               key,
		CacheL1Delay:      r.cacheL1Delay,
		CacheL2Delay:      r.cacheL2Delay,
		CacheVersionScope: taskCatalogCacheScope(workspaceID),
	}, func(ctx context.Context) (Task, error) {
		rows, err := r.q.GetClaimCatalogByKey(ctx, tasksqlc.GetClaimCatalogByKeyParams{
			WorkspaceID: workspaceID,
			Key:         taskKey,
		})
		if err != nil {
			return Task{}, err
		}
		if len(rows) == 0 {
			return Task{}, sql.ErrNoRows
		}
		return mapClaimCatalogTaskByKey(rows), nil
	})
	if err != nil {
		return Task{}, err
	}
	return out, nil
}

func (r *Repository) IntegrationCheckTask(
	ctx context.Context,
	workspaceID string,
	taskRefValue string,
) (Task, bool, error) {
	id, keyValue := taskRef(taskRefValue)
	if id != 0 {
		key := integrationCheckTaskByIDCacheKey(workspaceID, id)
		out, err := repositoryQuery[Task](ctx, r, sqlwrap.Params{
			Key:               key,
			CacheL1Delay:      r.cacheL1Delay,
			CacheL2Delay:      r.cacheL2Delay,
			CacheVersionScope: taskCatalogCacheScope(workspaceID),
		}, func(ctx context.Context) (Task, error) {
			row, err := r.q.GetIntegrationCheckTaskByID(ctx, tasksqlc.GetIntegrationCheckTaskByIDParams{
				WorkspaceID: workspaceID,
				ID:          int64(id),
			})
			if err != nil {
				return Task{}, err
			}
			return mapIntegrationCheckTaskByID(row), nil
		})
		if err != nil {
			if isNoRows(err) {
				return Task{}, false, nil
			}
			return Task{}, false, err
		}
		return out, true, nil
	}
	key := integrationCheckTaskByKeyCacheKey(workspaceID, keyValue)
	out, err := repositoryQuery[Task](ctx, r, sqlwrap.Params{
		Key:               key,
		CacheL1Delay:      r.cacheL1Delay,
		CacheL2Delay:      r.cacheL2Delay,
		CacheVersionScope: taskCatalogCacheScope(workspaceID),
	}, func(ctx context.Context) (Task, error) {
		row, err := r.q.GetIntegrationCheckTaskByKey(ctx, tasksqlc.GetIntegrationCheckTaskByKeyParams{
			WorkspaceID: workspaceID,
			Key:         keyValue,
		})
		if err != nil {
			return Task{}, err
		}
		return mapIntegrationCheckTaskByKey(row), nil
	})
	if err != nil {
		if isNoRows(err) {
			return Task{}, false, nil
		}
		return Task{}, false, err
	}
	return out, true, nil
}

func (r *Repository) rewardsCatalog(ctx context.Context, workspaceID string, taskID uint64) ([]Reward, error) {
	key := rewardsCatalogCacheKey(workspaceID, taskID)
	out, err := repositoryQuery[[]Reward](ctx, r, sqlwrap.Params{
		Key:               key,
		CacheL1Delay:      r.cacheL1Delay,
		CacheL2Delay:      r.cacheL2Delay,
		CacheVersionScope: taskCatalogCacheScope(workspaceID),
	}, func(ctx context.Context) ([]Reward, error) {
		rows, err := r.q.ListRewardsCatalog(ctx, tasksqlc.ListRewardsCatalogParams{
			WorkspaceID: workspaceID,
			TaskID:      int64(taskID),
		})
		if err != nil {
			return nil, err
		}
		rewards := make([]Reward, 0, len(rows))
		for _, row := range rows {
			rewards = append(rewards, Reward{
				Key:      row.RewardKey,
				Type:     string(row.RewardType),
				Quantity: row.Quantity,
				Scale:    uint16(row.Scale),
				Unit:     taskDurationUnitPtr(row.DurationUnit),
			})
		}
		return rewards, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) nextSequenceTask(
	ctx context.Context,
	workspaceID, sequenceKey string,
	sequencePosition uint32,
) (nextSequenceTask, error) {
	key := nextSequenceTaskCacheKey(workspaceID, sequenceKey, sequencePosition)
	out, err := repositoryQuery(ctx, r, sqlwrap.Params{
		Key:               key,
		CacheL1Delay:      r.cacheL1Delay,
		CacheL2Delay:      r.cacheL2Delay,
		CacheVersionScope: taskCatalogCacheScope(workspaceID),
	}, func(ctx context.Context) (nextSequenceTask, error) {
		id, err := r.q.GetNextSequenceTaskID(ctx, tasksqlc.GetNextSequenceTaskIDParams{
			WorkspaceID:      workspaceID,
			SequenceKey:      sql.NullString{String: sequenceKey, Valid: true},
			SequencePosition: sql.NullInt32{Int32: int32(sequencePosition), Valid: true},
		})
		if err != nil {
			if isNoRows(err) {
				return nextSequenceTask{}, nil
			}
			return nextSequenceTask{}, err
		}
		return nextSequenceTask{ID: uint64(id), Exists: true}, nil
	})
	if err != nil {
		return nextSequenceTask{}, err
	}
	return out, nil
}

func taskVisibleAt(task Task, now time.Time) bool {
	return (task.StartAt == nil || !task.StartAt.After(now)) && (task.EndAt == nil || task.EndAt.After(now))
}
