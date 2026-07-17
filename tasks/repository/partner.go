package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	serviceerrors "github.com/elum2b/services/errors"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/internal/utils/target"
	tasksqlc "github.com/elum2b/services/tasks/sqlc"
)

var ErrPartnerIssueAmbiguous = serviceerrors.New(
	serviceerrors.CodeConflict,
	"tasks partner issue lookup is ambiguous",
)

type PartnerIssueExternalUserLookup struct {
	WorkspaceID    string
	Provider       string
	GroupKey       string
	Platform       string
	ExternalID     string
	PlatformUserID string
	AppID          int64
	PlatformID     int64
}

type PartnerIssuePrivatePayloadLookup struct {
	WorkspaceID    string
	Provider       string
	GroupKey       string
	Platform       string
	LookupKey      string
	LookupValue    string
	PlatformUserID string
	AppID          int64
	PlatformID     int64
}

func PartnerIssueKey(id uint64) string {
	return PartnerIssueKeyPrefix + strconv.FormatUint(id, 10)
}

func ParsePartnerIssueRef(value string) (uint64, bool) {
	if !strings.HasPrefix(value, PartnerIssueKeyPrefix) {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(value, PartnerIssueKeyPrefix), 10, 64)
	return id, err == nil && id > 0 && id <= math.MaxInt64
}

func (r *Repository) SavePartnerConfig(ctx context.Context, params SavePartnerConfigParams) error {
	if err := requireWorkspaceID(params.WorkspaceID); err != nil {
		return err
	}
	if strings.TrimSpace(params.Provider) == "" ||
		strings.TrimSpace(params.GroupKey) == "" ||
		strings.TrimSpace(params.Platform) == "" {
		return fmt.Errorf("tasks partner config scope is required")
	}
	if err := target.Validate(params.Target); err != nil {
		return fmt.Errorf("tasks partner config target: %w", err)
	}
	if len(params.Settings) > 0 && !json.Valid(params.Settings) {
		return fmt.Errorf("tasks partner config settings must be valid JSON")
	}

	target := params.Target
	if len(target) == 0 {
		target = []byte("null")
	}
	settings := params.Settings
	if len(settings) == 0 {
		settings = []byte("{}")
	}
	if err := r.withWorkspaceMutation(ctx, params.WorkspaceID, func(txRepo *Repository) error {
		return txRepo.q.AdminUpsertPartnerConfig(ctx, tasksqlc.AdminUpsertPartnerConfigParams{
			WorkspaceID:   params.WorkspaceID,
			Provider:      params.Provider,
			GroupKey:      params.GroupKey,
			Platform:      params.Platform,
			IsEnabled:     params.IsEnabled,
			Secret:        nullString(params.Secret),
			WebhookSecret: nullString(params.WebhookSecret),
			Target:        rawMessageParam(target),
			Settings:      rawMessageParam(settings),
		})
	}); err != nil {
		return err
	}

	return r.bumpPartnerConfigCache(params.WorkspaceID)
}

func (r *Repository) GetPartnerConfig(
	ctx context.Context,
	workspaceID, provider, groupKey, platform string,
) (PartnerConfig, bool, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return PartnerConfig{}, false, err
	}

	config, err := repositoryQuery(ctx, r, sqlwrap.Params{
		Key:               partnerConfigCacheKey(workspaceID, provider, groupKey, platform),
		CacheL1Delay:      r.cacheL1Delay,
		CacheL2Delay:      r.cacheL2Delay,
		CacheVersionScope: partnerConfigCacheScope(workspaceID),
	}, func(ctx context.Context) (PartnerConfig, error) {
		row, err := r.q.AdminGetPartnerConfig(ctx, tasksqlc.AdminGetPartnerConfigParams{
			WorkspaceID: workspaceID, Provider: provider, GroupKey: groupKey, Platform: platform,
		})
		if err != nil {
			return PartnerConfig{}, err
		}
		return mapPartnerConfig(row), nil
	})
	if err != nil {
		if isNoRows(err) {
			return PartnerConfig{}, false, nil
		}
		return PartnerConfig{}, false, err
	}
	return config, true, nil
}

func (r *Repository) GetPartnerConfigByWebhookSecret(
	ctx context.Context,
	workspaceID, secret string,
) (PartnerConfig, bool, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return PartnerConfig{}, false, err
	}

	config, err := repositoryQuery(ctx, r, sqlwrap.Params{
		Key:               partnerConfigWebhookCacheKey(workspaceID, secret),
		CacheL1Delay:      r.cacheL1Delay,
		CacheL2Delay:      r.cacheL2Delay,
		CacheVersionScope: partnerConfigCacheScope(workspaceID),
	}, func(ctx context.Context) (PartnerConfig, error) {
		row, err := r.q.GetPartnerConfigByWebhookSecret(ctx, tasksqlc.GetPartnerConfigByWebhookSecretParams{
			WorkspaceID: workspaceID, WebhookSecret: sql.NullString{String: secret, Valid: secret != ""},
		})
		if err != nil {
			return PartnerConfig{}, err
		}
		return mapPartnerConfig(row), nil
	})
	if err != nil {
		if isNoRows(err) {
			return PartnerConfig{}, false, nil
		}
		return PartnerConfig{}, false, err
	}
	return config, true, nil
}

func (r *Repository) ListPartnerConfigs(ctx context.Context, workspaceID string) ([]PartnerConfig, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	return repositoryQuery(ctx, r, sqlwrap.Params{
		Key:               partnerConfigListCacheKey(workspaceID),
		CacheL1Delay:      r.cacheL1Delay,
		CacheL2Delay:      r.cacheL2Delay,
		CacheVersionScope: partnerConfigCacheScope(workspaceID),
	}, func(ctx context.Context) ([]PartnerConfig, error) {
		rows, err := r.q.AdminListPartnerConfigs(ctx, workspaceID)
		if err != nil {
			return nil, err
		}
		return mapPartnerConfigs(rows), nil
	})
}

