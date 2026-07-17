package integration

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/elum2b/services/internal/utils/apiflow"
	"github.com/go-resty/resty/v2"
	json "github.com/goccy/go-json"
)

const (
	defaultTelegramBotAPIBaseURL = "https://api.telegram.org"
	defaultVKAPIBaseURL          = "https://api.vk.com/method"
	defaultVKAPIVersion          = "5.199"
)

type ChannelSubscriptionCheckerOptions struct {
	Client                *resty.Client
	Timeout               time.Duration
	TelegramBotAPIBaseURL string
	VKAPIBaseURL          string
	TokenFlow             *apiflow.Flow
}

type ChannelSubscriptionPlatformChecker struct {
	Client                *resty.Client
	Timeout               time.Duration
	TelegramBotAPIBaseURL string
	VKAPIBaseURL          string
	tokenFlow             *apiflow.Flow
}

type channelSubscriptionPayload struct {
	Platform  string                              `json:"platform"`
	Token     string                              `json:"token"`
	Tokens    []string                            `json:"tokens"`
	ChatID    string                              `json:"chat_id"`
	ChannelID string                              `json:"channel_id"`
	GroupID   string                              `json:"group_id"`
	Strategy  string                              `json:"token_strategy"`
	VK        *channelSubscriptionPlatformPayload `json:"vk"`
	TG        *channelSubscriptionPlatformPayload `json:"tg"`
	Telegram  *channelSubscriptionPlatformPayload `json:"telegram"`
}

type channelSubscriptionPlatformPayload struct {
	Token      string   `json:"token"`
	Tokens     []string `json:"tokens"`
	ChatID     string   `json:"chat_id"`
	ChannelID  string   `json:"channel_id"`
	GroupID    string   `json:"group_id"`
	APIVersion string   `json:"api_version"`
	Strategy   string   `json:"token_strategy"`
}

type telegramGetChatMemberResponse struct {
	OK          bool               `json:"ok"`
	Description string             `json:"description"`
	Result      telegramChatMember `json:"result"`
}

type telegramChatMember struct {
	Status   string `json:"status"`
	IsMember *bool  `json:"is_member"`
}

type telegramGetUserChatBoostsResponse struct {
	OK          bool                   `json:"ok"`
	Description string                 `json:"description"`
	Result      telegramUserChatBoosts `json:"result"`
}

type telegramUserChatBoosts struct {
	Boosts []json.RawMessage `json:"boosts"`
}

type vkIsMemberResponse struct {
	Response any      `json:"response"`
	Error    *vkError `json:"error"`
}

type vkError struct {
	ErrorCode int    `json:"error_code"`
	ErrorMsg  string `json:"error_msg"`
}

func NewChannelSubscriptionChecker(options ChannelSubscriptionCheckerOptions) *ChannelSubscriptionPlatformChecker {
	return &ChannelSubscriptionPlatformChecker{
		Client:                options.Client,
		Timeout:               options.Timeout,
		TelegramBotAPIBaseURL: options.TelegramBotAPIBaseURL,
		VKAPIBaseURL:          options.VKAPIBaseURL,
		tokenFlow:             defaultTokenFlow(options.TokenFlow),
	}
}

func (c *ChannelSubscriptionPlatformChecker) CheckChannelSubscription(
	ctx context.Context,
	params ChannelSubscriptionCheckParams,
) (CheckResult, error) {
	config, err := parseChannelSubscriptionPayload(params.Task.IntegrationPayload)
	if err != nil {
		return CheckResult{}, err
	}
	platform := normalizeChannelPlatform(firstNonEmptyString(params.Provider, config.Platform, params.Task.ActionKey))
	switch platform {
	case "telegram", "tg":
		return c.checkTelegram(ctx, params, config)
	case "vk":
		return c.checkVK(ctx, params, config)
	default:
		payload := marshalChannelCheckPayload(platform, "", false, "unsupported_platform")
		return CheckResult{Completed: false, Reason: "unsupported_platform", Payload: payload}, nil
	}
}

