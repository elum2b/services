package user

import (
	"context"
	"fmt"
	"strconv"
	"time"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/tasks/repository"
	taskruntime "github.com/elum2b/services/tasks/runtime"
)

type LuaProvider struct {
	Runtime  *taskruntime.Manager
	Provider string
}

func (p LuaProvider) ListPartnerTasks(
	ctx context.Context,
	params PartnerListProviderParams,
) ([]PartnerExternalTask, error) {
	result, err := p.handle(
		ctx,
		"list",
		params.Identity,
		params.Config,
		repository.PartnerIssue{},
		params.Variables,
		params.Locale,
		params.Limit,
		params.Now,
	)
	if err != nil {
		return nil, err
	}
	if ok, _ := result["ok"].(bool); !ok {
		return nil, fmt.Errorf("lua partner %s list failed: %s", p.Provider, stringValue(result["error"]))
	}
	rawTasks, _ := result["tasks"].([]any)
	tasks := make([]PartnerExternalTask, 0, len(rawTasks))
	for _, rawTask := range rawTasks {
		item, ok := rawTask.(map[string]any)
		if !ok {
			continue
		}
		expiresAt := timePtr(item["expires_at"])
		tasks = append(tasks, PartnerExternalTask{
			ExternalID:     stringValue(item["external_id"]),
			ExternalType:   firstNonEmpty(stringValue(item["external_type"]), "default"),
			PublicPayload:  rawJSON(item["public_payload"]),
			PrivatePayload: rawJSON(item["private_payload"]),
			ExpiresAt:      expiresAt,
			StartMode:      firstNonEmpty(stringValue(item["start_mode"]), repository.StartModeNone),
			WindowKey:      stringValue(item["window_key"]),
		})
	}
	return tasks, nil
}

func (p LuaProvider) CheckPartnerTask(
	ctx context.Context,
	params PartnerCheckProviderParams,
) (PartnerCheckResult, error) {
	result, err := p.handle(
		ctx,
		"check",
		params.Identity,
		params.Config,
		params.Issue,
		params.Variables,
		"",
		0,
		params.Now,
	)
	if err != nil {
		return PartnerCheckResult{}, err
	}
	if ok, _ := result["ok"].(bool); !ok {
		return PartnerCheckResult{}, fmt.Errorf(
			"lua partner %s check failed: %s",
			p.Provider,
			stringValue(result["error"]),
		)
	}
	return PartnerCheckResult{
		Completed: boolValue(result["completed"]),
		Status:    firstNonEmpty(stringValue(result["status"]), repository.PartnerIssueStatusCompleted),
		Payload:   rawJSON(result["payload"]),
	}, nil
}

func (p LuaProvider) StartPartnerTask(
	ctx context.Context,
	params PartnerStartProviderParams,
) (PartnerStartResult, error) {
	result, err := p.handle(
		ctx,
		"start",
		params.Identity,
		params.Config,
		params.Issue,
		params.Variables,
		"",
		0,
		params.Now,
	)
	if err != nil {
		return PartnerStartResult{}, err
	}
	if ok, _ := result["ok"].(bool); !ok {
		return PartnerStartResult{Status: stringValue(result["error"])}, nil
	}
	return PartnerStartResult{
		Started:             boolValue(result["started"]),
		Status:              firstNonEmpty(stringValue(result["status"]), "started"),
		ActionURL:           stringValue(result["action_url"]),
		ExternalClickID:     stringValue(result["external_click_id"]),
		PublicPayloadPatch:  rawJSON(result["public_payload_patch"]),
		PrivatePayloadPatch: rawJSON(result["private_payload_patch"]),
		Payload:             rawJSON(result["payload"]),
	}, nil
}

func (p LuaProvider) handle(
	ctx context.Context,
	action string,
	identity Identity,
	config repository.PartnerConfig,
	issue repository.PartnerIssue,
	variables map[string]string,
	locale string,
	limit int32,
	now time.Time,
) (taskruntime.Result, error) {
	if p.Runtime == nil {
		return nil, fmt.Errorf("lua partner runtime is nil")
	}
	provider := p.Provider
	if provider == "" {
		provider = config.Provider
	}
	return p.Runtime.Handle(ctx, provider, taskruntime.Event{
		Action:    action,
		Provider:  provider,
		Identity:  identityMap(identity),
		Config:    configMap(config),
		Issue:     issueMap(issue),
		Variables: variablesMap(variables),
		Locale:    locale,
		Limit:     limit,
		Now:       now,
	})
}

func identityMap(identity Identity) map[string]any {
	return map[string]any{
		"workspace_id":     identity.WorkspaceID,
		"app_id":           identity.AppID,
		"platform_id":      identity.PlatformID,
		"platform":         identity.Platform,
		"platform_user_id": identity.PlatformUserID,
		"is_premium":       identity.IsPremium,
		"sex":              identity.Sex,
		"country":          identity.Country,
	}
}

func configMap(config repository.PartnerConfig) map[string]any {
	return map[string]any{
		"workspace_id": config.WorkspaceID,
		"provider":     config.Provider,
		"group_key":    config.GroupKey,
		"platform":     config.Platform,
		"secret":       stringPtrValue(config.Secret),
		"settings":     rawMap(config.Settings),
		"target":       rawMap(config.Target),
	}
}

func issueMap(issue repository.PartnerIssue) map[string]any {
	return map[string]any{
		"id":                issue.ID,
		"key":               repository.PartnerIssueKey(issue.ID),
		"provider":          issue.Provider,
		"group_key":         issue.GroupKey,
		"platform":          issue.Platform,
		"external_id":       issue.ExternalID,
		"external_type":     issue.ExternalType,
		"external_click_id": stringPtrValue(issue.ExternalClickID),
		"status":            issue.Status,
		"public_payload":    rawMap(issue.PublicPayload),
		"private_payload":   rawMap(issue.PrivatePayload),
	}
}

func variablesMap(values map[string]string) map[string]any {
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func rawJSON(value any) json.RawMessage {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return raw
}

func rawMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil || result == nil {
		return map[string]any{}
	}
	return result
}

func timePtr(value any) *time.Time {
	raw := stringValue(value)
	if raw == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil
	}
	return &parsed
}

func boolValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	case int64:
		return v != 0
	}
	return false
}

func stringValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(v)
	}
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

var _ PartnerProvider = LuaProvider{}
var _ PartnerStarter = LuaProvider{}
