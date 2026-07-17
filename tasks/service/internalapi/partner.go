package internalapi

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/tasks/repository"
	taskruntime "github.com/elum2b/services/tasks/runtime"
)

const (
	PartnerCallbackStatusRevoked   = "revoked"
	PartnerCallbackStatusAmbiguous = "ambiguous"
)

var partnerLookupKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type PartnerCallbackLookup struct {
	PlatformUserID string
	PrivatePayload []PartnerCallbackLookupItem
}

type PartnerCallbackLookupItem struct {
	Key   string
	Value string
}

type PartnerCallbackParams struct {
	WorkspaceID     string
	Provider        string
	GroupKey        string
	Platform        string
	IssueID         uint64
	IssueRef        string
	ExternalID      string
	ExternalClickID string
	PlatformUserID  string
	AppID           int64
	PlatformID      int64
	Lookup          PartnerCallbackLookup
	Status          string
	Payload         json.RawMessage
	Now             time.Time
}

type PartnerCallbackResult struct {
	Status string                   `json:"status"`
	Issue  *repository.PartnerIssue `json:"issue,omitempty"`
}

type PartnerWebhookParams struct {
	WorkspaceID string
	Secret      string
	Headers     map[string]string
	Query       map[string]string
	Body        json.RawMessage
	Now         time.Time
}

func (i *Internal) OnPartnerCallback(ctx context.Context, params PartnerCallbackParams) (PartnerCallbackResult, error) {
	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return PartnerCallbackResult{}, err
	}
	if params.Provider == "" {
		return PartnerCallbackResult{Status: repository.ClaimStatusNotFound}, nil
	}

	issueID := params.IssueID
	if issueID == 0 && params.IssueRef != "" {
		if parsed, ok := repository.ParsePartnerIssueRef(params.IssueRef); ok {
			issueID = parsed
		} else if parsed, err := strconv.ParseUint(strings.TrimSpace(params.IssueRef), 10, 64); err == nil {
			issueID = parsed
		}
	}
	if issueID == 0 {
		var issue repository.PartnerIssue
		var found bool
		var err error
		if params.ExternalClickID != "" {
			issue, found, err = i.repository.GetPartnerIssueByExternalClickID(
				mergedCtx,
				params.WorkspaceID,
				params.Provider,
				params.ExternalClickID,
			)
		} else if params.ExternalID != "" && params.PlatformUserID != "" {
			issue, found, err = i.repository.GetPartnerIssueByExternalUser(
				mergedCtx,
				repository.PartnerIssueExternalUserLookup{
					WorkspaceID:    params.WorkspaceID,
					Provider:       params.Provider,
					GroupKey:       params.GroupKey,
					Platform:       params.Platform,
					ExternalID:     params.ExternalID,
					PlatformUserID: params.PlatformUserID,
					AppID:          params.AppID,
					PlatformID:     params.PlatformID,
				},
			)
		} else if len(params.Lookup.PrivatePayload) > 0 && params.Lookup.PlatformUserID != "" {
			issue, found, err = i.lookupPartnerIssueByPrivatePayloadList(mergedCtx, params, params.Lookup.PrivatePayload, params.Lookup.PlatformUserID)
		} else {
			return PartnerCallbackResult{Status: repository.ClaimStatusNotFound}, nil
		}
		if err != nil {
			if errors.Is(err, repository.ErrPartnerIssueAmbiguous) {
				return PartnerCallbackResult{Status: PartnerCallbackStatusAmbiguous}, nil
			}

			return PartnerCallbackResult{}, err
		}
		if !found {
			return PartnerCallbackResult{Status: repository.ClaimStatusNotFound}, nil
		}
		issueID = issue.ID
		params.GroupKey = issue.GroupKey
		params.Platform = issue.Platform
	}
	if params.GroupKey == "" || params.Platform == "" {
		return PartnerCallbackResult{Status: repository.ClaimStatusNotFound}, nil
	}

	scope := repository.PartnerIssueScope{
		WorkspaceID: params.WorkspaceID,
		Provider:    params.Provider,
		GroupKey:    params.GroupKey,
		Platform:    params.Platform,
	}

	switch params.Status {
	case repository.PartnerIssueStatusCompleted, "complete", "step_completed", "subscribed":
		issue, changed, err := i.repository.CompletePartnerIssue(
			mergedCtx,
			scope,
			issueID,
			params.Status,
			params.Payload,
			params.Now,
		)
		if err != nil {
			return PartnerCallbackResult{}, err
		}
		if issue.ID == 0 {
			return PartnerCallbackResult{Status: repository.ClaimStatusNotFound}, nil
		}
		if !changed {
			return PartnerCallbackResult{Status: issue.Status, Issue: &issue}, nil
		}
		return PartnerCallbackResult{Status: issue.Status, Issue: &issue}, nil
	case PartnerCallbackStatusRevoked,
		repository.PartnerIssueStatusRevokedAfterClaim,
		"unsubscribe",
		"unsubscribed",
		"cancelled",
		"canceled":
		issue, changed, err := i.repository.RevokePartnerIssue(
			mergedCtx,
			scope,
			issueID,
			params.Status,
			params.Payload,
			params.Now,
		)
		if err != nil {
			return PartnerCallbackResult{}, err
		}
		if issue.ID == 0 {
			return PartnerCallbackResult{Status: repository.ClaimStatusNotFound}, nil
		}
		if !changed {
			return PartnerCallbackResult{Status: issue.Status, Issue: &issue}, nil
		}
		return PartnerCallbackResult{Status: issue.Status, Issue: &issue}, nil
	default:
		return PartnerCallbackResult{Status: "unsupported_status"}, nil
	}
}

