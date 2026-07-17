package repository

import (
	"database/sql"
	json "github.com/goccy/go-json"

	calendarsqlc "github.com/elum2b/services/calendar/sqlc"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/google/uuid"
)

func mapDefinition(row calendarsqlc.CalendarDefinition) Calendar {
	return Calendar{
		ID: row.ID, WorkspaceID: row.WorkspaceID, Type: row.Type,
		Mode:                row.Mode,
		IntervalType:        row.IntervalType,
		IntervalUnit:        row.IntervalUnit,
		IntervalCount:       uint32(row.IntervalCount),
		ResetAfterIntervals: uint32(row.ResetAfterIntervals),
		EndBehavior:         row.EndBehavior,
		Timezone:            row.Timezone, HideFutureRewards: row.HideFutureRewards,
		IsActive: row.IsActive, StartAt: sqlwrap.NullTimePtr(row.StartAt),
		EndAt: sqlwrap.NullTimePtr(row.EndAt), DeletedAt: sqlwrap.NullTimePtr(row.DeletedAt),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func appendStep(steps []Step, stepID sql.NullInt64, position sql.NullInt32,
	rewardID sql.NullInt64, rewardKey sql.NullString,
	rewardType sql.NullString, rewardCount sql.NullInt64,
	rewardScale sql.NullInt16, rewardUnit sql.NullString,
) []Step {
	if !stepID.Valid {
		return steps
	}
	if len(steps) == 0 || steps[len(steps)-1].ID != uint64(stepID.Int64) {
		steps = append(steps, Step{
			ID: uint64(stepID.Int64), Position: uint32(position.Int32), Rewards: make([]Reward, 0),
		})
	}
	if rewardID.Valid {
		index := len(steps) - 1
		steps[index].Rewards = append(steps[index].Rewards, Reward{
			Key: rewardKey.String, Type: rewardType.String,
			Quantity: rewardCount.Int64, Scale: uint16FromNull(rewardScale),
			Unit: calendarDurationUnitPtr(rewardUnit),
		})
	}
	return steps
}

func uint16FromNull(value sql.NullInt16) uint16 {
	if !value.Valid || value.Int16 < 0 {
		return 0
	}
	return uint16(value.Int16)
}

func calendarDurationUnitPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func calendarStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func decodeRewards(raw json.RawMessage) ([]Reward, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var rewards []Reward
	if err := json.Unmarshal(raw, &rewards); err != nil {
		return nil, err
	}
	return rewards, nil
}

func calendarReference(ref string) (id, calendarType string) {
	if _, err := uuid.Parse(ref); err == nil {
		return ref, ""
	}
	return "", ref
}