func (r *Repository) WarmPartnerConfigCache(ctx context.Context) ([]PartnerConfig, error) {
	rows, err := repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskPartnerConfig, error) {
		return r.q.ListAllPartnerConfigs(ctx)
	})
	if err != nil {
		if isMissingPartnerConfigTable(err) {
			return nil, nil
		}
		return nil, err
	}
	configs := mapPartnerConfigs(rows)
	byWorkspace := make(map[string][]PartnerConfig)
	for _, config := range configs {
		byWorkspace[config.WorkspaceID] = append(byWorkspace[config.WorkspaceID], config)
		if _, err := repositoryQuery(ctx, r, sqlwrap.Params{
			Key:               partnerConfigCacheKey(config.WorkspaceID, config.Provider, config.GroupKey, config.Platform),
			CacheL1Delay:      r.cacheL1Delay,
			CacheL2Delay:      r.cacheL2Delay,
			CacheVersionScope: partnerConfigCacheScope(config.WorkspaceID),
		}, func(context.Context) (PartnerConfig, error) {
			return config, nil
		}); err != nil {
			return nil, err
		}
		if config.WebhookSecret != nil && strings.TrimSpace(*config.WebhookSecret) != "" {
			if _, err := repositoryQuery(ctx, r, sqlwrap.Params{
				Key:               partnerConfigWebhookCacheKey(config.WorkspaceID, *config.WebhookSecret),
				CacheL1Delay:      r.cacheL1Delay,
				CacheL2Delay:      r.cacheL2Delay,
				CacheVersionScope: partnerConfigCacheScope(config.WorkspaceID),
			}, func(context.Context) (PartnerConfig, error) {
				return config, nil
			}); err != nil {
				return nil, err
			}
		}
	}
	for workspaceID, workspaceConfigs := range byWorkspace {
		configs := workspaceConfigs
		if _, err := repositoryQuery(ctx, r, sqlwrap.Params{
			Key:               partnerConfigListCacheKey(workspaceID),
			CacheL1Delay:      r.cacheL1Delay,
			CacheL2Delay:      r.cacheL2Delay,
			CacheVersionScope: partnerConfigCacheScope(workspaceID),
		}, func(context.Context) ([]PartnerConfig, error) {
			return configs, nil
		}); err != nil {
			return nil, err
		}
	}
	return configs, nil
}

func (r *Repository) SavePartnerScript(ctx context.Context, params SavePartnerScriptParams) error {
	if params.Version == "" {
		params.Version = time.Now().UTC().Format("20060102150405.000000000")
	}
	if err := repositoryExec(ctx, r, func(ctx context.Context) error {
		return r.q.AdminUpsertPartnerScript(ctx, tasksqlc.AdminUpsertPartnerScriptParams{
			Provider:  params.Provider,
			IsEnabled: params.IsEnabled,
			Version:   params.Version,
			Source:    params.Source,
		})
	}); err != nil {
		return err
	}
	return r.bumpPartnerScriptCache()
}

func (r *Repository) GetPartnerScript(ctx context.Context, provider string) (PartnerScript, bool, error) {
	script, err := repositoryQuery(ctx, r, sqlwrap.Params{
		Key:               partnerScriptCacheKey(provider),
		CacheL1Delay:      r.cacheL1Delay,
		CacheL2Delay:      r.cacheL2Delay,
		CacheVersionScope: partnerScriptCacheScope(),
	}, func(ctx context.Context) (PartnerScript, error) {
		row, err := r.q.AdminGetPartnerScript(ctx, provider)
		if err != nil {
			return PartnerScript{}, err
		}
		return mapPartnerScript(row), nil
	})
	if err != nil {
		if isNoRows(err) || isMissingPartnerScriptTable(err) {
			return PartnerScript{}, false, nil
		}
		return PartnerScript{}, false, err
	}
	return script, true, nil
}

func (r *Repository) GetEnabledPartnerScript(ctx context.Context, provider string) (PartnerScript, bool, error) {
	script, err := repositoryQuery(ctx, r, sqlwrap.Params{
		Key:               partnerScriptCacheKey(provider),
		CacheL1Delay:      r.cacheL1Delay,
		CacheL2Delay:      r.cacheL2Delay,
		CacheVersionScope: partnerScriptCacheScope(),
	}, func(ctx context.Context) (PartnerScript, error) {
		row, err := r.q.GetEnabledPartnerScript(ctx, provider)
		if err != nil {
			return PartnerScript{}, err
		}
		return mapPartnerScript(row), nil
	})
	if err != nil {
		if isNoRows(err) || isMissingPartnerScriptTable(err) {
			return PartnerScript{}, false, nil
		}
		return PartnerScript{}, false, err
	}
	return script, true, nil
}

func (r *Repository) ListPartnerScripts(ctx context.Context) ([]PartnerScript, error) {
	return repositoryQuery(ctx, r, sqlwrap.Params{
		Key:               partnerScriptListCacheKey(),
		CacheL1Delay:      r.cacheL1Delay,
		CacheL2Delay:      r.cacheL2Delay,
		CacheVersionScope: partnerScriptCacheScope(),
	}, func(ctx context.Context) ([]PartnerScript, error) {
		rows, err := r.q.AdminListPartnerScripts(ctx)
		if err != nil {
			if isMissingPartnerScriptTable(err) {
				return nil, nil
			}
			return nil, err
		}
		return mapPartnerScripts(rows), nil
	})
}

func (r *Repository) SavePartnerRewardRule(ctx context.Context, params SavePartnerRewardRuleParams) error {
	if err := requireWorkspaceID(params.WorkspaceID); err != nil {
		return err
	}
	if strings.TrimSpace(params.Provider) == "" ||
		strings.TrimSpace(params.GroupKey) == "" {
		return fmt.Errorf("tasks partner reward rule scope is required")
	}
	if err := validateRewardDefinition(ExportReward{
		Key:      params.Reward.Key,
		Type:     params.Reward.Type,
		Quantity: params.Reward.Quantity,
		Scale:    params.Reward.Scale,
		Unit:     params.Reward.Unit,
		Position: params.Position,
	}); err != nil {
		return fmt.Errorf("tasks partner reward rule: %w", err)
	}

	externalType := params.ExternalType
	if externalType == "" {
		externalType = "*"
	}
	rewardType := params.Reward.Type
	if rewardType == "" {
		rewardType = "quantity"
	}
	return r.withWorkspaceMutation(ctx, params.WorkspaceID, func(txRepo *Repository) error {
		return txRepo.q.AdminUpsertPartnerRewardRule(ctx, tasksqlc.AdminUpsertPartnerRewardRuleParams{
			WorkspaceID:  params.WorkspaceID,
			Provider:     params.Provider,
			GroupKey:     params.GroupKey,
			ExternalType: externalType,
			RewardKey:    params.Reward.Key,
			RewardType:   rewardType,
			Quantity:     params.Reward.Quantity,
			Scale:        int16(params.Reward.Scale),
			DurationUnit: sql.NullString{
				String: taskStringValue(params.Reward.Unit),
				Valid:  params.Reward.Unit != nil,
			},
			Position:  params.Position,
			IsEnabled: params.IsEnabled,
		})
	})
}