func (i *Internal) lookupPartnerIssueByPrivatePayloadList(
	ctx context.Context,
	params PartnerCallbackParams,
	values []PartnerCallbackLookupItem,
	platformUserID string,
) (repository.PartnerIssue, bool, error) {
	for _, item := range values {
		if item.Key == "" || item.Value == "" {
			continue
		}
		issue, found, err := i.lookupPartnerIssueByPrivatePayload(ctx, params, item.Key, item.Value, platformUserID)
		if err != nil || found {
			return issue, found, err
		}
	}
	return repository.PartnerIssue{}, false, nil
}

func (i *Internal) lookupPartnerIssueByPrivatePayload(
	ctx context.Context,
	params PartnerCallbackParams,
	key string,
	value string,
	platformUserID string,
) (repository.PartnerIssue, bool, error) {
	if !partnerLookupKeyPattern.MatchString(key) || value == "" || platformUserID == "" {
		return repository.PartnerIssue{}, false, nil
	}
	return i.repository.GetPartnerIssueByPrivatePayloadUser(
		ctx,
		repository.PartnerIssuePrivatePayloadLookup{
			WorkspaceID:    params.WorkspaceID,
			Provider:       params.Provider,
			GroupKey:       params.GroupKey,
			Platform:       params.Platform,
			LookupKey:      key,
			LookupValue:    value,
			PlatformUserID: platformUserID,
			AppID:          params.AppID,
			PlatformID:     params.PlatformID,
		},
	)
}

func (i *Internal) HandlePartnerWebhook(
	ctx context.Context,
	params PartnerWebhookParams,
) (PartnerCallbackResult, error) {
	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return PartnerCallbackResult{}, err
	}
	if params.Secret == "" {
		return PartnerCallbackResult{Status: repository.ClaimStatusNotFound}, nil
	}
	config, found, err := i.repository.GetPartnerConfigByWebhookSecret(
		mergedCtx,
		params.WorkspaceID,
		params.Secret,
	)
	if err != nil {
		return PartnerCallbackResult{}, err
	}
	if !found || !config.IsEnabled {
		return PartnerCallbackResult{Status: repository.ClaimStatusNotFound}, nil
	}
	if i.runtime == nil {
		return PartnerCallbackResult{}, fmt.Errorf("tasks partner runtime is not configured")
	}
	bodyMap := map[string]any{}
	if len(params.Body) != 0 {
		if err := json.Unmarshal(params.Body, &bodyMap); err != nil {
			bodyMap = map[string]any{"raw": string(params.Body)}
		}
	}
	result, err := i.runtime.Handle(mergedCtx, config.Provider, taskruntime.Event{
		Action:   "callback",
		Provider: config.Provider,
		Config:   internalPartnerConfigMap(config),
		Request: map[string]any{
			"headers":  stringMapToAny(params.Headers),
			"query":    stringMapToAny(params.Query),
			"body":     bodyMap,
			"raw_body": string(params.Body),
		},
		Now: params.Now,
	})
	if err != nil {
		return PartnerCallbackResult{}, err
	}
	if ok, _ := result["ok"].(bool); !ok {
		return PartnerCallbackResult{Status: firstWebhookString(result["error"], "unsupported_callback")}, nil
	}
	if callbacks, ok := result["callbacks"].([]any); ok {
		var last PartnerCallbackResult
		for _, item := range callbacks {
			callback, ok := item.(map[string]any)
			if !ok {
				continue
			}
			last, err = i.applyPartnerWebhookCallback(
				mergedCtx,
				config,
				callback,
				params.Body,
				params.Now,
			)
			if err != nil {
				return PartnerCallbackResult{}, err
			}
		}
		if last.Status == "" {
			return PartnerCallbackResult{Status: "processed"}, nil
		}
		return last, nil
	}
	return i.applyPartnerWebhookCallback(
		mergedCtx,
		config,
		result,
		params.Body,
		params.Now,
	)
}

