package admin

import (
	"context"

	"github.com/elum2b/services/tasks/repository"
)

func (a *Admin) UpsertGroup(ctx context.Context, workspaceID, key string, position int32, active bool) error {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.UpsertGroup(mergedCtx, workspaceID, key, position, active)
}

func (a *Admin) UpsertGroupLocalization(
	ctx context.Context,
	workspaceID, key, locale, title, description string,
) error {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.UpsertGroupLocalization(mergedCtx, workspaceID, key, locale, title, description)
}

func (a *Admin) UpsertSequence(ctx context.Context, workspaceID, key string, position int32, active bool) error {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.UpsertSequence(mergedCtx, workspaceID, key, position, active)
}

func (a *Admin) SaveTask(ctx context.Context, params SaveTaskParams) (uint64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.SaveTask(mergedCtx, repository.SaveTaskParams(params))
}

func (a *Admin) DeleteTask(ctx context.Context, workspaceID string, id uint64) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.DeleteTask(mergedCtx, workspaceID, id)
}

func (a *Admin) GetTask(ctx context.Context, workspaceID string, id uint64) (TaskModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	task, err := a.repository.GetTask(mergedCtx, workspaceID, id)
	if err != nil {
		return TaskModel{}, err
	}
	return mapTask(task), nil
}

func (a *Admin) ListTasks(ctx context.Context, workspaceID, groupKey string, limit, offset int32) ([]TaskModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	tasks, err := a.repository.ListTasks(mergedCtx, workspaceID, groupKey, limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]TaskModel, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, mapTask(task))
	}
	return result, nil
}

func (a *Admin) UpsertTaskLocalization(
	ctx context.Context,
	workspaceID string,
	taskID uint64,
	locale, title, description string,
) error {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.UpsertTaskLocalization(mergedCtx, workspaceID, taskID, locale, title, description)
}

func (a *Admin) UpsertReward(
	ctx context.Context,
	workspaceID string,
	taskID uint64,
	reward RewardModel,
	position int32,
) error {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	rewardType, err := validateReward(reward)
	if err != nil {
		return err
	}
	return a.repository.UpsertReward(mergedCtx, workspaceID, taskID, repository.Reward{
		Key: reward.Key, Type: rewardType, Quantity: reward.Quantity, Scale: reward.Scale, Unit: reward.Unit,
	}, position)
}

func validateReward(reward RewardModel) (string, error) {
	if reward.Key == "" || reward.Quantity <= 0 {
		return "", ErrRewardRequired
	}
	rewardType := reward.Type
	if rewardType == "" {
		rewardType = "quantity"
	}
	switch rewardType {
	case "quantity":
		if reward.Unit != nil {
			return "", ErrRewardQuantityUnit
		}
	case "duration":
		if reward.Unit == nil || !validDurationUnit(*reward.Unit) {
			return "", ErrRewardDurationUnit
		}
	default:
		return "", ErrRewardTypeUnsupported
	}
	return rewardType, nil
}

func validDurationUnit(unit string) bool {
	switch unit {
	case "second", "minute", "hour", "day", "week", "month", "year":
		return true
	default:
		return false
	}
}

func (a *Admin) DeleteReward(ctx context.Context, workspaceID string, taskID uint64, key string) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.DeleteReward(mergedCtx, workspaceID, taskID, key)
}

func (a *Admin) UpsertComplexCondition(ctx context.Context, params SaveComplexConditionParams) error {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.UpsertComplexCondition(mergedCtx, repository.SaveComplexConditionParams(params))
}

func (a *Admin) DeleteComplexCondition(
	ctx context.Context,
	workspaceID string,
	parentTaskID uint64,
	conditionTaskID uint64,
) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.DeleteComplexCondition(mergedCtx, workspaceID, parentTaskID, conditionTaskID)
}

func (a *Admin) ListComplexConditions(ctx context.Context, workspaceID string) ([]ComplexConditionModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	conditions, err := a.repository.ListComplexConditions(mergedCtx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]ComplexConditionModel, 0, len(conditions))
	for _, condition := range conditions {
		out = append(out, ComplexConditionModel{
			WorkspaceID:     condition.WorkspaceID,
			ParentTaskID:    condition.ParentTaskID,
			ConditionTaskID: condition.ConditionTaskID,
			RequiredStatus:  condition.RequiredStatus,
			Position:        condition.Position,
			IsRequired:      condition.IsRequired,
		})
	}
	return out, nil
}

func mapTask(task repository.Task) TaskModel {
	return TaskModel{
		ID: task.ID, Key: task.Key, GroupKey: task.GroupKey,
		SequenceKey: task.SequenceKey, SequencePosition: task.SequencePosition,
		TaskKind: task.TaskKind, ActionKey: task.ActionKey, ActionKind: task.ActionKind, ClaimMode: task.ClaimMode, StartMode: task.StartMode,
		TargetCount: task.TargetCount, ResetUnit: task.ResetUnit, ResetEvery: task.ResetEvery,
		Position: task.Position, Payload: task.Payload, Target: task.Target, IntegrationKind: task.IntegrationKind,
		IntegrationProvider: task.IntegrationProvider, IntegrationPayload: task.IntegrationPayload,
		ImageURL:  task.ImageURL,
		IsVisible: task.IsVisible, IsActive: task.IsActive, StartAt: task.StartAt,
		EndAt: task.EndAt, DeletedAt: task.DeletedAt,
	}
}