func (r *Repository) DeletePartnerRewardRule(
	ctx context.Context,
	workspaceID, provider, groupKey, externalType, rewardKey string,
) (int64, error) {
	if externalType == "" {
		externalType = "*"
	}
	var rows int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		var err error
		rows, err = txRepo.q.AdminDeletePartnerRewardRule(ctx, tasksqlc.AdminDeletePartnerRewardRuleParams{
			WorkspaceID:  workspaceID,
			Provider:     provider,
			GroupKey:     groupKey,
			ExternalType: externalType,
			RewardKey:    rewardKey,
		})
		return err
	})
	return rows, err
}

func (r *Repository) PartnerRewards(
	ctx context.Context,
	workspaceID, provider, groupKey, externalType string,
) ([]Reward, error) {
	rows, err := repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskPartnerRewardRule, error) {
		return r.q.ListPartnerRewardRules(ctx, tasksqlc.ListPartnerRewardRulesParams{
			WorkspaceID: workspaceID, Provider: provider, GroupKey: groupKey,
			ExternalType: externalType, ExternalType_2: externalType,
		})
	})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(rows))
	rewards := make([]Reward, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.RewardKey]; ok {
			continue
		}
		seen[row.RewardKey] = struct{}{}
		rewards = append(rewards, Reward{
			Key: row.RewardKey, Type: string(row.RewardType), Quantity: row.Quantity,
			Scale: uint16(row.Scale),
			Unit:  nullPartnerDurationUnit(row.DurationUnit),
		})
	}
	return rewards, nil
}

func (r *Repository) CreatePartnerIssue(
	ctx context.Context,
	params CreatePartnerIssueParams,
) (PartnerIssue, bool, error) {
	now := params.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	publicPayload := params.PublicPayload
	if len(publicPayload) == 0 {
		publicPayload = []byte("{}")
	}
	privatePayload := params.PrivatePayload
	if len(privatePayload) == 0 {
		privatePayload = []byte("{}")
	}
	rewards := params.Rewards
	if rewards == nil {
		rewards = []Reward{}
	}
	rewardsSnapshot, err := json.Marshal(rewards)
	if err != nil {
		return PartnerIssue{}, false, err
	}
	issueKey := params.IssueKey
	if issueKey == "" {
		issueKey = fmt.Sprintf(
			"%s:%s:%s:%d:%d:%s:%s",
			params.Provider,
			params.GroupKey,
			params.ExternalID,
			params.Identity.AppID,
			params.Identity.PlatformID,
			params.Identity.PlatformUserID,
			now.Format("20060102"),
		)
	}
	startMode := params.StartMode
	if startMode == "" {
		startMode = StartModeNone
	}
	var issue PartnerIssue
	var inserted bool
	err = r.WithTx(ctx, func(txRepo *Repository) error {
		id, err := txRepo.q.CreatePartnerIssue(ctx, tasksqlc.CreatePartnerIssueParams{
			WorkspaceID:     params.Identity.WorkspaceID,
			Provider:        params.Provider,
			GroupKey:        params.GroupKey,
			Platform:        params.Platform,
			ExternalID:      params.ExternalID,
			ExternalType:    params.ExternalType,
			ExternalClickID: nullString(params.ExternalClickID),
			StartMode:       startMode,
			IssueKey:        issueKey,
			AppID:           params.Identity.AppID,
			PlatformID:      params.Identity.PlatformID,
			PlatformUserID:  params.Identity.PlatformUserID,
			PublicPayload:   rawMessageParam(publicPayload),
			PrivatePayload:  rawMessageParam(privatePayload),
			RewardsSnapshot: rewardsSnapshot,
			IssuedAt:        now,
			ExpiresAt:       nullTime(params.ExpiresAt),
		})
		if err != nil {
			return err
		}

		row, err := txRepo.q.GetPartnerIssueByID(ctx, tasksqlc.GetPartnerIssueByIDParams{
			WorkspaceID: params.Identity.WorkspaceID,
			ID:          id,
		})
		if err != nil {
			return err
		}
		issue = mapPartnerIssue(row)

		eventKey := "partner.issue:" + issue.IssueKey
		inserted, err = txRepo.recordPartnerStatsEvent(
			ctx,
			issue,
			PartnerStatsEventIssued,
			eventKey,
			"",
			issue.PublicPayload,
			now,
		)
		return err
	})
	return issue, inserted, err
}

func (r *Repository) GetPartnerIssue(ctx context.Context, workspaceID string, id uint64) (PartnerIssue, bool, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return PartnerIssue{}, false, err
	}

	row, err := repositoryValue(ctx, r, func(ctx context.Context) (tasksqlc.TaskPartnerIssue, error) {
		return r.q.GetPartnerIssueByID(ctx, tasksqlc.GetPartnerIssueByIDParams{WorkspaceID: workspaceID, ID: int64(id)})
	})
	if err != nil {
		if isNoRows(err) {
			return PartnerIssue{}, false, nil
		}
		return PartnerIssue{}, false, err
	}
	return mapPartnerIssue(row), true, nil
}

func (r *Repository) GetPartnerIssueByExternalClickID(
	ctx context.Context,
	workspaceID, provider, externalClickID string,
) (PartnerIssue, bool, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return PartnerIssue{}, false, err
	}

	row, err := repositoryValue(ctx, r, func(ctx context.Context) (tasksqlc.TaskPartnerIssue, error) {
		return r.q.GetPartnerIssueByExternalClickID(ctx, tasksqlc.GetPartnerIssueByExternalClickIDParams{
			WorkspaceID: workspaceID, Provider: provider, ExternalClickID: sql.NullString{String: externalClickID, Valid: externalClickID != ""},
		})
	})
	if err != nil {
		if isNoRows(err) {
			return PartnerIssue{}, false, nil
		}
		return PartnerIssue{}, false, err
	}
	return mapPartnerIssue(row), true, nil
}

