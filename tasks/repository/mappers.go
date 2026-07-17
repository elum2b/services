package repository

import (
	"database/sql"

	tasksqlc "github.com/elum2b/services/tasks/sqlc"
)

func mapActionTask(row tasksqlc.ListRecordTasksRow) Task {
	return Task{
		ID:               uint64(row.ID),
		WorkspaceID:      row.WorkspaceID,
		Key:              row.Key,
		GroupKey:         row.GroupKey,
		SequenceKey:      ptrString(row.SequenceKey),
		SequencePosition: ptrUint32(row.SequencePosition),
		TaskKind:         row.TaskKind,
		ActionKey:        row.ActionKey,
		ActionKind:       string(row.ActionKind),
		ClaimMode:        string(row.ClaimMode),
		StartMode:        string(row.StartMode),
		TargetCount:      uint64(row.TargetCount),
		ResetUnit:        string(row.ResetUnit),
		ResetEvery:       uint32(row.ResetEvery),
		Position:         row.Position,
		Payload:          nullRawMessage(row.Payload),
		Target:           nullRawMessage(row.Target),
		Rewards:          make([]Reward, 0),
	}
}

func mapClaimTaskByID(rows []tasksqlc.GetClaimBundleByIDForUpdateRow) Task {
	row := rows[0]
	task := Task{
		ID:                  uint64(row.ID),
		WorkspaceID:         row.WorkspaceID,
		Key:                 row.Key,
		GroupKey:            row.GroupKey,
		SequenceKey:         ptrString(row.SequenceKey),
		SequencePosition:    ptrUint32(row.SequencePosition),
		TaskKind:            row.TaskKind,
		ActionKey:           row.ActionKey,
		ActionKind:          string(row.ActionKind),
		ClaimMode:           string(row.ClaimMode),
		StartMode:           string(row.StartMode),
		TargetCount:         uint64(row.TargetCount),
		Payload:             nullRawMessage(row.Payload),
		Target:              nullRawMessage(row.Target),
		ImageURL:            ptrString(row.ImageUrl),
		IntegrationKind:     ptrString(row.IntegrationKind),
		IntegrationProvider: ptrString(row.IntegrationProvider),
		IntegrationPayload:  nullRawMessage(row.IntegrationPayload),
		Rewards:             make([]Reward, 0, len(rows)),
	}
	for _, item := range rows {
		if item.RewardID.Valid {
			task.Rewards = append(task.Rewards, Reward{
				Key:      item.RewardKey.String,
				Type:     item.RewardType.String,
				Quantity: item.RewardQuantity.Int64,
				Scale:    uint16FromNull(item.RewardScale),
				Unit:     taskDurationUnitPtr(item.DurationUnit),
			})
		}
	}
	if row.ProgressID.Valid {
		task.Progress = &Progress{
			ID:            uint64(row.ProgressID.Int64),
			Progress:      uint64(row.Progress.Int64),
			Status:        row.Status.String,
			PeriodStartAt: row.PeriodStartAt.Time,
			PeriodEndAt:   row.PeriodEndAt.Time,
			ReadyAt:       ptrTime(row.ReadyAt),
			ClaimedAt:     ptrTime(row.ClaimedAt),
			OperationID:   ptrString(row.OperationID),
			Rewards:       decodeRewards(nullRawMessage(row.RewardsSnapshot)),
		}
	}
	return task
}

