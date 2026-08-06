package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	tasksqlc "github.com/elum2b/services/tasks/sqlc"
)

func (r *Repository) UpsertGroup(ctx context.Context, workspaceID, key string, position int32, active bool) error {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("tasks group workspace_id and key are required")
	}

	if err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		return txRepo.q.AdminUpsertGroup(ctx, tasksqlc.AdminUpsertGroupParams{
			WorkspaceID: workspaceID,
			Key:         key,
			Position:    position,
			IsActive:    active,
		})
	}); err != nil {
		return err
	}

	return r.invalidateTaskCache(ctx, workspaceID)
}

func (r *Repository) UpsertGroupLocalization(
	ctx context.Context,
	workspaceID, key, locale, title, description string,
) error {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" ||
		strings.TrimSpace(locale) == "" || strings.TrimSpace(title) == "" {
		return fmt.Errorf("tasks group localization scope, locale, and title are required")
	}

	if err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		return txRepo.q.AdminUpsertGroupLocalization(ctx, tasksqlc.AdminUpsertGroupLocalizationParams{
			WorkspaceID: workspaceID,
			GroupKey:    key,
			Locale:      locale,
			Title:       title,
			Description: description,
		})
	}); err != nil {
		return err
	}

	return r.invalidateTaskCache(ctx, workspaceID)
}

func (r *Repository) UpsertSequence(ctx context.Context, workspaceID, key string, position int32, active bool) error {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("tasks sequence workspace_id and key are required")
	}

	if err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		return txRepo.q.AdminUpsertSequence(ctx, tasksqlc.AdminUpsertSequenceParams{
			WorkspaceID: workspaceID,
			Key:         key,
			Position:    position,
			IsActive:    active,
		})
	}); err != nil {
		return err
	}

	return r.invalidateTaskCache(ctx, workspaceID)
}

func (r *Repository) GetGroup(ctx context.Context, workspaceID, key string) (Group, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return Group{}, err
	}
	row, err := r.q.AdminGetGroup(ctx, tasksqlc.AdminGetGroupParams{WorkspaceID: workspaceID, Key: key})
	if err != nil {
		return Group{}, err
	}
	return mapGroup(row), nil
}

func (r *Repository) ListGroups(ctx context.Context, workspaceID string) ([]Group, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}
	rows, err := r.q.AdminListGroups(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make([]Group, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapGroup(row))
	}
	return result, nil
}

func (r *Repository) DeleteGroup(ctx context.Context, workspaceID, key string) (int64, error) {
	var rows int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(tx *Repository) error {
		var err error
		rows, err = tx.q.AdminSoftDeleteGroup(ctx, tasksqlc.AdminSoftDeleteGroupParams{WorkspaceID: workspaceID, Key: key})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}
	return rows, r.invalidateTaskCache(ctx, workspaceID)
}

func (r *Repository) GetSequence(ctx context.Context, workspaceID, key string) (Sequence, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return Sequence{}, err
	}
	row, err := r.q.AdminGetSequence(ctx, tasksqlc.AdminGetSequenceParams{WorkspaceID: workspaceID, Key: key})
	if err != nil {
		return Sequence{}, err
	}
	return mapGroup(tasksqlc.TaskGroup(row)), nil
}

func (r *Repository) ListSequences(ctx context.Context, workspaceID string) ([]Sequence, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}
	rows, err := r.q.AdminListSequences(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make([]Sequence, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapGroup(tasksqlc.TaskGroup(row)))
	}
	return result, nil
}

func (r *Repository) DeleteSequence(ctx context.Context, workspaceID, key string) (int64, error) {
	var rows int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(tx *Repository) error {
		var err error
		rows, err = tx.q.AdminSoftDeleteSequence(ctx, tasksqlc.AdminSoftDeleteSequenceParams{WorkspaceID: workspaceID, Key: key})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}
	return rows, r.invalidateTaskCache(ctx, workspaceID)
}

func mapGroup(row tasksqlc.TaskGroup) Group {
	return Group{WorkspaceID: row.WorkspaceID, Key: row.Key, Position: row.Position, IsActive: row.IsActive, DeletedAt: sqlwrap.NullTimePtr(row.DeletedAt)}
}