func (r *Repository) GetPartnerIssueByExternalUser(
	ctx context.Context,
	params PartnerIssueExternalUserLookup,
) (PartnerIssue, bool, error) {
	if err := requireWorkspaceID(params.WorkspaceID); err != nil {
		return PartnerIssue{}, false, err
	}

	rows, err := repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskPartnerIssue, error) {
		return r.q.GetPartnerIssuesByExternalUser(ctx, tasksqlc.GetPartnerIssuesByExternalUserParams{
			WorkspaceID:    params.WorkspaceID,
			Provider:       params.Provider,
			GroupKey:       params.GroupKey,
			Platform:       params.Platform,
			ExternalID:     params.ExternalID,
			PlatformUserID: params.PlatformUserID,
			AppID:          params.AppID,
			PlatformID:     params.PlatformID,
		})
	})
	if err != nil {
		return PartnerIssue{}, false, err
	}
	if len(rows) == 0 {
		return PartnerIssue{}, false, nil
	}
	if len(rows) > 1 {
		return PartnerIssue{}, false, ErrPartnerIssueAmbiguous
	}

	return mapPartnerIssue(rows[0]), true, nil
}

func (r *Repository) GetPartnerIssueByPrivatePayloadUser(
	ctx context.Context,
	params PartnerIssuePrivatePayloadLookup,
) (PartnerIssue, bool, error) {
	if err := requireWorkspaceID(params.WorkspaceID); err != nil {
		return PartnerIssue{}, false, err
	}

	rows, err := repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskPartnerIssue, error) {
		return r.q.GetPartnerIssuesByPrivatePayloadUser(ctx, tasksqlc.GetPartnerIssuesByPrivatePayloadUserParams{
			WorkspaceID:    params.WorkspaceID,
			Provider:       params.Provider,
			GroupKey:       params.GroupKey,
			Platform:       params.Platform,
			LookupKey:      params.LookupKey,
			LookupValue:    params.LookupValue,
			PlatformUserID: params.PlatformUserID,
			AppID:          params.AppID,
			PlatformID:     params.PlatformID,
		})
	})
	if err != nil {
		return PartnerIssue{}, false, err
	}
	if len(rows) == 0 {
		return PartnerIssue{}, false, nil
	}
	if len(rows) > 1 {
		return PartnerIssue{}, false, ErrPartnerIssueAmbiguous
	}

	return mapPartnerIssue(rows[0]), true, nil
}

func (r *Repository) UpdatePartnerIssueStart(
	ctx context.Context,
	workspaceID string,
	id uint64,
	leaseToken string,
	externalClickID string,
	publicPatch, privatePatch json.RawMessage,
) (PartnerIssue, bool, error) {

	issue, found, err := r.GetPartnerIssue(ctx, workspaceID, id)
	if err != nil || !found {
		return issue, false, err
	}

	publicPayload := mergeRawObjects(issue.PublicPayload, publicPatch)
	privatePayload := mergeRawObjects(issue.PrivatePayload, privatePatch)

	changed := false
	err = r.WithTx(ctx, func(txRepo *Repository) error {
		affected, err := txRepo.q.UpdatePartnerIssueStart(ctx, tasksqlc.UpdatePartnerIssueStartParams{
			ExternalClickID: externalClickID,
			PublicPayload:   publicPayload,
			PrivatePayload:  privatePayload,
			WorkspaceID:     workspaceID,
			IssueID:         int64(id),
			LeaseToken:      leaseToken,
		})
		if err != nil {
			return err
		}

		changed = affected == 1
		_, err = txRepo.q.ReleasePartnerIssueStartLease(
			ctx,
			tasksqlc.ReleasePartnerIssueStartLeaseParams{
				WorkspaceID: workspaceID,
				IssueID:     int64(id),
				LeaseToken:  leaseToken,
			},
		)

		return err
	})
	if err != nil {
		return issue, false, err
	}

	updated, found, err := r.GetPartnerIssue(ctx, workspaceID, id)
	if err != nil || !found {
		return issue, false, err
	}

	return updated, changed, nil

}

func (r *Repository) AcquirePartnerIssueStartLease(
	ctx context.Context,
	workspaceID string,
	id uint64,
	leaseToken string,
	leaseDuration time.Duration,
) (bool, error) {

	leaseDurationMilliseconds := leaseDuration.Milliseconds()
	if leaseDurationMilliseconds <= 0 {
		leaseDurationMilliseconds = 1
	}

	acquiredToken, err := repositoryValue(ctx, r, func(ctx context.Context) (string, error) {
		return r.q.AcquirePartnerIssueStartLease(
			ctx,
			tasksqlc.AcquirePartnerIssueStartLeaseParams{
				WorkspaceID:               workspaceID,
				IssueID:                   int64(id),
				LeaseToken:                leaseToken,
				LeaseDurationMilliseconds: leaseDurationMilliseconds,
			},
		)
	})
	if isNoRows(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return acquiredToken == leaseToken, nil

}

func (r *Repository) RenewPartnerIssueStartLease(
	ctx context.Context,
	workspaceID string,
	id uint64,
	leaseToken string,
	leaseDuration time.Duration,
) (bool, error) {

	leaseDurationMilliseconds := leaseDuration.Milliseconds()
	if leaseDurationMilliseconds <= 0 {
		leaseDurationMilliseconds = 1
	}

	affected, err := repositoryValue(ctx, r, func(ctx context.Context) (int64, error) {
		return r.q.RenewPartnerIssueStartLease(
			ctx,
			tasksqlc.RenewPartnerIssueStartLeaseParams{
				LeaseDurationMilliseconds: leaseDurationMilliseconds,
				WorkspaceID:               workspaceID,
				IssueID:                   int64(id),
				LeaseToken:                leaseToken,
			},
		)
	})
	if err != nil {
		return false, err
	}

	return affected == 1, nil

}

func (r *Repository) ReleasePartnerIssueStartLease(
	ctx context.Context,
	workspaceID string,
	id uint64,
	leaseToken string,
) error {

	_, err := repositoryValue(ctx, r, func(ctx context.Context) (int64, error) {
		return r.q.ReleasePartnerIssueStartLease(
			ctx,
			tasksqlc.ReleasePartnerIssueStartLeaseParams{
				WorkspaceID: workspaceID,
				IssueID:     int64(id),
				LeaseToken:  leaseToken,
			},
		)
	})

	return err

}

func (r *Repository) ListPartnerIssuesForUser(
	ctx context.Context,
	identity Identity,
	provider, groupKey, platform string,
	now time.Time,
) ([]PartnerIssue, error) {
	if err := identity.Validate(); err != nil {
		return nil, err
	}

	if now.IsZero() {
		now = time.Now().UTC()
	}
	rows, err := repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskPartnerIssue, error) {
		return r.q.ListPartnerIssuesForUser(ctx, tasksqlc.ListPartnerIssuesForUserParams{
			WorkspaceID: identity.WorkspaceID, Provider: provider, GroupKey: groupKey, Platform: platform,
			AppID: identity.AppID, PlatformID: identity.PlatformID, PlatformUserID: identity.PlatformUserID,
			ExpiresAt: sql.NullTime{Time: now, Valid: true},
		})
	})
	if err != nil {
		return nil, err
	}
	result := make([]PartnerIssue, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapPartnerIssue(row))
	}
	return result, nil
}

