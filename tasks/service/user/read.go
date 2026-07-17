package user

import (
	"context"
	"time"

	"github.com/elum2b/services/tasks/repository"
)

func (u *User) ListActive(ctx context.Context, params ListActiveParams) ([]TaskGroupModel, error) {
	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()

	if err := params.Identity.Validate(); err != nil {
		return nil, err
	}

	tasks, err := u.repository.ListActive(
		mergedCtx,
		repositoryIdentity(params.Identity),
		params.Locale,
		params.GroupKey,
		params.Now,
	)
	if err != nil {
		return nil, err
	}
	return groupTasks(tasks), nil
}

func (u *User) StartTask(ctx context.Context, params StartTaskParams) (StartTaskResult, error) {
	if err := params.Identity.Validate(); err != nil {
		return StartTaskResult{}, err
	}

	if _, ok := repository.ParsePartnerIssueRef(params.TaskRef); ok {
		result, err := u.StartPartner(ctx, PartnerStartParams{
			Identity: params.Identity, IssueRef: params.TaskRef, Now: params.Now,
		})
		return StartTaskResult{Status: result.Status, Started: result.Started, Task: result.Task}, err
	}
	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()
	result, err := u.repository.StartTask(mergedCtx, repository.StartTaskParams{
		Identity: repositoryIdentity(params.Identity), TaskRef: params.TaskRef, Now: params.Now,
	})
	if err != nil {
		return StartTaskResult{}, err
	}
	output := StartTaskResult{Status: result.Status, Started: result.Status == repository.StartStatusStarted}
	if result.Task != nil {
		task := mapTask(*result.Task)
		output.Task = &task
	}
	return output, nil
}

func (u *User) Claim(ctx context.Context, params ClaimParams) (ClaimResult, error) {
	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()

	if err := params.Identity.Validate(); err != nil {
		return ClaimResult{}, err
	}

	if issueID, ok := repository.ParsePartnerIssueRef(params.TaskRef); ok {
		result, err := u.repository.ClaimPartnerIssue(
			mergedCtx,
			repositoryIdentity(params.Identity),
			issueID,
			params.OperationID,
			params.Now,
		)
		if err != nil {
			return ClaimResult{}, err
		}
		output := ClaimResult{Status: result.Status}
		if result.Issue.ID != 0 {
			now := params.Now
			if now.IsZero() {
				now = time.Now().UTC()
			}
			task := partnerIssueTask(result.Issue, result.Rewards, now)
			output.Task = &task
		}
		return output, nil
	}
	result, err := u.repository.Claim(mergedCtx, repository.ClaimParams{
		Identity:    repositoryIdentity(params.Identity),
		TaskRef:     params.TaskRef,
		OperationID: params.OperationID,
		Now:         params.Now,
	})
	if err != nil {
		return ClaimResult{}, err
	}
	output := ClaimResult{Status: result.Status}
	if result.Task != nil {
		task := mapTask(*result.Task)
		output.Task = &task
	}
	return output, nil
}

func repositoryIdentity(identity Identity) repository.Identity {
	return repository.Identity{
		WorkspaceID:    identity.WorkspaceID,
		AppID:          identity.AppID,
		PlatformID:     identity.PlatformID,
		Platform:       identity.Platform,
		PlatformUserID: identity.PlatformUserID,
		IsPremium:      identity.IsPremium,
		Sex:            identity.Sex,
		Country:        identity.Country,
	}
}

func groupTasks(tasks []repository.ActiveTask) []TaskGroupModel {
	groups := make([]TaskGroupModel, 0)
	indexByKey := make(map[string]int, len(tasks))
	for _, task := range tasks {
		index, ok := indexByKey[task.GroupKey]
		if !ok {
			title := task.GroupTitle
			if title == "" {
				title = task.GroupKey
			}
			groups = append(groups, TaskGroupModel{
				Key:         task.GroupKey,
				Title:       title,
				Description: task.GroupDesc,
				Tasks:       make([]TaskModel, 0),
			})
			index = len(groups) - 1
			indexByKey[task.GroupKey] = index
		}
		groups[index].Tasks = append(groups[index].Tasks, task)
	}
	return groups
}

func mapTask(task repository.Task) TaskModel {
	result := TaskModel{
		ID: task.ID, Key: task.Key, GroupKey: task.GroupKey, TaskKind: task.TaskKind,
		ActionKey: task.ActionKey, ActionKind: task.ActionKind, ClaimMode: task.ClaimMode, StartMode: task.StartMode,
		TargetCount: task.TargetCount, Payload: task.Payload, ImageURL: task.ImageURL,
		Rewards: task.Rewards,
	}
	if task.Localization != nil {
		result.Title = task.Localization.Title
		result.Description = task.Localization.Description
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