func mapClaimTaskByKey(rows []tasksqlc.GetClaimBundleByKeyForUpdateRow) Task {
	row := rows[0]
	task := Task{
		ID:                  uint64(row.ID),
		WorkspaceID:         row.WorkspaceID,
		Key:                 row.Key,
		GroupKey:            row.GroupKey,
		SequenceKey:         ptrString(row.SequenceKey),
		SequencePosition:    ptrUint32(row.SequencePosition),
		TaskKind:            row.TaskKind,
		ActionKey:           row.ActionKey,
		ActionKind:          string(row.ActionKind),
		ClaimMode:           string(row.ClaimMode),
		StartMode:           string(row.StartMode),
		TargetCount:         uint64(row.TargetCount),
		Payload:             nullRawMessage(row.Payload),
		Target:              nullRawMessage(row.Target),
		ImageURL:            ptrString(row.ImageUrl),
		IntegrationKind:     ptrString(row.IntegrationKind),
		IntegrationProvider: ptrString(row.IntegrationProvider),
		IntegrationPayload:  nullRawMessage(row.IntegrationPayload),
		Rewards:             make([]Reward, 0, len(rows)),
	}
	for _, item := range rows {
		if item.RewardID.Valid {
			task.Rewards = append(task.Rewards, Reward{
				Key:      item.RewardKey.String,
				Type:     item.RewardType.String,
				Quantity: item.RewardQuantity.Int64,
				Scale:    uint16FromNull(item.RewardScale),
				Unit:     taskDurationUnitPtr(item.DurationUnit),
			})
		}
	}
	if row.ProgressID.Valid {
		task.Progress = &Progress{
			ID:            uint64(row.ProgressID.Int64),
			Progress:      uint64(row.Progress.Int64),
			Status:        row.Status.String,
			PeriodStartAt: row.PeriodStartAt.Time,
			PeriodEndAt:   row.PeriodEndAt.Time,
			ReadyAt:       ptrTime(row.ReadyAt),
			ClaimedAt:     ptrTime(row.ClaimedAt),
			OperationID:   ptrString(row.OperationID),
			Rewards:       decodeRewards(nullRawMessage(row.RewardsSnapshot)),
		}
	}
	return task
}

func mapActiveBundles(rows []tasksqlc.ListActiveTaskBundlesRow) []Task {
	result := make([]Task, 0, len(rows))
	var lastID uint64
	index := -1
	for _, row := range rows {
		if index < 0 || uint64(row.ID) != lastID {
			task := Task{
				ID:          uint64(row.ID),
				Key:         row.Key,
				GroupKey:    row.GroupKey,
				TaskKind:    row.TaskKind,
				ActionKey:   row.ActionKey,
				ActionKind:  string(row.ActionKind),
				ClaimMode:   string(row.ClaimMode),
				StartMode:   string(row.StartMode),
				TargetCount: uint64(row.TargetCount),
				Payload:     nullRawMessage(row.Payload),
				Target:      nullRawMessage(row.Target),
				ImageURL:    ptrString(row.ImageUrl),
				StartAt:     ptrTime(row.StartAt),
				EndAt:       ptrTime(row.EndAt),
				Rewards:     make([]Reward, 0),
			}
			if row.GroupTitle.Valid {
				task.GroupTitle = row.GroupTitle.String
			}
			if row.GroupDescription.Valid {
				task.GroupDesc = row.GroupDescription.String
			}
			if row.Locale.Valid {
				task.Localization = &Localization{
					Locale:      row.Locale.String,
					Title:       row.Title.String,
					Description: row.Description.String,
				}
			}
			result = append(result, task)
			index = len(result) - 1
			lastID = uint64(row.ID)
		}
		if row.RewardID.Valid {
			result[index].Rewards = append(result[index].Rewards, Reward{
				Key:      row.RewardKey.String,
				Type:     row.RewardType.String,
				Quantity: row.RewardQuantity.Int64,
				Scale:    uint16FromNull(row.RewardScale),
				Unit:     taskDurationUnitPtr(row.DurationUnit),
			})
		}
	}
	return result
}

func mapRecordCatalogTask(row tasksqlc.ListRecordCatalogRow) Task {
	return Task{
		ID: uint64(row.ID), WorkspaceID: row.WorkspaceID, Key: row.Key, GroupKey: row.GroupKey,
		SequenceKey: ptrString(row.SequenceKey), SequencePosition: ptrUint32(row.SequencePosition),
		TaskKind: row.TaskKind, ActionKey: row.ActionKey, ActionKind: string(row.ActionKind),
		ClaimMode: string(
			row.ClaimMode,
		), StartMode: string(row.StartMode), TargetCount: uint64(row.TargetCount), ResetUnit: string(row.ResetUnit),
		ResetEvery: uint32(
			row.ResetEvery,
		), Position: row.Position, Payload: nullRawMessage(row.Payload), Target: nullRawMessage(row.Target),
		StartAt: ptrTime(row.StartAt), EndAt: ptrTime(row.EndAt), Rewards: decodeRewards([]byte(row.Rewards)),
	}
}