func (r *Repository) CompletePartnerIssue(
	ctx context.Context,
	scope PartnerIssueScope,
	id uint64,
	status string,
	payload json.RawMessage,
	now time.Time,
) (PartnerIssue, bool, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var issue PartnerIssue
	completed := false
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		row, err := txRepo.q.GetPartnerIssueByIDForUpdate(
			ctx,
			tasksqlc.GetPartnerIssueByIDForUpdateParams{
				WorkspaceID: scope.WorkspaceID,
				ID:          int64(id),
			},
		)
		if err != nil {
			return err
		}
		issue = mapPartnerIssue(row)
		if !scope.matches(issue) {
			return sql.ErrNoRows
		}
		if issue.Status == PartnerIssueStatusCompleted || issue.Status == PartnerIssueStatusClaimed {
			return nil
		}
		if issue.Status == PartnerIssueStatusExpired {
			return nil
		}
		if partnerIssueDeadlinePassed(issue, now) {
			var expireErr error
			issue, _, expireErr = txRepo.expirePartnerIssueLocked(
				ctx,
				issue,
				status,
				payload,
				now,
			)
			return expireErr
		}
		if issue.StartMode == StartModeRequired && issue.StartedAt == nil {
			startRequested, err := txRepo.q.HasActivePartnerIssueStartLease(
				ctx,
				tasksqlc.HasActivePartnerIssueStartLeaseParams{
					WorkspaceID: issue.WorkspaceID,
					IssueID:     int64(issue.ID),
				},
			)
			if err != nil {
				return err
			}
			if !startRequested {
				return nil
			}
		}

		affected, err := txRepo.q.CompletePartnerIssue(ctx, tasksqlc.CompletePartnerIssueParams{
			CompletedAt: nullTime(&now),
			WorkspaceID: scope.WorkspaceID,
			ID:          int64(id),
		})
		if err != nil {
			return err
		}
		completed = affected == 1
		if completed {
			issue.Status = PartnerIssueStatusCompleted
			issue.CompletedAt = &now
			eventKey := fmt.Sprintf("partner.completed:%d", issue.ID)
			_, err = txRepo.recordPartnerStatsEvent(
				ctx,
				issue,
				PartnerStatsEventCompleted,
				eventKey,
				status,
				payload,
				now,
			)
			return err
		}
		return nil
	})
	if err != nil {
		if errorsIsNoRows(err) {
			return PartnerIssue{}, false, nil
		}
		return PartnerIssue{}, false, err
	}
	return issue, completed, nil
}

func (r *Repository) ExpirePartnerIssue(
	ctx context.Context,
	scope PartnerIssueScope,
	id uint64,
	status string,
	payload json.RawMessage,
	now time.Time,
) (PartnerIssue, bool, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	var issue PartnerIssue
	expired := false
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		row, err := txRepo.q.GetPartnerIssueByIDForUpdate(
			ctx,
			tasksqlc.GetPartnerIssueByIDForUpdateParams{
				WorkspaceID: scope.WorkspaceID,
				ID:          int64(id),
			},
		)
		if err != nil {
			return err
		}

		issue = mapPartnerIssue(row)
		if !scope.matches(issue) {
			return sql.ErrNoRows
		}
		if issue.Status == PartnerIssueStatusExpired {
			return nil
		}
		if issue.Status != PartnerIssueStatusIssued || !partnerIssueDeadlinePassed(issue, now) {
			return nil
		}

		issue, expired, err = txRepo.expirePartnerIssueLocked(
			ctx,
			issue,
			status,
			payload,
			now,
		)
		return err
	})
	if err != nil {
		if errorsIsNoRows(err) {
			return PartnerIssue{}, false, nil
		}
		return PartnerIssue{}, false, err
	}

	return issue, expired, nil
}

func (r *Repository) expirePartnerIssueLocked(
	ctx context.Context,
	issue PartnerIssue,
	status string,
	payload json.RawMessage,
	now time.Time,
) (PartnerIssue, bool, error) {
	affected, err := r.q.ExpirePartnerIssue(ctx, tasksqlc.ExpirePartnerIssueParams{
		WorkspaceID: issue.WorkspaceID,
		ID:          int64(issue.ID),
	})
	if err != nil || affected == 0 {
		return issue, false, err
	}

	issue.Status = PartnerIssueStatusExpired
	eventKey := fmt.Sprintf("partner.expired:%d", issue.ID)
	_, err = r.recordPartnerStatsEvent(
		ctx,
		issue,
		PartnerStatsEventExpired,
		eventKey,
		status,
		payload,
		now,
	)
	if err != nil {
		return PartnerIssue{}, false, err
	}

	return issue, true, nil
}

