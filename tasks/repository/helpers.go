package repository

import (
	"database/sql"
	json "github.com/goccy/go-json"
	"strconv"
	"time"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	tasksqlc "github.com/elum2b/services/tasks/sqlc"
)

var periodAnchor = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
var periodForeverEnd = time.Date(9998, 12, 31, 23, 59, 59, 0, time.UTC)

func nullString(value *string) sql.NullString {
	return sqlwrap.NullFromPtr(value, func(v string) sql.NullString {
		return sql.NullString{String: v, Valid: true}
	})
}

func nullInt32FromUint32(value *uint32) sql.NullInt32 {
	return sqlwrap.NullFromPtr(value, func(v uint32) sql.NullInt32 {
		return sql.NullInt32{Int32: int32(v), Valid: true}
	})
}

func nullTime(value *time.Time) sql.NullTime {
	return sqlwrap.NullFromPtr(value, func(v time.Time) sql.NullTime {
		return sql.NullTime{Time: v, Valid: true}
	})
}

func ptrString(value sql.NullString) *string {
	return sqlwrap.NullStringPtr(value)
}

func ptrUint32(value sql.NullInt32) *uint32 {
	if !value.Valid {
		return nil
	}
	result := uint32(value.Int32)
	return &result
}

func int64sFromUint64s(values []uint64) []int64 {
	out := make([]int64, 0, len(values))
	for _, value := range values {
		out = append(out, int64(value))
	}
	return out
}

func uint64sFromInt64s(values []int64) []uint64 {
	out := make([]uint64, 0, len(values))
	for _, value := range values {
		out = append(out, uint64(value))
	}
	return out
}

func ptrTime(value sql.NullTime) *time.Time {
	return sqlwrap.NullTimePtr(value)
}

func taskRef(value string) (id uint64, key string) {
	if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
		return parsed, ""
	}
	return 0, value
}

func periodFor(task Task, now time.Time) (time.Time, time.Time) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	if task.ResetUnit == ResetNever {
		return periodAnchor, periodForeverEnd
	}
	count := task.ResetEvery
	if count == 0 {
		count = 1
	}
	start := periodAnchor
	switch task.ResetUnit {
	case ResetSecond:
		step := time.Duration(count) * time.Second
		start = periodAnchor.Add(time.Duration(now.Sub(periodAnchor)/step) * step)
		return start, start.Add(step)
	case ResetMinute:
		step := time.Duration(count) * time.Minute
		start = periodAnchor.Add(time.Duration(now.Sub(periodAnchor)/step) * step)
		return start, start.Add(step)
	case ResetHour:
		step := time.Duration(count) * time.Hour
		start = periodAnchor.Add(time.Duration(now.Sub(periodAnchor)/step) * step)
		return start, start.Add(step)
	case ResetDay:
		days := int(now.Sub(periodAnchor).Hours() / 24)
		start = periodAnchor.AddDate(0, 0, (days/int(count))*int(count))
		return start, start.AddDate(0, 0, int(count))
	case ResetYear:
		years := now.Year() - periodAnchor.Year()
		start = periodAnchor.AddDate((years/int(count))*int(count), 0, 0)
		return start, start.AddDate(int(count), 0, 0)
	default:
		return periodAnchor, periodForeverEnd
	}
}

func mapTask(row tasksqlc.TaskDefinition) Task {
	return Task{
		ID: uint64(row.ID), WorkspaceID: row.WorkspaceID, Key: row.Key, GroupKey: row.GroupKey,
		SequenceKey: ptrString(row.SequenceKey), SequencePosition: ptrUint32(row.SequencePosition),
		TaskKind: row.TaskKind, ActionKey: row.ActionKey, ActionKind: string(row.ActionKind), ClaimMode: string(row.ClaimMode), StartMode: string(row.StartMode),
		TargetCount: uint64(row.TargetCount), ResetUnit: string(row.ResetUnit), ResetEvery: uint32(row.ResetEvery),
		Position: row.Position, Payload: nullRawMessage(row.Payload), Target: nullRawMessage(row.Target), IntegrationKind: ptrString(row.IntegrationKind),
		IntegrationProvider: ptrString(
			row.IntegrationProvider,
		), IntegrationPayload: nullRawMessage(row.IntegrationPayload),
		ImageURL:  ptrString(row.ImageUrl),
		IsVisible: row.IsVisible, IsActive: row.IsActive, StartAt: ptrTime(row.StartAt),
		EndAt: ptrTime(row.EndAt), DeletedAt: ptrTime(row.DeletedAt),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Rewards: make([]Reward, 0),
	}
}

func decodeRewards(raw json.RawMessage) []Reward {
	if len(raw) == 0 {
		return nil
	}
	var rewards []Reward
	_ = json.Unmarshal(raw, &rewards)
	return rewards
}
