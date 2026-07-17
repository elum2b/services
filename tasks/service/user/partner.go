package user

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"
	"github.com/google/uuid"

	serviceerrors "github.com/elum2b/services/errors"
	"github.com/elum2b/services/internal/utils/target"
	"github.com/elum2b/services/tasks/repository"
)

const (
	PartnerStatusNotConfigured  = "not_configured"
	PartnerStatusDisabled       = "disabled"
	PartnerStatusTargetMismatch = "target_mismatch"
	PartnerStatusNoProvider     = "no_provider"
	PartnerStatusNotSupported   = "not_supported"
	PartnerStatusNotFound       = repository.ClaimStatusNotFound
	PartnerStatusNotCompleted   = "not_completed"
	PartnerStatusReady          = repository.StatusReady
	PartnerStatusStarted        = "started"
	PartnerStatusExpired        = repository.PartnerIssueStatusExpired

	defaultPartnerStartLeaseDuration = time.Minute
	partnerStartPollDelay            = 10 * time.Millisecond
	partnerStartReleaseTimeout       = 2 * time.Second
)

var ErrPartnerStartLeaseLost = serviceerrors.New(
	serviceerrors.CodeConflict,
	"tasks partner start lease lost",
)

func (u *User) ListPartner(ctx context.Context, params PartnerListParams) ([]TaskModel, error) {
	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()

	if err := params.Identity.Validate(); err != nil {
		return nil, err
	}

	now := params.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	platform := params.Platform
	if platform == "" {
		platform = params.Identity.Platform
	}
	config, found, err := u.repository.GetPartnerConfig(
		mergedCtx,
		params.Identity.WorkspaceID,
		params.Provider,
		params.GroupKey,
		platform,
	)
	if err != nil || !found || !config.IsEnabled {
		return nil, err
	}
	if !target.Match(config.Target, target.Context{
		IsPremium: params.Identity.IsPremium, Sex: params.Identity.Sex, Country: params.Identity.Country,
		Locale: params.Locale, Platform: params.Identity.Platform, PlatformID: params.Identity.PlatformID,
	}) {
		return []TaskModel{}, nil
	}
	repoIdentity := repositoryIdentity(params.Identity)
	existing, err := u.repository.ListPartnerIssuesForUser(
		mergedCtx,
		repoIdentity,
		config.Provider,
		config.GroupKey,
		config.Platform,
		now,
	)
	if err != nil {
		return nil, err
	}
	result := make([]TaskModel, 0, len(existing))
	seen := make(map[string]struct{}, len(existing))
	for _, issue := range existing {
		result = append(result, partnerIssueTask(issue, issue.Rewards, now))
		seen[issue.IssueKey] = struct{}{}
	}
	provider := u.partnerProvider(params.Provider)
	if provider == nil {
		return result, nil
	}
	externalTasks, err := provider.ListPartnerTasks(mergedCtx, PartnerListProviderParams{
		Identity: params.Identity, Config: config, Locale: params.Locale,
		Limit: params.Limit, Variables: params.Variables, Now: now,
	})
	if err != nil {
		return nil, err
	}
	for _, external := range externalTasks {
		issueKey := partnerIssueKey(config, params.Identity, external)
		if _, ok := seen[issueKey]; ok {
			continue
		}
		rewards, err := u.repository.PartnerRewards(
			mergedCtx,
			config.WorkspaceID,
			config.Provider,
			config.GroupKey,
			external.ExternalType,
		)
		if err != nil {
			return nil, err
		}
		issue, _, err := u.repository.CreatePartnerIssue(mergedCtx, repository.CreatePartnerIssueParams{
			Identity:       repoIdentity,
			Provider:       config.Provider,
			GroupKey:       config.GroupKey,
			Platform:       config.Platform,
			ExternalID:     external.ExternalID,
			ExternalType:   external.ExternalType,
			IssueKey:       issueKey,
			PublicPayload:  external.PublicPayload,
			PrivatePayload: external.PrivatePayload,
			Rewards:        rewards,
			ExpiresAt:      external.ExpiresAt,
			StartMode:      external.StartMode,
			Now:            now,
		})
		if err != nil {
			return nil, err
		}
		result = append(result, partnerIssueTask(issue, issue.Rewards, now))
		seen[issue.IssueKey] = struct{}{}
	}
	return result, nil
}

