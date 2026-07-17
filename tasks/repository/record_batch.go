package repository

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type recordProgressUpsert struct {
	taskID        uint64
	periodStartAt time.Time
	periodEndAt   time.Time
	delta         uint64
	status        string
	readyAt       *time.Time
	rewards       []Reward
}

type recordAutoClaim struct {
	task          Task
	progress      Progress
	exists        bool
	periodStartAt time.Time
	periodEndAt   time.Time
}

func (r *Repository) batchUpsertProgress(
	ctx context.Context,
	identity Identity,
	items []recordProgressUpsert,
) (int64, error) {
	if len(items) == 0 {
		return 0, nil
	}
	query, args := compileProgressBulkUpsert(identity, items)
	return repositoryValue[int64](ctx, r, func(ctx context.Context) (int64, error) {
		result, err := r.executor.ExecContext(ctx, query, args...)
		if err != nil {
			return 0, err
		}
		return result.RowsAffected()
	})
}

func compileProgressBulkUpsert(identity Identity, items []recordProgressUpsert) (string, []any) {
	const columns = 11
	var builder strings.Builder
	builder.Grow(len(items)*columns*4 + 320)
	builder.WriteString("INSERT INTO task_progress (")
	builder.WriteString("workspace_id, task_id, app_id, platform_id, platform_user_id, ")
	builder.WriteString("period_start_at, period_end_at, progress, status, ready_at, rewards_snapshot")
	builder.WriteString(") VALUES ")
	args := make([]any, 0, len(items)*columns)
	for index, item := range items {
		if index > 0 {
			builder.WriteString(", ")
		}
		builder.WriteByte('(')
		for columnIndex := 0; columnIndex < columns; columnIndex++ {
			if columnIndex > 0 {
				builder.WriteString(", ")
			}
			builder.WriteByte('$')
			builder.WriteString(fmt.Sprint(len(args) + columnIndex + 1))
		}
		builder.WriteByte(')')
		args = append(args,
			identity.WorkspaceID,
			int64(item.taskID),
			identity.AppID,
			identity.PlatformID,
			identity.PlatformUserID,
			item.periodStartAt,
			item.periodEndAt,
			int64(item.delta),
			item.status,
			nullTime(item.readyAt),
			rawMessageParam(rewardsSnapshot(item.rewards)),
		)
	}
	builder.WriteString(
		" ON CONFLICT (workspace_id, task_id, app_id, platform_id, platform_user_id, period_start_at) DO UPDATE SET ",
	)
	builder.WriteString("period_end_at = EXCLUDED.period_end_at, ")
	builder.WriteString("progress = LEAST(task_progress.progress + EXCLUDED.progress, ")
	builder.WriteString(
		"(SELECT target_count FROM task_definition WHERE workspace_id = EXCLUDED.workspace_id AND id = EXCLUDED.task_id)), ",
	)
	builder.WriteString("status = CASE ")
	builder.WriteString("WHEN task_progress.status IN ('ready', 'claimed') THEN task_progress.status ")
	builder.WriteString("WHEN task_progress.progress + EXCLUDED.progress >= ")
	builder.WriteString(
		"(SELECT target_count FROM task_definition WHERE workspace_id = EXCLUDED.workspace_id AND id = EXCLUDED.task_id) THEN 'ready' ",
	)
	builder.WriteString("ELSE EXCLUDED.status END, ")
	builder.WriteString("ready_at = CASE ")
	builder.WriteString("WHEN task_progress.ready_at IS NOT NULL THEN task_progress.ready_at ")
	builder.WriteString("WHEN task_progress.progress + EXCLUDED.progress >= ")
	builder.WriteString(
		"(SELECT target_count FROM task_definition WHERE workspace_id = EXCLUDED.workspace_id AND id = EXCLUDED.task_id) THEN COALESCE(EXCLUDED.ready_at, now()) ",
	)
	builder.WriteString("ELSE EXCLUDED.ready_at END")
	return builder.String(), args
}