func (r *Repository) RevokePartnerIssue(
	ctx context.Context,
	scope PartnerIssueScope,
	id uint64,
	status string,
	payload json.RawMessage,
	now time.Time,
) (PartnerIssue, bool, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var issue PartnerIssue
	revoked := false
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		row, err := txRepo.q.GetPartnerIssueByIDForUpdate(
			ctx,
			tasksqlc.GetPartnerIssueByIDForUpdateParams{
				WorkspaceID: scope.WorkspaceID,
				ID:          int64(id),
			},
		)
		if err != nil {
			return err
		}
		issue = mapPartnerIssue(row)
		if !scope.matches(issue) {
			return sql.ErrNoRows
		}
		if issue.Status == PartnerIssueStatusRevoked || issue.Status == PartnerIssueStatusRevokedAfterClaim {
			return nil
		}
		revokedStatus := PartnerIssueStatusRevoked
		eventType := PartnerStatsEventRevoked
		if issue.Status == PartnerIssueStatusClaimed {
			revokedStatus = PartnerIssueStatusRevokedAfterClaim
			eventType = PartnerStatsEventRevokedAfterClaim
		} else if issue.Status != PartnerIssueStatusIssued && issue.Status != PartnerIssueStatusCompleted {
			return nil
		}
		affected, err := txRepo.q.RevokePartnerIssue(ctx, tasksqlc.RevokePartnerIssueParams{
			WorkspaceID: scope.WorkspaceID,
			ID:          int64(id),
		})
		if err != nil {
			return err
		}
		revoked = affected == 1
		if !revoked {
			return nil
		}
		issue.Status = revokedStatus
		eventKey := fmt.Sprintf("partner.%s:%d", eventType, issue.ID)
		if _, err = txRepo.recordPartnerStatsEvent(ctx, issue, eventType, eventKey, status, payload, now); err != nil {
			return err
		}
		if revokedStatus != PartnerIssueStatusRevokedAfterClaim {
			return nil
		}
		operationID := ""
		grant, err := txRepo.q.GetPartnerRewardGrantByIssue(ctx, tasksqlc.GetPartnerRewardGrantByIssueParams{
			WorkspaceID: scope.WorkspaceID,
			IssueID:     int64(id),
		})
		if err != nil && !isNoRows(err) {
			return err
		}
		if err == nil {
			operationID = grant.OperationID
		}
		callbackPayload, err := partnerCallbackPayload(issue, operationID, now)
		if err != nil {
			return err
		}
		callbackEventKey := fmt.Sprintf("tasks.partner.revoked:%d", issue.ID)
		_, err = txRepo.callbacks.CreateEvent(ctx, callbackutil.CreateParams{
			WorkspaceID: issue.WorkspaceID, SourceService: "tasks", EventType: CallbackEventRevoked,
			EventKey: callbackEventKey, IdempotencyKey: callbackEventKey,
			Payload: callbackPayload, NextAttemptAt: now,
		})
		return err
	})
	if err != nil {
		if errorsIsNoRows(err) {
			return PartnerIssue{}, false, nil
		}
		return PartnerIssue{}, false, err
	}
	return issue, revoked, nil
}

func (r *Repository) ClaimPartnerIssue(
	ctx context.Context,
	identity Identity,
	issueID uint64,
	operationID string,
	now time.Time,
) (PartnerClaimResult, error) {
	if err := identity.Validate(); err != nil {
		return PartnerClaimResult{}, err
	}

	operationID, err := validateOperationID(operationID)
	if err != nil {
		return PartnerClaimResult{}, err
	}

	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := PartnerClaimResult{Status: ClaimStatusNotFound, OperationID: operationID}
	err = r.WithTx(ctx, func(txRepo *Repository) error {
		row, err := txRepo.q.GetPartnerIssueByIDForUpdate(ctx, tasksqlc.GetPartnerIssueByIDForUpdateParams{
			WorkspaceID: identity.WorkspaceID, ID: int64(issueID),
		})
		if err != nil {
			if isNoRows(err) {
				return nil
			}
			return err
		}
		issue := mapPartnerIssue(row)
		result.Issue = issue
		if issue.AppID != identity.AppID || issue.PlatformID != identity.PlatformID ||
			issue.PlatformUserID != identity.PlatformUserID {
			result.Status = ClaimStatusNotFound
			return nil
		}
		if issue.Status == PartnerIssueStatusClaimed {
			grant, err := txRepo.q.GetPartnerRewardGrantByIssue(
				ctx,
				tasksqlc.GetPartnerRewardGrantByIssueParams{
					WorkspaceID: issue.WorkspaceID,
					IssueID:     int64(issue.ID),
				},
			)
			if err != nil {
				return err
			}
			if grant.OperationID != operationID {
				return ErrOperationIDConflict
			}

			result.Status = ClaimStatusAlreadyDone
			return nil
		}
		if issue.Status == PartnerIssueStatusExpired {
			result.Status = ClaimStatusExpired
			return nil
		}
		if partnerIssueMustExpireBeforeClaim(issue, now) {
			issue, _, err = txRepo.expirePartnerIssueLocked(
				ctx,
				issue,
				PartnerIssueStatusExpired,
				nil,
				now,
			)
			if err != nil {
				return err
			}
			result.Issue = issue
			result.Status = ClaimStatusExpired
			return nil
		}

		if issue.StartMode == StartModeRequired && issue.StartedAt == nil {
			result.Status = ClaimStatusNotStarted
			return nil
		}
		if issue.Status != PartnerIssueStatusCompleted {
			result.Status = ClaimStatusNotReady
			return nil
		}

		reserved, err := txRepo.q.ReserveRewardOperation(ctx, tasksqlc.ReserveRewardOperationParams{
			WorkspaceID: issue.WorkspaceID,
			OperationID: operationID,
			SourceKind:  "partner_issue",
			SourceID:    int64(issue.ID),
		})
		if err != nil {
			return err
		}
		if reserved != 1 {
			return ErrOperationIDConflict
		}

		rewards := cloneRewards(issue.Rewards)
		rewardPayload, err := json.Marshal(rewards)
		if err != nil {
			return err
		}
		inserted, err := txRepo.q.InsertPartnerRewardGrant(ctx, tasksqlc.InsertPartnerRewardGrantParams{
			WorkspaceID: issue.WorkspaceID, IssueID: int64(issue.ID), Provider: issue.Provider, GroupKey: issue.GroupKey,
			ExternalType: issue.ExternalType, AppID: issue.AppID, PlatformID: issue.PlatformID,
			PlatformUserID: issue.PlatformUserID, OperationID: operationID, RewardSnapshot: rewardPayload, ClaimedAt: now,
		})
		if err != nil {
			return err
		}
		if inserted == 0 {
			result.Status = ClaimStatusAlreadyDone
			return nil
		}
		affected, err := txRepo.q.ClaimPartnerIssue(ctx, tasksqlc.ClaimPartnerIssueParams{
			ClaimedAt: nullTime(&now), WorkspaceID: issue.WorkspaceID, ID: int64(issue.ID),
		})
		if err != nil {
			return err
		}
		if affected == 0 {
			result.Status = ClaimStatusAlreadyDone
			return nil
		}
		issue.Status = PartnerIssueStatusClaimed
		issue.ClaimedAt = &now
		result.Issue = issue
		result.Rewards = rewards
		result.Status = ClaimStatusClaimed
		eventKey := fmt.Sprintf("partner.claimed:%d", issue.ID)
		if _, err = txRepo.recordPartnerStatsEvent(ctx, issue, PartnerStatsEventClaimed, eventKey, PartnerIssueStatusClaimed, rewardPayload, now); err != nil {
			return err
		}
		callbackPayload, err := partnerCallbackPayload(issue, operationID, now)
		if err != nil {
			return err
		}
		callbackEventKey := fmt.Sprintf("tasks.partner.claimed:%d", issue.ID)
		_, err = txRepo.callbacks.CreateEvent(ctx, callbackutil.CreateParams{
			WorkspaceID: issue.WorkspaceID, SourceService: "tasks", EventType: CallbackEventClaimed,
			EventKey: callbackEventKey, IdempotencyKey: callbackEventKey,
			Payload: callbackPayload, NextAttemptAt: now,
		})
		return err
	})
	return result, err
}