func mapIntegrationCheckTaskByID(row tasksqlc.GetIntegrationCheckTaskByIDRow) Task {
	return Task{
		ID: uint64(row.ID), WorkspaceID: row.WorkspaceID, Key: row.Key, GroupKey: row.GroupKey,
		SequenceKey: ptrString(row.SequenceKey), SequencePosition: ptrUint32(row.SequencePosition),
		TaskKind: row.TaskKind, ActionKey: row.ActionKey, ActionKind: string(row.ActionKind),
		ClaimMode: string(
			row.ClaimMode,
		), StartMode: string(row.StartMode), TargetCount: uint64(row.TargetCount), ResetUnit: string(row.ResetUnit),
		ResetEvery: uint32(
			row.ResetEvery,
		), Payload: nullRawMessage(row.Payload), Target: nullRawMessage(row.Target), IntegrationKind: ptrString(row.IntegrationKind),
		IntegrationProvider: ptrString(
			row.IntegrationProvider,
		), IntegrationPayload: nullRawMessage(row.IntegrationPayload),
		ImageURL: ptrString(row.ImageUrl), StartAt: ptrTime(row.StartAt), EndAt: ptrTime(row.EndAt),
		Rewards: make([]Reward, 0),
	}
}

func mapIntegrationCheckTaskByKey(row tasksqlc.GetIntegrationCheckTaskByKeyRow) Task {
	return Task{
		ID: uint64(row.ID), WorkspaceID: row.WorkspaceID, Key: row.Key, GroupKey: row.GroupKey,
		SequenceKey: ptrString(row.SequenceKey), SequencePosition: ptrUint32(row.SequencePosition),
		TaskKind: row.TaskKind, ActionKey: row.ActionKey, ActionKind: string(row.ActionKind),
		ClaimMode: string(
			row.ClaimMode,
		), StartMode: string(row.StartMode), TargetCount: uint64(row.TargetCount), ResetUnit: string(row.ResetUnit),
		ResetEvery: uint32(
			row.ResetEvery,
		), Payload: nullRawMessage(row.Payload), Target: nullRawMessage(row.Target), IntegrationKind: ptrString(row.IntegrationKind),
		IntegrationProvider: ptrString(
			row.IntegrationProvider,
		), IntegrationPayload: nullRawMessage(row.IntegrationPayload),
		ImageURL: ptrString(row.ImageUrl), StartAt: ptrTime(row.StartAt), EndAt: ptrTime(row.EndAt),
		Rewards: make([]Reward, 0),
	}
}

func mapStartTaskByID(row tasksqlc.GetStartTaskByIDRow) Task {
	return Task{
		ID: uint64(row.ID), WorkspaceID: row.WorkspaceID, Key: row.Key, GroupKey: row.GroupKey,
		SequenceKey: ptrString(row.SequenceKey), SequencePosition: ptrUint32(row.SequencePosition),
		TaskKind: row.TaskKind, ActionKey: row.ActionKey, ActionKind: string(row.ActionKind),
		ClaimMode: string(row.ClaimMode), StartMode: string(row.StartMode), TargetCount: uint64(row.TargetCount),
		ResetUnit: string(
			row.ResetUnit,
		), ResetEvery: uint32(row.ResetEvery), Payload: nullRawMessage(row.Payload), Target: nullRawMessage(row.Target),
		IntegrationKind: ptrString(row.IntegrationKind), IntegrationProvider: ptrString(row.IntegrationProvider),
		IntegrationPayload: nullRawMessage(row.IntegrationPayload), ImageURL: ptrString(row.ImageUrl),
		StartAt: ptrTime(row.StartAt), EndAt: ptrTime(row.EndAt), Rewards: make([]Reward, 0),
	}
}

func mapStartTaskByKey(row tasksqlc.GetStartTaskByKeyRow) Task {
	return Task{
		ID: uint64(row.ID), WorkspaceID: row.WorkspaceID, Key: row.Key, GroupKey: row.GroupKey,
		SequenceKey: ptrString(row.SequenceKey), SequencePosition: ptrUint32(row.SequencePosition),
		TaskKind: row.TaskKind, ActionKey: row.ActionKey, ActionKind: string(row.ActionKind),
		ClaimMode: string(row.ClaimMode), StartMode: string(row.StartMode), TargetCount: uint64(row.TargetCount),
		ResetUnit: string(
			row.ResetUnit,
		), ResetEvery: uint32(row.ResetEvery), Payload: nullRawMessage(row.Payload), Target: nullRawMessage(row.Target),
		IntegrationKind: ptrString(row.IntegrationKind), IntegrationProvider: ptrString(row.IntegrationProvider),
		IntegrationPayload: nullRawMessage(row.IntegrationPayload), ImageURL: ptrString(row.ImageUrl),
		StartAt: ptrTime(row.StartAt), EndAt: ptrTime(row.EndAt), Rewards: make([]Reward, 0),
	}
}