func (u *User) CheckPartner(ctx context.Context, params PartnerCheckParams) (PartnerCheckOutput, error) {
	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()

	if err := params.Identity.Validate(); err != nil {
		return PartnerCheckOutput{}, err
	}

	now := params.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	issueID, ok := repository.ParsePartnerIssueRef(params.IssueRef)
	if !ok {
		return PartnerCheckOutput{Status: PartnerStatusNotFound}, nil
	}
	issue, found, err := u.repository.GetPartnerIssue(mergedCtx, params.Identity.WorkspaceID, issueID)
	if err != nil {
		return PartnerCheckOutput{}, err
	}
	if !found || issue.AppID != params.Identity.AppID || issue.PlatformID != params.Identity.PlatformID ||
		issue.PlatformUserID != params.Identity.PlatformUserID {
		return PartnerCheckOutput{Status: PartnerStatusNotFound}, nil
	}
	task := partnerIssueTask(issue, issue.Rewards, now)
	if issue.Status == repository.PartnerIssueStatusCompleted || issue.Status == repository.PartnerIssueStatusClaimed {
		return PartnerCheckOutput{Status: task.Progress.Status, Completed: true, Task: &task}, nil
	}
	if issue.Status == repository.PartnerIssueStatusExpired {
		return PartnerCheckOutput{Status: PartnerStatusExpired, Completed: false, Task: &task}, nil
	}
	if partnerIssueDeadlinePassed(issue, now) {
		issue, _, err = u.repository.ExpirePartnerIssue(
			mergedCtx,
			partnerIssueScope(issue),
			issue.ID,
			PartnerStatusExpired,
			nil,
			now,
		)
		if err != nil {
			return PartnerCheckOutput{}, err
		}
		task = partnerIssueTask(issue, issue.Rewards, now)
		return PartnerCheckOutput{Status: PartnerStatusExpired, Completed: false, Task: &task}, nil
	}
	if issue.Status == repository.PartnerIssueStatusRevoked ||
		issue.Status == repository.PartnerIssueStatusRevokedAfterClaim {
		return PartnerCheckOutput{Status: task.Progress.Status, Completed: false, Task: &task}, nil
	}
	if issue.StartMode == repository.StartModeRequired && issue.StartedAt == nil {
		return PartnerCheckOutput{Status: repository.ClaimStatusNotStarted, Completed: false, Task: &task}, nil
	}
	config, found, err := u.repository.GetPartnerConfig(
		mergedCtx,
		issue.WorkspaceID,
		issue.Provider,
		issue.GroupKey,
		issue.Platform,
	)
	if err != nil {
		return PartnerCheckOutput{}, err
	}
	if !found || !config.IsEnabled {
		return PartnerCheckOutput{Status: PartnerStatusNotConfigured, Task: &task}, nil
	}
	provider := u.partnerProvider(issue.Provider)
	if provider == nil {
		return PartnerCheckOutput{Status: PartnerStatusNoProvider, Task: &task}, nil
	}
	check, err := provider.CheckPartnerTask(mergedCtx, PartnerCheckProviderParams{
		Identity: params.Identity, Config: config, Issue: issue, Variables: params.Variables, Now: now,
	})
	if err != nil {
		return PartnerCheckOutput{}, err
	}
	if !check.Completed {
		return PartnerCheckOutput{Status: PartnerStatusNotCompleted, Completed: false, Task: &task}, nil
	}
	issue, _, err = u.repository.CompletePartnerIssue(
		mergedCtx,
		repository.PartnerIssueScope{
			WorkspaceID: issue.WorkspaceID,
			Provider:    issue.Provider,
			GroupKey:    issue.GroupKey,
			Platform:    issue.Platform,
		},
		issue.ID,
		check.Status,
		check.Payload,
		now,
	)
	if err != nil {
		return PartnerCheckOutput{}, err
	}
	task = partnerIssueTask(issue, issue.Rewards, now)
	if issue.Status == repository.PartnerIssueStatusExpired {
		return PartnerCheckOutput{Status: PartnerStatusExpired, Completed: false, Task: &task}, nil
	}
	return PartnerCheckOutput{Status: PartnerStatusReady, Completed: true, Task: &task}, nil
}