func (c *ChannelSubscriptionPlatformChecker) CheckChannelBoost(
	ctx context.Context,
	params ChannelBoostCheckParams,
) (CheckResult, error) {
	config, err := parseChannelSubscriptionPayload(params.Task.IntegrationPayload)
	if err != nil {
		return CheckResult{}, err
	}
	platform := normalizeChannelPlatform(firstNonEmptyString(params.Provider, config.Platform, params.Task.ActionKey))
	if platform != "telegram" {
		payload := marshalChannelCheckPayload(platform, "", false, "unsupported_platform")
		return CheckResult{Completed: false, Reason: "unsupported_platform", Payload: payload}, nil
	}
	return c.checkTelegramBoost(ctx, params, config)
}

func (c *ChannelSubscriptionPlatformChecker) checkTelegram(
	ctx context.Context,
	params ChannelSubscriptionCheckParams,
	config channelSubscriptionPayload,
) (CheckResult, error) {
	tg := mergeChannelPlatform(config, config.TG, config.Telegram)
	chatID := firstNonEmptyString(
		tg.ChatID,
		tg.ChannelID,
		partnerVariable(params.Variables, "chat_id"),
		partnerVariable(params.Variables, "channel_id"),
	)
	userID := firstNonEmptyString(
		partnerVariable(params.Variables, "tg_user_id"),
		partnerVariable(params.Variables, "user_id"),
		params.Identity.PlatformUserID,
	)
	tokens := channelTokens(tg)
	if chatID == "" || userID == "" || len(tokens) == 0 {
		payload := marshalChannelCheckPayload("telegram", "", false, "invalid_config")
		return CheckResult{Completed: false, Reason: "invalid_config", Payload: payload}, nil
	}
	token, err := c.acquireToken(ctx, tokens, tg.Strategy)
	if err != nil {
		return CheckResult{}, err
	}
	client := c.restyClient()
	baseURL := c.TelegramBotAPIBaseURL
	if baseURL == "" {
		baseURL = defaultTelegramBotAPIBaseURL
	}
	var response telegramGetChatMemberResponse
	resp, err := client.R().
		SetContext(ctx).
		SetQueryParam("chat_id", chatID).
		SetQueryParam("user_id", userID).
		Get(strings.TrimRight(baseURL, "/") + "/bot" + token + "/getChatMember")
	if err != nil {
		return CheckResult{}, err
	}
	if err := json.Unmarshal(resp.Body(), &response); err != nil {
		return CheckResult{}, err
	}
	if !response.OK {
		status := firstNonEmptyString(response.Description, resp.Status())
		payload := marshalChannelCheckPayload("telegram", status, false, "check_failed")
		return CheckResult{Completed: false, Reason: "check_failed", Payload: payload}, nil
	}
	completed := telegramMemberSubscribed(response.Result)
	status := response.Result.Status
	if status == "" {
		status = boolStatus(completed)
	}
	payload := marshalChannelCheckPayload("telegram", status, completed, "")
	return CheckResult{Completed: completed, Payload: payload}, nil
}