func mapClaimCatalogTaskByID(rows []tasksqlc.GetClaimCatalogByIDRow) Task {
	row := rows[0]
	task := Task{
		ID: uint64(row.ID), WorkspaceID: row.WorkspaceID, Key: row.Key, GroupKey: row.GroupKey,
		SequenceKey: ptrString(row.SequenceKey), SequencePosition: ptrUint32(row.SequencePosition),
		TaskKind: row.TaskKind, ActionKey: row.ActionKey, ActionKind: string(row.ActionKind),
		ClaimMode: string(
			row.ClaimMode,
		), StartMode: string(row.StartMode), TargetCount: uint64(row.TargetCount), Payload: nullRawMessage(row.Payload), Target: nullRawMessage(row.Target),
		IntegrationKind: ptrString(row.IntegrationKind), IntegrationProvider: ptrString(row.IntegrationProvider),
		IntegrationPayload: nullRawMessage(row.IntegrationPayload), ImageURL: ptrString(row.ImageUrl),
		Rewards: make([]Reward, 0, len(rows)),
	}
	for _, item := range rows {
		if item.RewardID.Valid {
			task.Rewards = append(task.Rewards, Reward{
				Key: item.RewardKey.String, Type: item.RewardType.String,
				Quantity: item.RewardQuantity.Int64, Scale: uint16FromNull(item.RewardScale),
				Unit: taskDurationUnitPtr(item.DurationUnit),
			})
		}
	}
	return task
}

func mapClaimCatalogTaskByKey(rows []tasksqlc.GetClaimCatalogByKeyRow) Task {
	row := rows[0]
	task := Task{
		ID: uint64(row.ID), WorkspaceID: row.WorkspaceID, Key: row.Key, GroupKey: row.GroupKey,
		SequenceKey: ptrString(row.SequenceKey), SequencePosition: ptrUint32(row.SequencePosition),
		TaskKind: row.TaskKind, ActionKey: row.ActionKey, ActionKind: string(row.ActionKind),
		ClaimMode: string(
			row.ClaimMode,
		), StartMode: string(row.StartMode), TargetCount: uint64(row.TargetCount), Payload: nullRawMessage(row.Payload), Target: nullRawMessage(row.Target),
		IntegrationKind: ptrString(row.IntegrationKind), IntegrationProvider: ptrString(row.IntegrationProvider),
		IntegrationPayload: nullRawMessage(row.IntegrationPayload), ImageURL: ptrString(row.ImageUrl),
		Rewards: make([]Reward, 0, len(rows)),
	}
	for _, item := range rows {
		if item.RewardID.Valid {
			task.Rewards = append(task.Rewards, Reward{
				Key: item.RewardKey.String, Type: item.RewardType.String,
				Quantity: item.RewardQuantity.Int64, Scale: uint16FromNull(item.RewardScale),
				Unit: taskDurationUnitPtr(item.DurationUnit),
			})
		}
	}
	return task
}

func taskDurationUnitPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	unit := value.String
	return &unit
}

func uint16FromNull(value sql.NullInt16) uint16 {
	if !value.Valid || value.Int16 < 0 {
		return 0
	}
	return uint16(value.Int16)
}

func taskStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func mapProgress(row tasksqlc.TaskProgress) Progress {
	return Progress{
		ID:            uint64(row.ID),
		Progress:      uint64(row.Progress),
		Status:        row.Status,
		PeriodStartAt: row.PeriodStartAt,
		PeriodEndAt:   row.PeriodEndAt,
		ReadyAt:       ptrTime(row.ReadyAt),
		ClaimedAt:     ptrTime(row.ClaimedAt),
		OperationID:   ptrString(row.OperationID),
		Rewards:       decodeRewards(nullRawMessage(row.RewardsSnapshot)),
	}
}

func mapActiveProgress(row tasksqlc.TaskProgress) ActiveProgress {
	return ActiveProgress{
		Progress:      uint64(row.Progress),
		Status:        row.Status,
		PeriodStartAt: row.PeriodStartAt,
		PeriodEndAt:   row.PeriodEndAt,
		ReadyAt:       ptrTime(row.ReadyAt),
		ClaimedAt:     ptrTime(row.ClaimedAt),
	}
}