func partnerCallbackPayload(
	issue PartnerIssue,
	operationID string,
	now time.Time,
) ([]byte, error) {
	return json.Marshal(CallbackPayload{
		WorkspaceID: issue.WorkspaceID, AppID: issue.AppID, PlatformID: issue.PlatformID,
		PlatformUserID: issue.PlatformUserID, TaskID: 0, TaskKey: PartnerIssueKey(issue.ID),
		OperationID: operationID, PeriodStartAt: issue.IssuedAt, PeriodEndAt: partnerIssuePeriodEnd(issue, now),
		Rewards: cloneRewards(issue.Rewards), Payload: issue.PublicPayload,
	})
}

func (r *Repository) ListPartnerDailyStats(
	ctx context.Context,
	workspaceID, provider, groupKey string,
	from, until time.Time,
) ([]PartnerStatsDaily, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	rows, err := repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskPartnerStatsDaily, error) {
		return r.q.AdminListPartnerDailyStats(ctx, tasksqlc.AdminListPartnerDailyStatsParams{
			WorkspaceID: workspaceID, StatsDate: from, StatsDate_2: until,
			Column4: provider, Provider: provider, Column6: groupKey, GroupKey: groupKey,
		})
	})
	if err != nil {
		return nil, err
	}
	result := make([]PartnerStatsDaily, 0, len(rows))
	for _, row := range rows {
		result = append(result, PartnerStatsDaily{
			Date: row.StatsDate, Provider: row.Provider, GroupKey: row.GroupKey, ExternalType: row.ExternalType,
			IssuedCount: uint64(
				row.IssuedCount,
			), CompletedCount: uint64(row.CompletedCount), ClaimedCount: uint64(row.ClaimedCount),
			RevokedCount: uint64(row.RevokedCount), RevokedAfterClaimCount: uint64(row.RevokedAfterClaimCount),
			FailedCount: uint64(
				row.FailedCount,
			), FakeCount: uint64(row.FakeCount), ExpiredCount: uint64(row.ExpiredCount),
			UniqueIssuedUsers: uint64(
				row.UniqueIssuedUsers,
			), UniqueCompletedUsers: uint64(row.UniqueCompletedUsers), UniqueClaimers: uint64(row.UniqueClaimers),
		})
	}
	return result, nil
}

func (r *Repository) recordPartnerStatsEvent(
	ctx context.Context,
	issue PartnerIssue,
	eventType, eventKey, status string,
	payload json.RawMessage,
	now time.Time,
) (bool, error) {
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	inserted, err := r.q.InsertPartnerStatsEvent(ctx, tasksqlc.InsertPartnerStatsEventParams{
		WorkspaceID: issue.WorkspaceID, Provider: issue.Provider, GroupKey: issue.GroupKey, ExternalType: issue.ExternalType,
		IssueID:    sql.NullInt64{Int64: int64(issue.ID), Valid: issue.ID != 0},
		ExternalID: sql.NullString{String: issue.ExternalID, Valid: issue.ExternalID != ""},
		AppID:      issue.AppID, PlatformID: issue.PlatformID, PlatformUserID: issue.PlatformUserID,
		EventType: eventType, EventKey: eventKey, Status: sql.NullString{String: status, Valid: status != ""},
		Payload: rawMessageParam(payload), OccurredAt: now,
	})
	if err != nil || inserted == 0 {
		return false, err
	}
	uniqueInserted, err := r.q.InsertPartnerStatsUniqueUser(ctx, tasksqlc.InsertPartnerStatsUniqueUserParams{
		WorkspaceID: issue.WorkspaceID, Column2: now, Provider: issue.Provider, GroupKey: issue.GroupKey,
		ExternalType: issue.ExternalType, EventType: eventType, AppID: issue.AppID, PlatformID: issue.PlatformID,
		PlatformUserID: issue.PlatformUserID,
	})
	if err != nil {
		return false, err
	}
	increment := partnerStatsIncrement(eventType, status)
	switch eventType {
	case PartnerStatsEventIssued:
		increment.UniqueIssuedUsers = uint64(uniqueInserted)
	case PartnerStatsEventCompleted:
		increment.UniqueCompletedUsers = uint64(uniqueInserted)
	case PartnerStatsEventClaimed:
		increment.UniqueClaimers = uint64(uniqueInserted)
	}
	err = r.q.IncrementPartnerStatsDaily(ctx, tasksqlc.IncrementPartnerStatsDailyParams{
		WorkspaceID: issue.WorkspaceID, Column2: now, Provider: issue.Provider, GroupKey: issue.GroupKey, ExternalType: issue.ExternalType,
		IssuedCount: int64(
			increment.IssuedCount,
		), CompletedCount: int64(increment.CompletedCount), ClaimedCount: int64(increment.ClaimedCount),
		RevokedCount: int64(increment.RevokedCount), RevokedAfterClaimCount: int64(increment.RevokedAfterClaimCount),
		FailedCount: int64(
			increment.FailedCount,
		), FakeCount: int64(increment.FakeCount), ExpiredCount: int64(increment.ExpiredCount),
		UniqueIssuedUsers: int64(
			increment.UniqueIssuedUsers,
		), UniqueCompletedUsers: int64(increment.UniqueCompletedUsers), UniqueClaimers: int64(increment.UniqueClaimers),
	})
	return true, err
}