func (c *ChannelSubscriptionPlatformChecker) checkTelegramBoost(
	ctx context.Context,
	params ChannelBoostCheckParams,
	config channelSubscriptionPayload,
) (CheckResult, error) {
	tg := mergeChannelPlatform(config, config.TG, config.Telegram)
	chatID := firstNonEmptyString(
		tg.ChatID,
		tg.ChannelID,
		partnerVariable(params.Variables, "chat_id"),
		partnerVariable(params.Variables, "channel_id"),
	)
	userID := firstNonEmptyString(
		partnerVariable(params.Variables, "tg_user_id"),
		partnerVariable(params.Variables, "user_id"),
		params.Identity.PlatformUserID,
	)
	tokens := channelTokens(tg)
	if chatID == "" || userID == "" || len(tokens) == 0 {
		payload := marshalChannelCheckPayload("telegram", "", false, "invalid_config")
		return CheckResult{Completed: false, Reason: "invalid_config", Payload: payload}, nil
	}
	token, err := c.acquireToken(ctx, tokens, tg.Strategy)
	if err != nil {
		return CheckResult{}, err
	}
	client := c.restyClient()
	baseURL := c.TelegramBotAPIBaseURL
	if baseURL == "" {
		baseURL = defaultTelegramBotAPIBaseURL
	}
	var response telegramGetUserChatBoostsResponse
	resp, err := client.R().
		SetContext(ctx).
		SetQueryParam("chat_id", chatID).
		SetQueryParam("user_id", userID).
		Get(strings.TrimRight(baseURL, "/") + "/bot" + token + "/getUserChatBoosts")
	if err != nil {
		return CheckResult{}, err
	}
	if err := json.Unmarshal(resp.Body(), &response); err != nil {
		return CheckResult{}, err
	}
	if !response.OK {
		status := firstNonEmptyString(response.Description, resp.Status())
		payload := marshalChannelCheckPayload("telegram", status, false, "check_failed")
		return CheckResult{Completed: false, Reason: "check_failed", Payload: payload}, nil
	}
	completed := len(response.Result.Boosts) > 0
	status := boostStatus(completed)
	payload := marshalChannelCheckPayload("telegram", status, completed, "")
	return CheckResult{Completed: completed, Payload: payload}, nil
}

func (c *ChannelSubscriptionPlatformChecker) checkVK(
	ctx context.Context,
	params ChannelSubscriptionCheckParams,
	config channelSubscriptionPayload,
) (CheckResult, error) {
	vk := mergeChannelPlatform(config, config.VK)
	groupID := firstNonEmptyString(
		vk.GroupID,
		vk.ChannelID,
		partnerVariable(params.Variables, "group_id"),
		partnerVariable(params.Variables, "channel_id"),
	)
	userID := firstNonEmptyString(
		partnerVariable(params.Variables, "vk_user_id"),
		partnerVariable(params.Variables, "user_id"),
		params.Identity.PlatformUserID,
	)
	tokens := channelTokens(vk)
	if groupID == "" || userID == "" || len(tokens) == 0 {
		payload := marshalChannelCheckPayload("vk", "", false, "invalid_config")
		return CheckResult{Completed: false, Reason: "invalid_config", Payload: payload}, nil
	}
	apiVersion := firstNonEmptyString(vk.APIVersion, defaultVKAPIVersion)
	token, err := c.acquireToken(ctx, tokens, vk.Strategy)
	if err != nil {
		return CheckResult{}, err
	}
	client := c.restyClient()
	baseURL := c.VKAPIBaseURL
	if baseURL == "" {
		baseURL = defaultVKAPIBaseURL
	}
	var response vkIsMemberResponse
	resp, err := client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"group_id":     groupID,
			"user_id":      userID,
			"access_token": token,
			"v":            apiVersion,
		}).
		Get(strings.TrimRight(baseURL, "/") + "/groups.isMember")
	if err != nil {
		return CheckResult{}, err
	}
	if err := json.Unmarshal(resp.Body(), &response); err != nil {
		return CheckResult{}, err
	}
	if response.Error != nil {
		payload := marshalChannelCheckPayload("vk", response.Error.ErrorMsg, false, "check_failed")
		return CheckResult{Completed: false, Reason: "check_failed", Payload: payload}, nil
	}
	completed := vkIsMember(response.Response)
	status := boolStatus(completed)
	payload := marshalChannelCheckPayload("vk", status, completed, "")
	return CheckResult{Completed: completed, Payload: payload}, nil
}

