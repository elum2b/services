package integration

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/tasks/repository"
)

type Options struct {
	RepositoryOptions    repository.Options
	ChannelCheckers      map[string]ChannelSubscriptionChecker
	ChannelBoostCheckers map[string]ChannelBoostChecker
	ExternalCheckers     map[string]ExternalTaskChecker
}

type Integration struct {
	rootCtx              context.Context
	repository           *repository.Repository
	channelCheckers      map[string]ChannelSubscriptionChecker
	channelBoostCheckers map[string]ChannelBoostChecker
	externalCheckers     map[string]ExternalTaskChecker
}

func New(ctx context.Context, db *sqlwrap.Client) *Integration {
	return NewWithOptions(ctx, db, Options{})
}

func NewWithOptions(ctx context.Context, db *sqlwrap.Client, options Options) *Integration {
	channelChecker := NewChannelSubscriptionChecker(ChannelSubscriptionCheckerOptions{})
	return &Integration{
		rootCtx:              contextutil.Normalize(ctx),
		repository:           repository.NewWithOptions(db, options.RepositoryOptions),
		channelCheckers:      defaultChannelCheckers(channelChecker, options.ChannelCheckers),
		channelBoostCheckers: defaultChannelBoostCheckers(channelChecker, options.ChannelBoostCheckers),
		externalCheckers:     cloneExternalCheckers(options.ExternalCheckers),
	}
}

func (i *Integration) Close() error {
	if i == nil || i.repository == nil {
		return nil
	}
	return i.repository.Close()
}

func (i *Integration) Check(ctx context.Context, params CheckParams) (Result, error) {
	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	if err := params.Identity.Validate(); err != nil {
		return Result{}, err
	}

	now := params.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	task, found, err := i.repository.IntegrationCheckTask(mergedCtx, params.Identity.WorkspaceID, params.TaskRef)
	if err != nil {
		return Result{}, err
	}
	if !found {
		return Result{Status: StatusNotFound}, nil
	}
	checkParams, ok := i.checkParamsForTask(params, task.ActionKind)
	if !ok {
		publicTask := activeTask(task)
		return Result{Status: StatusInvalidTask, Task: &publicTask}, nil
	}
	return i.checkLoadedAndRecord(mergedCtx, checkParams, task, now)
}

func (i *Integration) CheckChannelSubscription(
	ctx context.Context,
	params CheckChannelSubscriptionParams,
) (Result, error) {
	return i.checkAndRecord(ctx, checkAndRecordParams{
		taskRef: params.TaskRefParams, provider: params.Provider,
		variables: params.Variables, expectedActionKind: repository.ActionKindChannelSubscribe,
		check: func(checkCtx context.Context, checker any, task repository.Task, provider string, now time.Time) (CheckResult, error) {
			return checker.(ChannelSubscriptionChecker).CheckChannelSubscription(
				checkCtx,
				ChannelSubscriptionCheckParams{
					Identity: params.Identity, Task: taskContext(task), Provider: provider,
					Variables: params.Variables, OccurredAt: now,
				},
			)
		},
		checker: func(provider string) any {
			return i.channelCheckers[provider]
		},
	})
}

func (i *Integration) CheckChannelBoost(ctx context.Context, params CheckChannelBoostParams) (Result, error) {
	return i.checkAndRecord(ctx, checkAndRecordParams{
		taskRef: params.TaskRefParams, provider: params.Provider,
		variables: params.Variables, expectedActionKind: repository.ActionKindChannelBoost,
		check: func(checkCtx context.Context, checker any, task repository.Task, provider string, now time.Time) (CheckResult, error) {
			return checker.(ChannelBoostChecker).CheckChannelBoost(checkCtx, ChannelBoostCheckParams{
				Identity: params.Identity, Task: taskContext(task), Provider: provider,
				Variables: params.Variables, OccurredAt: now,
			})
		},
		checker: func(provider string) any {
			return i.channelBoostCheckers[provider]
		},
	})
}

func (i *Integration) CheckExternal(ctx context.Context, params CheckExternalParams) (Result, error) {
	return i.checkAndRecord(ctx, checkAndRecordParams{
		taskRef: params.TaskRefParams, provider: params.Provider,
		variables: params.Variables, expectedActionKind: repository.ActionKindExternal,
		check: func(checkCtx context.Context, checker any, task repository.Task, provider string, now time.Time) (CheckResult, error) {
			return checker.(ExternalTaskChecker).CheckExternalTask(checkCtx, ExternalTaskCheckParams{
				Identity: params.Identity, Task: taskContext(task), Provider: provider,
				Variables: params.Variables, OccurredAt: now,
			})
		},
		checker: func(provider string) any {
			return i.externalCheckers[provider]
		},
	})
}