func partnerStatsIncrement(eventType, status string) PartnerStatsDaily {
	var out PartnerStatsDaily
	switch eventType {
	case PartnerStatsEventIssued:
		out.IssuedCount = 1
	case PartnerStatsEventCompleted:
		out.CompletedCount = 1
	case PartnerStatsEventClaimed:
		out.ClaimedCount = 1
	case PartnerStatsEventRevoked:
		out.RevokedCount = 1
	case PartnerStatsEventRevokedAfterClaim:
		out.RevokedAfterClaimCount = 1
	case PartnerStatsEventFailed:
		out.FailedCount = 1
	case PartnerStatsEventFake:
		out.FakeCount = 1
	case PartnerStatsEventExpired:
		out.ExpiredCount = 1
	}
	switch status {
	case "fake", "fraud_suspected":
		out.FakeCount = 1
	case "expired", "offer_expired":
		out.ExpiredCount = 1
	}
	return out
}

func partnerIssuePeriodEnd(issue PartnerIssue, now time.Time) time.Time {
	if issue.ExpiresAt != nil {
		return *issue.ExpiresAt
	}
	return now
}

func partnerIssueDeadlinePassed(issue PartnerIssue, now time.Time) bool {
	return issue.ExpiresAt != nil && !now.Before(*issue.ExpiresAt)
}

func partnerIssueMustExpireBeforeClaim(issue PartnerIssue, now time.Time) bool {
	if issue.ExpiresAt == nil {
		return false
	}

	switch issue.Status {
	case PartnerIssueStatusIssued:
		return !now.Before(*issue.ExpiresAt)
	case PartnerIssueStatusCompleted:
		return issue.CompletedAt == nil || !issue.CompletedAt.Before(*issue.ExpiresAt)
	default:
		return false
	}
}

func mapPartnerConfig(row tasksqlc.TaskPartnerConfig) PartnerConfig {
	return PartnerConfig{
		WorkspaceID:   row.WorkspaceID,
		Provider:      row.Provider,
		GroupKey:      row.GroupKey,
		Platform:      row.Platform,
		IsEnabled:     row.IsEnabled,
		Secret:        stringPtrFromNull(row.Secret),
		WebhookSecret: stringPtrFromNull(row.WebhookSecret),
		Target:        nullRawMessage(row.Target),
		Settings:      nullRawMessage(row.Settings),
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

func mapPartnerConfigs(rows []tasksqlc.TaskPartnerConfig) []PartnerConfig {
	result := make([]PartnerConfig, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapPartnerConfig(row))
	}
	return result
}

func mapPartnerScript(row tasksqlc.TaskPartnerScript) PartnerScript {
	return PartnerScript{
		Provider:  row.Provider,
		IsEnabled: row.IsEnabled,
		Version:   row.Version,
		Source:    row.Source,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func mapPartnerScripts(rows []tasksqlc.TaskPartnerScript) []PartnerScript {
	result := make([]PartnerScript, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapPartnerScript(row))
	}
	return result
}

func mapPartnerIssue(row tasksqlc.TaskPartnerIssue) PartnerIssue {
	rewards := make([]Reward, 0)
	if snapshot := row.RewardsSnapshot; len(snapshot) > 0 {
		if err := json.Unmarshal(snapshot, &rewards); err != nil || rewards == nil {
			rewards = make([]Reward, 0)
		}
	}

	return PartnerIssue{
		ID:              uint64(row.ID),
		WorkspaceID:     row.WorkspaceID,
		Provider:        row.Provider,
		GroupKey:        row.GroupKey,
		Platform:        row.Platform,
		ExternalID:      row.ExternalID,
		ExternalType:    row.ExternalType,
		IssueKey:        row.IssueKey,
		ExternalClickID: stringPtrFromNull(row.ExternalClickID),
		StartMode:       string(row.StartMode),
		AppID:           row.AppID,
		PlatformID:      row.PlatformID,
		PlatformUserID:  row.PlatformUserID,
		PublicPayload:   nullRawMessage(row.PublicPayload),
		PrivatePayload:  nullRawMessage(row.PrivatePayload),
		Rewards:         rewards,
		Status:          row.Status,
		IssuedAt:        row.IssuedAt,
		StartedAt:       timePtrFromNull(row.StartedAt),
		CompletedAt:     timePtrFromNull(row.CompletedAt),
		ClaimedAt:       timePtrFromNull(row.ClaimedAt),
		ExpiresAt:       timePtrFromNull(row.ExpiresAt),
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func cloneRewards(rewards []Reward) []Reward {
	if len(rewards) == 0 {
		return []Reward{}
	}

	result := make([]Reward, len(rewards))
	copy(result, rewards)

	return result
}

func nullPartnerDurationUnit(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	unit := value.String
	return &unit
}

func stringPtrFromNull(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func timePtrFromNull(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func errorsIsNoRows(err error) bool {
	return err == sql.ErrNoRows
}

func isMissingPartnerConfigTable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "task_partner_config") &&
		(strings.Contains(err.Error(), "Error 1146") || strings.Contains(err.Error(), "doesn't exist"))
}

func isMissingPartnerScriptTable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "task_partner_script") &&
		(strings.Contains(err.Error(), "Error 1146") || strings.Contains(err.Error(), "doesn't exist"))
}

func mergeRawObjects(base, patch json.RawMessage) json.RawMessage {
	if len(base) == 0 {
		base = []byte("{}")
	}
	if len(patch) == 0 {
		return base
	}
	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil || baseMap == nil {
		baseMap = make(map[string]any)
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patch, &patchMap); err != nil || patchMap == nil {
		return base
	}
	for key, value := range patchMap {
		baseMap[key] = value
	}
	out, err := json.Marshal(baseMap)
	if err != nil {
		return base
	}
	return out
}