func mergeChannelPlatform(
	root channelSubscriptionPayload,
	nested ...*channelSubscriptionPlatformPayload,
) channelSubscriptionPlatformPayload {
	out := channelSubscriptionPlatformPayload{
		Token:     root.Token,
		Tokens:    append([]string(nil), root.Tokens...),
		ChatID:    root.ChatID,
		ChannelID: root.ChannelID,
		GroupID:   root.GroupID,
		Strategy:  root.Strategy,
	}
	for _, value := range nested {
		if value == nil {
			continue
		}
		if value.Token != "" {
			out.Token = value.Token
		}
		if len(value.Tokens) > 0 {
			out.Tokens = append([]string(nil), value.Tokens...)
		}
		if value.ChatID != "" {
			out.ChatID = value.ChatID
		}
		if value.ChannelID != "" {
			out.ChannelID = value.ChannelID
		}
		if value.GroupID != "" {
			out.GroupID = value.GroupID
		}
		if value.APIVersion != "" {
			out.APIVersion = value.APIVersion
		}
		if value.Strategy != "" {
			out.Strategy = value.Strategy
		}
	}
	return out
}

func channelTokens(config channelSubscriptionPlatformPayload) []string {
	out := make([]string, 0, len(config.Tokens)+1)
	if config.Token != "" {
		out = append(out, config.Token)
	}
	for _, token := range config.Tokens {
		if token != "" {
			out = append(out, token)
		}
	}
	return out
}

func parseChannelSubscriptionPayload(raw json.RawMessage) (channelSubscriptionPayload, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return channelSubscriptionPayload{}, nil
	}
	var config channelSubscriptionPayload
	if err := json.Unmarshal(raw, &config); err != nil {
		return channelSubscriptionPayload{}, err
	}
	return config, nil
}

func telegramMemberSubscribed(member telegramChatMember) bool {
	switch member.Status {
	case "creator", "administrator", "member":
		return true
	case "restricted":
		return member.IsMember != nil && *member.IsMember
	default:
		return false
	}
}

func vkIsMember(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed == 1
	case int:
		return typed == 1
	case int64:
		return typed == 1
	case string:
		return typed == "1" || strings.EqualFold(typed, "true")
	case []any:
		if slices.ContainsFunc(typed, vkIsMember) {
			return true
		}
	}
	return false
}

func (c *ChannelSubscriptionPlatformChecker) restyClient() *resty.Client {
	client := c.Client
	if client == nil {
		client = resty.New()
	}
	if c.Timeout > 0 {
		client.SetTimeout(c.Timeout)
	}
	return client
}

func (c *ChannelSubscriptionPlatformChecker) acquireToken(
	ctx context.Context,
	tokens []string,
	strategy string,
) (string, error) {
	if c.tokenFlow == nil {
		c.tokenFlow = defaultTokenFlow(nil)
	}
	return c.tokenFlow.Acquire(ctx, apiflow.TokenSet{Tokens: tokens, Strategy: strategy})
}

func defaultTokenFlow(value *apiflow.Flow) *apiflow.Flow {
	if value != nil {
		return value
	}
	return apiflow.New(apiflow.Options{RatePerSecond: apiflow.DefaultRatePerSecond})
}

func normalizeChannelPlatform(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "telegram", "tg", "tma":
		return "telegram"
	case "vk", "vkma", "vkontakte":
		return "vk"
	default:
		return value
	}
}

func partnerVariable(values map[string]string, key string) string {
	if values == nil {
		return ""
	}
	return values[key]
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func boolStatus(value bool) string {
	if value {
		return "subscribed"
	}
	return "not_subscribed"
}

func boostStatus(value bool) string {
	if value {
		return "boosted"
	}
	return "not_boosted"
}

func marshalChannelCheckPayload(platform, status string, completed bool, reason string) json.RawMessage {
	payload, err := json.Marshal(map[string]any{
		"provider":  platform,
		"status":    status,
		"completed": completed,
		"reason":    reason,
	})
	if err != nil {
		return []byte("{}")
	}
	return payload
}