func (i *Internal) applyPartnerWebhookCallback(
	ctx context.Context,
	config repository.PartnerConfig,
	result map[string]any,
	fallbackPayload json.RawMessage,
	now time.Time,
) (PartnerCallbackResult, error) {
	status := firstWebhookString(result["status"], result["action"])
	if status == "complete" {
		status = repository.PartnerIssueStatusCompleted
	}
	if status == "" && webhookBool(result["completed"]) {
		status = repository.PartnerIssueStatusCompleted
	}
	payload := webhookRaw(result["payload"])
	if len(payload) == 0 {
		payload = fallbackPayload
	}
	return i.OnPartnerCallback(ctx, PartnerCallbackParams{
		WorkspaceID:     config.WorkspaceID,
		Provider:        config.Provider,
		GroupKey:        config.GroupKey,
		Platform:        config.Platform,
		IssueID:         webhookUint64(result["issue_id"]),
		IssueRef:        firstWebhookString(result["issue_ref"], result["task_ref"]),
		ExternalID:      firstWebhookString(result["external_id"], result["offer_id"], result["task_id"]),
		ExternalClickID: firstWebhookString(result["external_click_id"], result["click_id"]),
		PlatformUserID:  firstWebhookString(result["platform_user_id"], result["user_id"], result["tg_user_id"]),
		AppID:           int64(webhookUint64(result["app_id"])),
		PlatformID:      int64(webhookUint64(result["platform_id"])),
		Lookup:          webhookLookup(result),
		Status:          status,
		Payload:         payload,
		Now:             now,
	})
}

func internalPartnerConfigMap(config repository.PartnerConfig) map[string]any {
	return map[string]any{
		"workspace_id":   config.WorkspaceID,
		"provider":       config.Provider,
		"group_key":      config.GroupKey,
		"platform":       config.Platform,
		"secret":         stringPtrValue(config.Secret),
		"webhook_secret": stringPtrValue(config.WebhookSecret),
		"settings":       rawObject(config.Settings),
		"target":         rawObject(config.Target),
	}
}

func stringMapToAny(values map[string]string) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func rawObject(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func webhookRaw(value any) json.RawMessage {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return raw
}

func webhookLookup(result map[string]any) PartnerCallbackLookup {
	lookupMap, _ := result["lookup"].(map[string]any)
	lookup := PartnerCallbackLookup{
		PlatformUserID: firstWebhookString(
			result["platform_user_id"], result["user_id"], result["tg_user_id"],
			lookupMap["platform_user_id"], lookupMap["user_id"], lookupMap["tg_user_id"],
		),
		PrivatePayload: []PartnerCallbackLookupItem{},
	}
	privatePayload, _ := lookupMap["private_payload"].([]any)
	for _, value := range privatePayload {
		item, _ := value.(map[string]any)
		key := firstWebhookString(item["key"])
		text := firstWebhookString(item["value"])
		if key == "" || text == "" {
			continue
		}
		lookup.PrivatePayload = append(lookup.PrivatePayload, PartnerCallbackLookupItem{Key: key, Value: text})
	}
	if len(lookup.PrivatePayload) == 0 {
		lookup.PrivatePayload = nil
	}
	return lookup
}

func firstWebhookString(values ...any) string {
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			if typed != "" {
				return typed
			}
		case int64:
			if typed != 0 {
				return strconv.FormatInt(typed, 10)
			}
		case float64:
			if typed != 0 {
				return strconv.FormatInt(int64(typed), 10)
			}
		}
	}
	return ""
}

func webhookUint64(value any) uint64 {
	switch typed := value.(type) {
	case int64:
		if typed > 0 {
			return uint64(typed)
		}
	case float64:
		if typed > 0 {
			return uint64(typed)
		}
	case string:
		parsed, _ := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		return parsed
	}
	return 0
}

func webhookBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "true" || typed == "1"
	}
	return false
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