func (u *User) StartPartner(ctx context.Context, params PartnerStartParams) (PartnerStartOutput, error) {

	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()

	if err := params.Identity.Validate(); err != nil {
		return PartnerStartOutput{}, err
	}

	now := params.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	issueID, ok := repository.ParsePartnerIssueRef(params.IssueRef)
	if !ok {
		return PartnerStartOutput{Status: PartnerStatusNotFound}, nil
	}

	issue, found, err := u.repository.GetPartnerIssue(mergedCtx, params.Identity.WorkspaceID, issueID)
	if err != nil {
		return PartnerStartOutput{}, err
	}
	if !found || issue.AppID != params.Identity.AppID || issue.PlatformID != params.Identity.PlatformID ||
		issue.PlatformUserID != params.Identity.PlatformUserID {
		return PartnerStartOutput{Status: PartnerStatusNotFound}, nil
	}

	if output, done := existingPartnerStartOutput(issue, now); done {
		return output, nil
	}
	if partnerIssueDeadlinePassed(issue, now) {
		issue, _, err = u.repository.ExpirePartnerIssue(
			mergedCtx,
			partnerIssueScope(issue),
			issue.ID,
			PartnerStatusExpired,
			nil,
			now,
		)
		if err != nil {
			return PartnerStartOutput{}, err
		}

		output, _ := existingPartnerStartOutput(issue, now)

		return output, nil
	}

	task := partnerIssueTask(issue, issue.Rewards, now)

	config, found, err := u.repository.GetPartnerConfig(
		mergedCtx,
		issue.WorkspaceID,
		issue.Provider,
		issue.GroupKey,
		issue.Platform,
	)
	if err != nil {
		return PartnerStartOutput{}, err
	}
	if !found || !config.IsEnabled {
		return PartnerStartOutput{Status: PartnerStatusNotConfigured, Task: &task}, nil
	}

	provider := u.partnerProvider(issue.Provider)
	starter, ok := provider.(PartnerStarter)
	if (!ok || starter == nil) && issue.StartMode != repository.StartModeRequired {
		return PartnerStartOutput{Status: PartnerStatusNotSupported, Task: &task}, nil
	}

	leaseToken := uuid.NewString()
	for {
		acquired, err := u.repository.AcquirePartnerIssueStartLease(
			mergedCtx,
			issue.WorkspaceID,
			issue.ID,
			leaseToken,
			u.partnerStartLeaseDuration,
		)
		if err != nil {
			return PartnerStartOutput{}, err
		}
		if acquired {
			break
		}

		issue, found, err = u.repository.GetPartnerIssue(
			mergedCtx,
			params.Identity.WorkspaceID,
			issueID,
		)
		if err != nil {
			return PartnerStartOutput{}, err
		}
		if !found {
			return PartnerStartOutput{Status: PartnerStatusNotFound}, nil
		}
		if output, done := existingPartnerStartOutput(issue, now); done {
			return output, nil
		}
		if partnerIssueDeadlinePassed(issue, now) {
			issue, _, err = u.repository.ExpirePartnerIssue(
				mergedCtx,
				partnerIssueScope(issue),
				issue.ID,
				PartnerStatusExpired,
				nil,
				now,
			)
			if err != nil {
				return PartnerStartOutput{}, err
			}

			output, _ := existingPartnerStartOutput(issue, now)

			return output, nil
		}

		timer := time.NewTimer(partnerStartPollDelay)
		select {
		case <-mergedCtx.Done():
			timer.Stop()

			return PartnerStartOutput{}, mergedCtx.Err()
		case <-timer.C:
		}
	}
	defer func() {
		u.releasePartnerIssueStartLease(
			mergedCtx,
			issue.WorkspaceID,
			issue.ID,
			leaseToken,
		)
	}()

	if !ok || starter == nil {
		updated, _, err := u.repository.UpdatePartnerIssueStart(
			mergedCtx,
			issue.WorkspaceID,
			issue.ID,
			leaseToken,
			"",
			nil,
			nil,
		)
		if err != nil {
			return PartnerStartOutput{}, err
		}

		output, _ := existingPartnerStartOutput(updated, now)

		return output, nil
	}

	started, err := u.startPartnerTaskWithLease(
		mergedCtx,
		starter,
		PartnerStartProviderParams{
			Identity:  params.Identity,
			Config:    config,
			Issue:     issue,
			Variables: params.Variables,
			Now:       now,
		},
		issue.WorkspaceID,
		issue.ID,
		leaseToken,
	)
	if err != nil {
		return PartnerStartOutput{}, err
	}
	if !started.Started {
		return PartnerStartOutput{
			Status:  started.Status,
			Started: false,
			Task:    &task,
		}, nil
	}

	publicPatch, err := partnerStartPublicPayloadPatch(
		started.PublicPayloadPatch,
		started.ActionURL,
	)
	if err != nil {
		return PartnerStartOutput{}, err
	}

	updated, changed, err := u.repository.UpdatePartnerIssueStart(
		mergedCtx,
		issue.WorkspaceID,
		issue.ID,
		leaseToken,
		started.ExternalClickID,
		publicPatch,
		started.PrivatePayloadPatch,
	)
	if err != nil {
		return PartnerStartOutput{}, err
	}
	if !changed {
		if output, done := existingPartnerStartOutput(updated, now); done {
			return output, nil
		}

		return PartnerStartOutput{Status: PartnerStatusNotCompleted}, nil
	}

	output, _ := existingPartnerStartOutput(updated, now)

	return output, nil

}