type checkAndRecordParams struct {
	taskRef            TaskRefParams
	provider           string
	variables          map[string]string
	expectedActionKind string
	checker            func(provider string) any
	check              func(context.Context, any, repository.Task, string, time.Time) (CheckResult, error)
}

func (i *Integration) checkAndRecord(ctx context.Context, params checkAndRecordParams) (Result, error) {
	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	if err := params.taskRef.Identity.Validate(); err != nil {
		return Result{}, err
	}

	now := params.taskRef.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	task, found, err := i.repository.IntegrationCheckTask(
		mergedCtx,
		params.taskRef.Identity.WorkspaceID,
		params.taskRef.TaskRef,
	)
	if err != nil {
		return Result{}, err
	}
	if !found {
		return Result{Status: StatusNotFound}, nil
	}
	return i.checkLoadedAndRecord(mergedCtx, params, task, now)
}

func (i *Integration) checkLoadedAndRecord(
	ctx context.Context,
	params checkAndRecordParams,
	task repository.Task,
	now time.Time,
) (Result, error) {
	publicTask := activeTask(task)
	if task.ActionKind != params.expectedActionKind {
		return Result{Status: StatusInvalidTask, Task: &publicTask}, nil
	}
	if task.ClaimMode != repository.ClaimModeManual {
		return Result{Status: StatusInvalidTask, Task: &publicTask}, nil
	}
	provider := ""
	if task.IntegrationProvider != nil {
		provider = *task.IntegrationProvider
	}
	checker := params.checker(provider)
	if checker == nil {
		return Result{Status: StatusNoChecker, Task: &publicTask}, nil
	}
	check, err := params.check(ctx, checker, task, provider, now)
	if err != nil {
		return Result{Status: StatusCheckRejected, Task: &publicTask}, err
	}
	if !check.Completed {
		return Result{Status: StatusNotCompleted, Completed: false, Task: &publicTask, Check: check.Payload}, nil
	}
	ready, err := i.repository.MarkIntegrationTaskReady(ctx, repository.MarkIntegrationTaskReadyParams{
		Identity: params.taskRef.Identity, Task: task,
		Source: integrationSource(provider), ExternalEventKey: integrationEventKey(task, params.taskRef.Identity),
		Payload: check.Payload, Now: now,
	})
	if err != nil {
		return Result{}, err
	}
	status := repository.StatusReady
	if ready.Status == repository.RecordStatusNoTasks {
		status = StatusNotReady
	} else if ready.Status == repository.RecordStatusDuplicate && ready.Task.Progress != nil {
		status = ready.Task.Progress.Status
	}
	readyTask := activeTask(ready.Task)
	return Result{Status: status, Completed: true, Task: &readyTask, Check: check.Payload}, nil
}

func (i *Integration) checkParamsForTask(params CheckParams, actionKind string) (checkAndRecordParams, bool) {
	switch actionKind {
	case repository.ActionKindChannelSubscribe:
		return checkAndRecordParams{
			taskRef: params.TaskRefParams, provider: params.Provider,
			variables: params.Variables, expectedActionKind: repository.ActionKindChannelSubscribe,
			check: func(checkCtx context.Context, checker any, task repository.Task, provider string, now time.Time) (CheckResult, error) {
				return checker.(ChannelSubscriptionChecker).CheckChannelSubscription(
					checkCtx,
					ChannelSubscriptionCheckParams{
						Identity: params.Identity, Task: taskContext(task), Provider: provider,
						Variables: params.Variables, OccurredAt: now,
					},
				)
			},
			checker: func(provider string) any {
				return i.channelCheckers[provider]
			},
		}, true
	case repository.ActionKindChannelBoost:
		return checkAndRecordParams{
			taskRef: params.TaskRefParams, provider: params.Provider,
			variables: params.Variables, expectedActionKind: repository.ActionKindChannelBoost,
			check: func(checkCtx context.Context, checker any, task repository.Task, provider string, now time.Time) (CheckResult, error) {
				return checker.(ChannelBoostChecker).CheckChannelBoost(checkCtx, ChannelBoostCheckParams{
					Identity: params.Identity, Task: taskContext(task), Provider: provider,
					Variables: params.Variables, OccurredAt: now,
				})
			},
			checker: func(provider string) any {
				return i.channelBoostCheckers[provider]
			},
		}, true
	case repository.ActionKindExternal:
		return checkAndRecordParams{
			taskRef: params.TaskRefParams, provider: params.Provider,
			variables: params.Variables, expectedActionKind: repository.ActionKindExternal,
			check: func(checkCtx context.Context, checker any, task repository.Task, provider string, now time.Time) (CheckResult, error) {
				return checker.(ExternalTaskChecker).CheckExternalTask(checkCtx, ExternalTaskCheckParams{
					Identity: params.Identity, Task: taskContext(task), Provider: provider,
					Variables: params.Variables, OccurredAt: now,
				})
			},
			checker: func(provider string) any {
				return i.externalCheckers[provider]
			},
		}, true
	default:
		return checkAndRecordParams{}, false
	}
}

