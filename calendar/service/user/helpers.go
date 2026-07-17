package user

import "github.com/elum2b/services/calendar/repository"

func mapCalendar(value repository.Calendar) CalendarModel {
	result := CalendarModel{
		ID: value.ID, Type: value.Type, Mode: value.Mode, IntervalType: value.IntervalType,
		IntervalUnit: value.IntervalUnit, IntervalCount: value.IntervalCount,
		ResetAfterIntervals: value.ResetAfterIntervals, EndBehavior: value.EndBehavior,
		Timezone: value.Timezone, HideFutureRewards: value.HideFutureRewards,
		IsActive: value.IsActive, StartAt: value.StartAt, EndAt: value.EndAt,
		Steps: make([]StepModel, 0, len(value.Steps)),
	}
	if value.Localization != nil {
		result.Title = value.Localization.Title
		result.Description = value.Localization.Description
	}
	for _, step := range value.Steps {
		item := StepModel{ID: step.ID, Position: step.Position, Rewards: make([]RewardModel, 0, len(step.Rewards))}
		for _, reward := range step.Rewards {
			item.Rewards = append(item.Rewards, RewardModel{
				Key: reward.Key, Type: reward.Type, Quantity: reward.Quantity, Scale: reward.Scale, Unit: reward.Unit,
			})
		}
		result.Steps = append(result.Steps, item)
	}
	return result
}

func mapProgress(value repository.Progress) ProgressModel {
	return ProgressModel{
		CurrentPosition: value.CurrentPosition, ClaimCount: value.ClaimCount,
		LastClaimPosition: value.LastClaimPosition, LastClaimAt: value.LastClaimAt,
		NextClaimAt: value.NextClaimAt, IsCompleted: value.IsCompleted,
		ResetCount: value.ResetCount, LastWasReset: value.LastWasReset,
	}
}

func mapRecord(value repository.RecordResult) RecordResult {
	result := RecordResult{
		OperationRowID: value.OperationRowID, OperationID: value.OperationID,
		Granted: value.Granted, Status: value.Status, Calendar: mapCalendar(value.Calendar),
		Position: value.Position, Progress: mapProgress(value.Progress), OccurredAt: value.OccurredAt,
		Rewards: make([]RewardModel, 0, len(value.Rewards)),
	}
	for _, reward := range value.Rewards {
		result.Rewards = append(result.Rewards, RewardModel{
			Key: reward.Key, Type: reward.Type, Quantity: reward.Quantity, Scale: reward.Scale, Unit: reward.Unit,
		})
	}
	return result
}

func hideFutureRewardSteps(result *RecordResult) {
	if result == nil || !result.Calendar.HideFutureRewards {
		return
	}

	maxPosition := result.Progress.CurrentPosition + 1
	filtered := result.Calendar.Steps[:0]
	for _, step := range result.Calendar.Steps {
		if step.Position <= maxPosition {
			filtered = append(filtered, step)
		}
	}
	result.Calendar.Steps = filtered
}

func repositoryIdentity(value Identity) repository.Identity {
	return repository.Identity{
		WorkspaceID: value.WorkspaceID, AppID: value.AppID,
		PlatformID: value.PlatformID, PlatformUserID: value.PlatformUserID,
	}
}