type partnerStartCall struct {
	result PartnerStartResult
	err    error
}

func (u *User) startPartnerTaskWithLease(
	ctx context.Context,
	starter PartnerStarter,
	params PartnerStartProviderParams,
	workspaceID string,
	issueID uint64,
	leaseToken string,
) (PartnerStartResult, error) {

	providerCtx, cancelProvider := context.WithCancel(ctx)
	defer cancelProvider()

	resultCh := make(chan partnerStartCall, 1)
	started := u.goroutines.Go("tasks.partner.start", func() {
		call := partnerStartCall{}
		defer func() {
			if recovered := recover(); recovered != nil {
				call = partnerStartCall{
					err: fmt.Errorf("tasks partner start provider panic: %v", recovered),
				}
			}
			resultCh <- call
		}()

		call.result, call.err = starter.StartPartnerTask(providerCtx, params)
	})
	if !started {
		return PartnerStartResult{}, context.Canceled
	}

	renewInterval := u.partnerStartLeaseDuration / 3
	if renewInterval <= 0 {
		renewInterval = time.Millisecond
	}
	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()

	for {
		select {
		case call := <-resultCh:
			return call.result, call.err
		case <-ticker.C:
			renewed, err := u.repository.RenewPartnerIssueStartLease(
				ctx,
				workspaceID,
				issueID,
				leaseToken,
				u.partnerStartLeaseDuration,
			)
			if err != nil {
				cancelProvider()

				return PartnerStartResult{}, err
			}
			if !renewed {
				cancelProvider()

				return PartnerStartResult{}, ErrPartnerStartLeaseLost
			}
		case <-ctx.Done():
			cancelProvider()

			return PartnerStartResult{}, ctx.Err()
		}
	}

}

func (u *User) releasePartnerIssueStartLease(
	ctx context.Context,
	workspaceID string,
	issueID uint64,
	leaseToken string,
) {

	releaseCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		partnerStartReleaseTimeout,
	)
	defer cancel()

	_ = u.repository.ReleasePartnerIssueStartLease(
		releaseCtx,
		workspaceID,
		issueID,
		leaseToken,
	)

}

func normalizePartnerStartLeaseDuration(value time.Duration) time.Duration {
	if value <= 0 {
		return defaultPartnerStartLeaseDuration
	}

	return value
}

func existingPartnerStartOutput(
	issue repository.PartnerIssue,
	now time.Time,
) (PartnerStartOutput, bool) {

	task := partnerIssueTask(issue, issue.Rewards, now)
	started := issue.StartedAt != nil
	actionURL := ""
	if started {
		actionURL = partnerIssueActionURL(issue)
	}

	switch issue.Status {
	case repository.PartnerIssueStatusExpired:
		return PartnerStartOutput{
			Status:    PartnerStatusExpired,
			Started:   started,
			ActionURL: actionURL,
			Task:      &task,
		}, true
	case repository.PartnerIssueStatusCompleted:
		return PartnerStartOutput{
			Status:    PartnerStatusReady,
			Started:   started,
			ActionURL: actionURL,
			Task:      &task,
		}, true
	case repository.PartnerIssueStatusClaimed,
		repository.PartnerIssueStatusRevoked,
		repository.PartnerIssueStatusRevokedAfterClaim:
		return PartnerStartOutput{
			Status:    issue.Status,
			Started:   started,
			ActionURL: actionURL,
			Task:      &task,
		}, true
	}

	if started {
		return PartnerStartOutput{
			Status:    PartnerStatusStarted,
			Started:   true,
			ActionURL: actionURL,
			Task:      &task,
		}, true
	}

	return PartnerStartOutput{}, false

}