func (i *Integration) ConfirmCompletion(
	ctx context.Context,
	params ConfirmCompletionParams,
) (ConfirmCompletionResult, error) {
	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	if err := params.Identity.Validate(); err != nil {
		return ConfirmCompletionResult{}, err
	}

	task, found, err := i.repository.GetClaimTask(mergedCtx, params.Identity, params.TaskRef, params.Now)
	if err != nil {
		return ConfirmCompletionResult{}, err
	}
	if !found {
		return ConfirmCompletionResult{Status: StatusNotFound}, nil
	}
	result := ConfirmCompletionResult{Status: StatusNotReady, TaskID: task.ID, TaskKey: task.Key}
	if task.Progress == nil {
		return result, nil
	}
	if task.Progress.Status != repository.StatusClaimed {
		result.Status = task.Progress.Status
		return result, nil
	}
	result.Status = repository.StatusClaimed
	result.Completed = true
	result.ClaimedAt = task.Progress.ClaimedAt
	result.OperationID = task.Progress.OperationID
	return result, nil
}

func (i *Integration) withContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if i == nil {
		return contextutil.Merge(context.Background(), ctx)
	}
	return contextutil.Merge(i.rootCtx, ctx)
}

func activeTask(task repository.Task) TaskModel {
	result := TaskModel{
		ID: task.ID, Key: task.Key, GroupKey: task.GroupKey, TaskKind: task.TaskKind,
		ActionKey: task.ActionKey, ActionKind: task.ActionKind, ClaimMode: task.ClaimMode,
		TargetCount: task.TargetCount, Payload: task.Payload, ImageURL: task.ImageURL,
		Rewards: task.Rewards,
	}
	if task.Progress != nil {
		result.Progress = &repository.ActiveProgress{
			Progress: task.Progress.Progress, Status: task.Progress.Status,
			PeriodStartAt: task.Progress.PeriodStartAt, PeriodEndAt: task.Progress.PeriodEndAt,
			ReadyAt: task.Progress.ReadyAt, ClaimedAt: task.Progress.ClaimedAt,
		}
	}
	return result
}

func taskContext(task repository.Task) TaskContext {
	return TaskContext{
		ID: task.ID, Key: task.Key, TaskKind: task.TaskKind,
		ActionKey: task.ActionKey, ActionKind: task.ActionKind, Payload: task.Payload,
		IntegrationKind: task.IntegrationKind, IntegrationProvider: task.IntegrationProvider,
		IntegrationPayload: task.IntegrationPayload,
	}
}

func integrationSource(provider string) string {
	if provider == "" {
		return "tasks.integration"
	}
	return "tasks.integration:" + provider
}

func integrationEventKey(task repository.Task, identity repository.Identity) string {
	return fmt.Sprintf(
		"integration:%d:%s:%d:%d:%s",
		task.ID,
		identity.WorkspaceID,
		identity.AppID,
		identity.PlatformID,
		identity.PlatformUserID,
	)
}

func defaultChannelCheckers(
	checker ChannelSubscriptionChecker,
	overrides map[string]ChannelSubscriptionChecker,
) map[string]ChannelSubscriptionChecker {
	result := map[string]ChannelSubscriptionChecker{
		"telegram": checker,
		"tg":       checker,
		"vk":       checker,
	}
	for key, value := range overrides {
		if value == nil {
			delete(result, key)
			continue
		}
		result[key] = value
	}
	return result
}

func defaultChannelBoostCheckers(
	checker ChannelBoostChecker,
	overrides map[string]ChannelBoostChecker,
) map[string]ChannelBoostChecker {
	result := map[string]ChannelBoostChecker{
		"telegram": checker,
		"tg":       checker,
	}
	for key, value := range overrides {
		if value == nil {
			delete(result, key)
			continue
		}
		result[key] = value
	}
	return result
}

func cloneExternalCheckers(values map[string]ExternalTaskChecker) map[string]ExternalTaskChecker {
	result := make(map[string]ExternalTaskChecker, len(values))
	maps.Copy(result, values)
	return result
}