func (r *Repository) SaveTask(ctx context.Context, params SaveTaskParams) (uint64, error) {
	params = normalizeSaveTaskParams(params)
	if err := validateSaveTask(params); err != nil {
		return 0, err
	}
	if params.ID == 0 {
		var id int64
		err := r.withWorkspaceMutation(ctx, params.WorkspaceID, func(txRepo *Repository) error {
			var err error
			id, err = txRepo.q.AdminCreateTask(ctx, tasksqlc.AdminCreateTaskParams{
				WorkspaceID: params.WorkspaceID, Key: params.Key, GroupKey: params.GroupKey,
				SequenceKey: nullString(
					params.SequenceKey,
				), SequencePosition: nullInt32FromUint32(params.SequencePosition),
				TaskKind:  params.TaskKind,
				ActionKey: params.ActionKey, ActionKind: params.ActionKind,
				ClaimMode: params.ClaimMode, StartMode: params.StartMode, TargetCount: int64(params.TargetCount),
				ResetUnit: params.ResetUnit, ResetEvery: int32(params.ResetEvery),
				Position: params.Position, Payload: rawMessageParam(params.Payload), Target: rawMessageParam(params.Target), IntegrationKind: nullString(params.IntegrationKind),
				IntegrationProvider: nullString(
					params.IntegrationProvider,
				), IntegrationPayload: rawMessageParam(params.IntegrationPayload),
				ImageUrl:  nullString(params.ImageURL),
				IsVisible: params.IsVisible, IsActive: params.IsActive,
				StartAt: nullTime(params.StartAt), EndAt: nullTime(params.EndAt),
			})
			return err
		})
		if err != nil {
			return 0, err
		}

		return uint64(id), r.invalidateTaskCache(ctx, params.WorkspaceID)
	}

	err := r.withWorkspaceMutation(ctx, params.WorkspaceID, func(txRepo *Repository) error {
		_, err := txRepo.q.AdminUpdateTask(ctx, tasksqlc.AdminUpdateTaskParams{
			GroupKey: params.GroupKey, SequenceKey: nullString(params.SequenceKey),
			SequencePosition: nullInt32FromUint32(
				params.SequencePosition,
			), TaskKind: params.TaskKind, ActionKey: params.ActionKey,
			ActionKind: params.ActionKind,
			ClaimMode:  params.ClaimMode, StartMode: params.StartMode, TargetCount: int64(params.TargetCount),
			ResetUnit: params.ResetUnit, ResetEvery: int32(params.ResetEvery),
			Position: params.Position, Payload: rawMessageParam(params.Payload), Target: rawMessageParam(params.Target), IntegrationKind: nullString(params.IntegrationKind),
			IntegrationProvider: nullString(
				params.IntegrationProvider,
			), IntegrationPayload: rawMessageParam(params.IntegrationPayload),
			ImageUrl:  nullString(params.ImageURL),
			IsVisible: params.IsVisible, IsActive: params.IsActive,
			StartAt: nullTime(params.StartAt), EndAt: nullTime(params.EndAt),
			WorkspaceID: params.WorkspaceID, ID: int64(params.ID),
		})
		return err
	})
	if err != nil {
		return 0, err
	}

	return params.ID, r.invalidateTaskCache(ctx, params.WorkspaceID)
}

func (r *Repository) DeleteTask(ctx context.Context, workspaceID string, id uint64) (int64, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return 0, err
	}
	if id == 0 || id > math.MaxInt64 {
		return 0, fmt.Errorf("tasks delete task scope or id is invalid")
	}

	var rows int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		var err error
		rows, err = txRepo.q.AdminDeleteTask(ctx, tasksqlc.AdminDeleteTaskParams{
			WorkspaceID: workspaceID,
			ID:          int64(id),
		})
		return err
	})
	if err != nil {
		return 0, err
	}
	if rows > 0 {
		return rows, r.invalidateTaskCache(ctx, workspaceID)
	}

	return rows, nil
}

func (r *Repository) GetTask(ctx context.Context, workspaceID string, id uint64) (Task, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return Task{}, err
	}
	if id == 0 || id > math.MaxInt64 {
		return Task{}, fmt.Errorf("tasks get task scope or id is invalid")
	}

	key := adminGetTaskCacheKey(workspaceID, id)
	out, err := repositoryQuery[Task](ctx, r, sqlwrap.Params{
		Key:               key,
		CacheL1Delay:      r.cacheL1Delay,
		CacheL2Delay:      r.cacheL2Delay,
		CacheVersionScope: taskCatalogCacheScope(workspaceID),
	}, func(ctx context.Context) (Task, error) {
		row, err := r.q.AdminGetTask(ctx, tasksqlc.AdminGetTaskParams{
			WorkspaceID: workspaceID,
			ID:          int64(id),
		})
		if err != nil {
			return Task{}, err
		}
		return mapTask(row), nil
	})
	if err != nil {
		return Task{}, err
	}
	return out, nil
}