func partnerStartPublicPayloadPatch(
	patch json.RawMessage,
	actionURL string,
) (json.RawMessage, error) {

	if len(patch) == 0 && actionURL == "" {
		return nil, nil
	}

	values := make(map[string]any)
	if len(patch) > 0 {
		if err := json.Unmarshal(patch, &values); err != nil {
			return nil, fmt.Errorf("tasks partner start public payload patch: %w", err)
		}
	}
	if values == nil {
		values = make(map[string]any)
	}
	if actionURL != "" {
		values["action_url"] = actionURL
	}

	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("tasks partner start public payload patch: %w", err)
	}

	return encoded, nil

}

func partnerIssueActionURL(issue repository.PartnerIssue) string {

	var payload struct {
		ActionURL string `json:"action_url"`
	}
	if len(issue.PublicPayload) == 0 || json.Unmarshal(issue.PublicPayload, &payload) != nil {
		return ""
	}

	return payload.ActionURL

}

func partnerIssueKey(config repository.PartnerConfig, identity Identity, external PartnerExternalTask) string {
	value := strings.Join([]string{
		config.Provider,
		config.GroupKey,
		config.Platform,
		strconv.FormatInt(identity.AppID, 10),
		strconv.FormatInt(identity.PlatformID, 10),
		identity.PlatformUserID,
		external.ExternalID,
		external.ExternalType,
		partnerIssueWindowKey(external),
	}, "\x00")
	sum := sha256.Sum256([]byte(value))

	return "v3:" + hex.EncodeToString(sum[:])
}

func partnerIssueWindowKey(external PartnerExternalTask) string {
	if external.ExpiresAt == nil {
		return "unlimited"
	}

	if windowKey := strings.TrimSpace(external.WindowKey); windowKey != "" {
		return "window:" + windowKey
	}

	return "expires:" + external.ExpiresAt.UTC().Format(time.RFC3339Nano)
}

func partnerIssueTask(issue repository.PartnerIssue, rewards []repository.Reward, now time.Time) TaskModel {
	status := repository.StatusOpen
	progressValue := uint64(0)
	switch issue.Status {
	case repository.PartnerIssueStatusCompleted:
		status = repository.StatusReady
		progressValue = 1
	case repository.PartnerIssueStatusClaimed:
		status = repository.StatusClaimed
		progressValue = 1
	case repository.PartnerIssueStatusExpired:
		status = repository.PartnerIssueStatusExpired
	case repository.PartnerIssueStatusRevoked, repository.PartnerIssueStatusRevokedAfterClaim:
		status = issue.Status
	}
	periodEnd := now
	if issue.ExpiresAt != nil {
		periodEnd = *issue.ExpiresAt
	}
	return TaskModel{
		ID: issue.ID, Key: repository.PartnerIssueKey(issue.ID), GroupKey: issue.GroupKey,
		TaskKind: repository.TaskKindPartner, ActionKey: "partner:" + issue.Provider,
		ActionKind: repository.ActionKindExternal, ClaimMode: repository.ClaimModeManual,
		StartMode: issue.StartMode, TargetCount: 1, Payload: issue.PublicPayload, Rewards: rewards,
		Progress: &repository.ActiveProgress{
			Progress: progressValue, Status: status,
			PeriodStartAt: issue.IssuedAt, PeriodEndAt: periodEnd,
			ReadyAt: issue.CompletedAt, ClaimedAt: issue.ClaimedAt,
		},
	}
}

func partnerIssueScope(issue repository.PartnerIssue) repository.PartnerIssueScope {
	return repository.PartnerIssueScope{
		WorkspaceID: issue.WorkspaceID,
		Provider:    issue.Provider,
		GroupKey:    issue.GroupKey,
		Platform:    issue.Platform,
	}
}

func partnerIssueDeadlinePassed(issue repository.PartnerIssue, now time.Time) bool {
	return issue.ExpiresAt != nil && !now.Before(*issue.ExpiresAt)
}
