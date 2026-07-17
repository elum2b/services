package repository

import (
	"context"
	json "github.com/goccy/go-json"
	"time"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/internal/utils/target"
	tasksqlc "github.com/elum2b/services/tasks/sqlc"
)

func (r *Repository) ListActive(
	ctx context.Context,
	identity Identity,
	locale, groupKey string,
	now time.Time,
) ([]ActiveTask, error) {
	if err := identity.Validate(); err != nil {
		return nil, err
	}

	if now.IsZero() {
		now = time.Now().UTC()
	}
	catalog, err := r.listActiveCatalog(ctx, identity.WorkspaceID, locale, groupKey)
	if err != nil {
		return nil, err
	}
	tasks := make([]ActiveTask, 0, len(catalog))
	for _, task := range catalog {
		if activeTaskVisibleAt(task, now) && activeTaskTargetMatches(task, identity, locale) {
			task.Progress = nil
			task.Target = nil
			tasks = append(tasks, task)
		}
	}
	progressRows, err := repositoryValue[[]tasksqlc.TaskProgress](
		ctx,
		r,
		func(ctx context.Context) ([]tasksqlc.TaskProgress, error) {
			return r.q.ListCurrentProgressForUser(ctx, tasksqlc.ListCurrentProgressForUserParams{
				WorkspaceID: identity.WorkspaceID,
				AppID:       identity.AppID, PlatformID: identity.PlatformID, PlatformUserID: identity.PlatformUserID,
				PeriodStartAt: now, PeriodEndAt: now,
			})
		},
	)
	if err != nil {
		return nil, err
	}
	progressByTask := make(map[uint64]ActiveProgress, len(progressRows))
	for _, row := range progressRows {
		progressByTask[uint64(row.TaskID)] = mapActiveProgress(row)
	}
	for index := range tasks {
		if progress, ok := progressByTask[tasks[index].ID]; ok {
			tasks[index].Progress = &progress
		}
	}
	if err := r.attachComplexConditions(ctx, identity.WorkspaceID, tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *Repository) attachComplexConditions(ctx context.Context, workspaceID string, tasks []ActiveTask) error {
	if len(tasks) == 0 {
		return nil
	}
	taskByID := make(map[uint64]int, len(tasks))
	hasComplex := false
	for index, task := range tasks {
		taskByID[task.ID] = index
		if task.TaskKind == TaskKindComplex {
			hasComplex = true
		}
	}
	if !hasComplex {
		return nil
	}
	key := activeComplexConditionsCacheKey(workspaceID)
	rows, err := repositoryQuery(ctx, r, sqlwrap.Params{
		Key:               key,
		CacheL1Delay:      r.cacheL1Delay,
		CacheL2Delay:      r.cacheL2Delay,
		CacheVersionScope: taskCatalogCacheScope(workspaceID),
	}, func(ctx context.Context) ([]tasksqlc.ListActiveComplexConditionsRow, error) {
		return r.q.ListActiveComplexConditions(ctx, workspaceID)
	})
	if err != nil {
		return err
	}
	for _, row := range rows {
		parentIndex, parentOK := taskByID[uint64(row.ParentTaskID)]
		childIndex, childOK := taskByID[uint64(row.ConditionTaskID)]
		if !parentOK || !childOK || tasks[parentIndex].TaskKind != TaskKindComplex {
			continue
		}
		child := tasks[childIndex]
		child.Conditions = nil
		tasks[parentIndex].Conditions = append(tasks[parentIndex].Conditions, child)
	}
	for index := range tasks {
		if tasks[index].TaskKind == TaskKindComplex && len(tasks[index].Conditions) > 0 {
			tasks[index].TargetCount = uint64(len(tasks[index].Conditions))
		}
	}
	return nil
}

func (r *Repository) listActiveCatalog(
	ctx context.Context,
	workspaceID, locale, groupKey string,
) ([]ActiveTask, error) {
	key := activeCatalogCacheKey(workspaceID, locale, groupKey)
	out, err := repositoryQuery(ctx, r, sqlwrap.Params{
		Key:               key,
		CacheL1Delay:      r.cacheL1Delay,
		CacheL2Delay:      r.cacheL2Delay,
		CacheVersionScope: taskCatalogCacheScope(workspaceID),
	}, func(ctx context.Context) ([]ActiveTask, error) {
		rows, err := r.q.ListActiveTaskBundles(ctx, tasksqlc.ListActiveTaskBundlesParams{
			Locale:      locale,
			Locale_2:    locale,
			WorkspaceID: workspaceID,
			Column4:     groupKey,
			GroupKey:    groupKey,
		})
		if err != nil {
			return nil, err
		}
		return activeTasksFromTasks(mapActiveBundles(rows)), nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func activeTasksFromTasks(tasks []Task) []ActiveTask {
	result := make([]ActiveTask, 0, len(tasks))
	for _, task := range tasks {
		out := ActiveTask{
			ID: task.ID, Key: task.Key, GroupKey: task.GroupKey, TaskKind: task.TaskKind,
			GroupTitle: task.GroupTitle, GroupDesc: task.GroupDesc,
			ActionKey: task.ActionKey, ActionKind: task.ActionKind, ClaimMode: task.ClaimMode, StartMode: task.StartMode,
			TargetCount: task.TargetCount, Payload: task.Payload, ImageURL: task.ImageURL,
			Rewards: task.Rewards, StartAt: task.StartAt, EndAt: task.EndAt, Target: task.Target,
			Conditions: task.Conditions,
		}
		if task.Localization != nil {
			out.Title = task.Localization.Title
			out.Description = task.Localization.Description
		}
		result = append(result, out)
	}
	return result
}

func activeTaskVisibleAt(task ActiveTask, now time.Time) bool {
	return (task.StartAt == nil || !task.StartAt.After(now)) && (task.EndAt == nil || task.EndAt.After(now))
}

func activeTaskTargetMatches(task ActiveTask, identity Identity, locale string) bool {
	return target.Match(task.Target, target.Context{
		IsPremium:  identity.IsPremium,
		Sex:        identity.Sex,
		Country:    identity.Country,
		Locale:     locale,
		Platform:   identity.Platform,
		PlatformID: identity.PlatformID,
	})
}

type CallbackPayload struct {
	WorkspaceID    string          `json:"workspace_id"`
	AppID          int64           `json:"app_id"`
	PlatformID     int64           `json:"platform_id"`
	PlatformUserID string          `json:"platform_user_id"`
	TaskID         uint64          `json:"task_id"`
	TaskKey        string          `json:"task_key"`
	OperationID    string          `json:"operation_id"`
	PeriodStartAt  time.Time       `json:"period_start_at"`
	PeriodEndAt    time.Time       `json:"period_end_at"`
	Rewards        []Reward        `json:"rewards"`
	Payload        json.RawMessage `json:"payload"`
}