func (r *Repository) ListTasks(ctx context.Context, workspaceID, groupKey string, limit, offset int32) ([]Task, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	limit, offset = normalizePage(limit, offset)
	key := adminListTasksCacheKey(workspaceID, groupKey, limit, offset)
	out, err := repositoryQuery[[]Task](ctx, r, sqlwrap.Params{
		Key:               key,
		CacheL1Delay:      r.cacheL1Delay,
		CacheL2Delay:      r.cacheL2Delay,
		CacheVersionScope: taskCatalogCacheScope(workspaceID),
	}, func(ctx context.Context) ([]Task, error) {
		var result []Task
		if groupKey != "" {
			rows, err := r.q.AdminListTasksByGroup(ctx, tasksqlc.AdminListTasksByGroupParams{
				WorkspaceID: workspaceID, GroupKey: groupKey, Limit: limit, Offset: offset,
			})
			if err != nil {
				return nil, err
			}
			result = make([]Task, 0, len(rows))
			for _, row := range rows {
				result = append(result, mapTask(tasksqlc.TaskDefinition(row)))
			}
			return result, nil
		}
		rows, err := r.q.AdminListTasks(ctx, tasksqlc.AdminListTasksParams{
			WorkspaceID: workspaceID, Limit: limit, Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		result = make([]Task, 0, len(rows))
		for _, row := range rows {
			result = append(result, mapTask(tasksqlc.TaskDefinition(row)))
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) UpsertTaskLocalization(
	ctx context.Context,
	workspaceID string,
	taskID uint64,
	locale, title, description string,
) error {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return err
	}
	if taskID == 0 || taskID > math.MaxInt64 ||
		strings.TrimSpace(locale) == "" || strings.TrimSpace(title) == "" {
		return fmt.Errorf("tasks localization scope, locale, or title is invalid")
	}

	if err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		return txRepo.q.AdminUpsertTaskLocalization(ctx, tasksqlc.AdminUpsertTaskLocalizationParams{
			WorkspaceID: workspaceID,
			TaskID:      int64(taskID),
			Locale:      locale,
			Title:       title,
			Description: description,
		})
	}); err != nil {
		return err
	}

	return r.invalidateTaskCache(ctx, workspaceID)
}

func (r *Repository) GetGroupLocalization(ctx context.Context, workspaceID, key, locale string) (Localization, error) {
	row, err := r.q.AdminGetGroupLocalization(ctx, tasksqlc.AdminGetGroupLocalizationParams{WorkspaceID: workspaceID, GroupKey: key, Locale: locale})
	if err != nil {
		return Localization{}, err
	}
	return Localization{Locale: row.Locale, Title: row.Title, Description: row.Description}, nil
}
func (r *Repository) ListGroupLocalizations(ctx context.Context, workspaceID, key string) ([]Localization, error) {
	rows, err := r.q.AdminListGroupLocalizationsByGroup(ctx, tasksqlc.AdminListGroupLocalizationsByGroupParams{WorkspaceID: workspaceID, GroupKey: key})
	if err != nil {
		return nil, err
	}
	result := make([]Localization, 0, len(rows))
	for _, row := range rows {
		result = append(result, Localization{Locale: row.Locale, Title: row.Title, Description: row.Description})
	}
	return result, nil
}
func (r *Repository) DeleteGroupLocalization(ctx context.Context, workspaceID, key, locale string) (int64, error) {
	var rows int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(tx *Repository) error {
		var err error
		rows, err = tx.q.AdminDeleteGroupLocalization(ctx, tasksqlc.AdminDeleteGroupLocalizationParams{WorkspaceID: workspaceID, GroupKey: key, Locale: locale})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}
	return rows, r.invalidateTaskCache(ctx, workspaceID)
}
func (r *Repository) GetTaskLocalization(ctx context.Context, workspaceID string, taskID uint64, locale string) (Localization, error) {
	row, err := r.q.AdminGetTaskLocalization(ctx, tasksqlc.AdminGetTaskLocalizationParams{WorkspaceID: workspaceID, TaskID: int64(taskID), Locale: locale})
	if err != nil {
		return Localization{}, err
	}
	return Localization{Locale: row.Locale, Title: row.Title, Description: row.Description}, nil
}
func (r *Repository) ListTaskLocalizations(ctx context.Context, workspaceID string, taskID uint64) ([]Localization, error) {
	rows, err := r.q.AdminListTaskLocalizationsByTask(ctx, tasksqlc.AdminListTaskLocalizationsByTaskParams{WorkspaceID: workspaceID, TaskID: int64(taskID)})
	if err != nil {
		return nil, err
	}
	result := make([]Localization, 0, len(rows))
	for _, row := range rows {
		result = append(result, Localization{Locale: row.Locale, Title: row.Title, Description: row.Description})
	}
	return result, nil
}
func (r *Repository) DeleteTaskLocalization(ctx context.Context, workspaceID string, taskID uint64, locale string) (int64, error) {
	var rows int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(tx *Repository) error {
		var err error
		rows, err = tx.q.AdminDeleteTaskLocalization(ctx, tasksqlc.AdminDeleteTaskLocalizationParams{WorkspaceID: workspaceID, TaskID: int64(taskID), Locale: locale})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}
	return rows, r.invalidateTaskCache(ctx, workspaceID)
}
func (r *Repository) GetReward(ctx context.Context, workspaceID string, taskID uint64, key string) (Reward, error) {
	row, err := r.q.AdminGetReward(ctx, tasksqlc.AdminGetRewardParams{WorkspaceID: workspaceID, TaskID: int64(taskID), RewardKey: key})
	if err != nil {
		return Reward{}, err
	}
	return mapTaskReward(row), nil
}
func (r *Repository) ListRewards(ctx context.Context, workspaceID string, taskID uint64) ([]Reward, error) {
	rows, err := r.q.AdminListRewardsByTask(ctx, tasksqlc.AdminListRewardsByTaskParams{WorkspaceID: workspaceID, TaskID: int64(taskID)})
	if err != nil {
		return nil, err
	}
	result := make([]Reward, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapTaskReward(row))
	}
	return result, nil
}
func (r *Repository) GetComplexCondition(ctx context.Context, workspaceID string, parentTaskID, conditionTaskID uint64) (ComplexCondition, error) {
	row, err := r.q.AdminGetComplexCondition(ctx, tasksqlc.AdminGetComplexConditionParams{WorkspaceID: workspaceID, ParentTaskID: int64(parentTaskID), ConditionTaskID: int64(conditionTaskID)})
	if err != nil {
		return ComplexCondition{}, err
	}
	return ComplexCondition{WorkspaceID: row.WorkspaceID, ParentTaskID: uint64(row.ParentTaskID), ConditionTaskID: uint64(row.ConditionTaskID), RequiredStatus: row.RequiredStatus, Position: row.Position, IsRequired: row.IsRequired}, nil
}
func mapTaskReward(row tasksqlc.TaskReward) Reward {
	return Reward{Key: row.RewardKey, Type: row.RewardType, Quantity: row.Quantity, Scale: uint16(row.Scale), Unit: taskDurationUnitPtr(row.DurationUnit)}
}

func (r *Repository) UpsertReward(
	ctx context.Context,
	workspaceID string,
	taskID uint64,
	reward Reward,
	position int32,
) error {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return err
	}
	if taskID == 0 || taskID > math.MaxInt64 {
		return fmt.Errorf("tasks reward scope is invalid")
	}
	if err := validateRewardDefinition(ExportReward{
		Key:      reward.Key,
		Type:     reward.Type,
		Quantity: reward.Quantity,
		Scale:    reward.Scale,
		Unit:     reward.Unit,
		Position: position,
	}); err != nil {
		return fmt.Errorf("tasks reward: %w", err)
	}

	if err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		return txRepo.q.AdminUpsertReward(ctx, tasksqlc.AdminUpsertRewardParams{
			WorkspaceID: workspaceID,
			TaskID:      int64(taskID),
			RewardKey:   reward.Key,
			RewardType:  reward.Type,
			Quantity:    reward.Quantity,
			Scale:       int16(reward.Scale),
			DurationUnit: sql.NullString{
				String: taskStringValue(reward.Unit),
				Valid:  reward.Unit != nil,
			},
			Position: position,
		})
	}); err != nil {
		return err
	}

	return r.invalidateTaskCache(ctx, workspaceID)
}

func (r *Repository) DeleteReward(ctx context.Context, workspaceID string, taskID uint64, key string) (int64, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return 0, err
	}
	if taskID == 0 || taskID > math.MaxInt64 ||
		strings.TrimSpace(key) == "" {
		return 0, fmt.Errorf("tasks delete reward scope is invalid")
	}

	var rows int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		var err error
		rows, err = txRepo.q.AdminDeleteReward(ctx, tasksqlc.AdminDeleteRewardParams{
			WorkspaceID: workspaceID,
			TaskID:      int64(taskID),
			RewardKey:   key,
		})
		return err
	})
	if err != nil {
		return 0, err
	}
	if rows > 0 {
		return rows, r.invalidateTaskCache(ctx, workspaceID)
	}

	return rows, nil
}

func (r *Repository) UpsertComplexCondition(ctx context.Context, params SaveComplexConditionParams) error {
	params.RequiredStatus = defaultString(params.RequiredStatus, ComplexRequiredStatusReady)
	if err := validateComplexCondition(params); err != nil {
		return err
	}

	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, params.WorkspaceID); err != nil {
			return err
		}

		parent, err := txRepo.q.AdminGetTask(ctx, tasksqlc.AdminGetTaskParams{
			WorkspaceID: params.WorkspaceID,
			ID:          int64(params.ParentTaskID),
		})
		if err != nil {
			return err
		}
		if parent.DeletedAt.Valid || parent.TaskKind != TaskKindComplex {
			return fmt.Errorf("tasks complex condition parent must be a complex task")
		}

		condition, err := txRepo.q.AdminGetTask(ctx, tasksqlc.AdminGetTaskParams{
			WorkspaceID: params.WorkspaceID,
			ID:          int64(params.ConditionTaskID),
		})
		if err != nil {
			return err
		}
		if condition.DeletedAt.Valid {
			return fmt.Errorf("tasks complex condition task is deleted")
		}

		rows, err := txRepo.q.AdminListComplexConditions(ctx, params.WorkspaceID)
		if err != nil {
			return err
		}

		graph := make(map[uint64][]uint64)
		candidateExists := false
		for _, row := range rows {
			parentID := uint64(row.ParentTaskID)
			conditionID := uint64(row.ConditionTaskID)
			graph[parentID] = append(graph[parentID], conditionID)
			if parentID == params.ParentTaskID && conditionID == params.ConditionTaskID {
				candidateExists = true
			}
		}
		if !candidateExists {
			graph[params.ParentTaskID] = append(graph[params.ParentTaskID], params.ConditionTaskID)
		}
		if hasDirectedCycle(graph) {
			return fmt.Errorf("tasks complex condition creates a cycle")
		}

		return txRepo.q.AdminUpsertComplexCondition(ctx, tasksqlc.AdminUpsertComplexConditionParams{
			WorkspaceID:     params.WorkspaceID,
			ParentTaskID:    int64(params.ParentTaskID),
			ConditionTaskID: int64(params.ConditionTaskID),
			RequiredStatus:  params.RequiredStatus,
			Position:        params.Position,
			IsRequired:      params.IsRequired,
		})
	})
	if err != nil {
		return err
	}

	return r.invalidateTaskCache(ctx, params.WorkspaceID)
}

func (r *Repository) DeleteComplexCondition(
	ctx context.Context,
	workspaceID string,
	parentTaskID uint64,
	conditionTaskID uint64,
) (int64, error) {
	if err := validateComplexCondition(SaveComplexConditionParams{
		WorkspaceID:     workspaceID,
		ParentTaskID:    parentTaskID,
		ConditionTaskID: conditionTaskID,
		RequiredStatus:  ComplexRequiredStatusReady,
	}); err != nil {
		return 0, err
	}

	var rows int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		var err error
		rows, err = txRepo.q.AdminDeleteComplexCondition(ctx, tasksqlc.AdminDeleteComplexConditionParams{
			WorkspaceID:     workspaceID,
			ParentTaskID:    int64(parentTaskID),
			ConditionTaskID: int64(conditionTaskID),
		})
		return err
	})
	if err != nil {
		return 0, err
	}
	if rows > 0 {
		return rows, r.invalidateTaskCache(ctx, workspaceID)
	}

	return rows, nil
}

func (r *Repository) ListComplexConditions(ctx context.Context, workspaceID string) ([]ComplexCondition, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	rows, err := repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskComplexCondition, error) {
		return r.q.AdminListComplexConditions(ctx, workspaceID)
	})
	if err != nil {
		return nil, err
	}
	out := make([]ComplexCondition, 0, len(rows))
	for _, row := range rows {
		out = append(out, ComplexCondition{
			WorkspaceID:     row.WorkspaceID,
			ParentTaskID:    uint64(row.ParentTaskID),
			ConditionTaskID: uint64(row.ConditionTaskID),
			RequiredStatus:  row.RequiredStatus,
			Position:        row.Position,
			IsRequired:      row.IsRequired,
		})
	}
	return out, nil
}

func sqlNullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}
