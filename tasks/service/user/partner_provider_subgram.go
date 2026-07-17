package user

import (
	"context"
	"net/http"
	"strconv"
	"time"

	json "github.com/goccy/go-json"
)

const defaultSubGramBaseURL = "https://api.subgram.org"

type SubGramProvider struct {
	Client  *http.Client
	BaseURL string
	Timeout time.Duration
}

type subGramSponsorsResponse struct {
	Status     string `json:"status"`
	Additional struct {
		Sponsors []subGramSponsor `json:"sponsors"`
	} `json:"additional"`
}

type subGramSponsor struct {
	AdsID        any    `json:"ads_id"`
	Link         string `json:"link"`
	ResourceID   string `json:"resource_id"`
	Type         string `json:"type"`
	Status       string `json:"status"`
	AvailableNow bool   `json:"available_now"`
	ButtonText   string `json:"button_text"`
	ResourceLogo string `json:"resource_logo"`
	ResourceName string `json:"resource_name"`
}

type subGramSubscriptionsResponse struct {
	Status     string `json:"status"`
	Additional struct {
		Sponsors []subGramSponsor `json:"sponsors"`
	} `json:"additional"`
}

func (p SubGramProvider) ListPartnerTasks(
	ctx context.Context,
	params PartnerListProviderParams,
) ([]PartnerExternalTask, error) {
	maxSponsors := partnerLimit(params.Limit, 5)
	if configured := partnerConfigSetting(params.Config.Settings, "max_sponsors", ""); configured != "" &&
		params.Limit <= 0 {
		if parsed, err := strconv.Atoi(configured); err == nil && parsed > 0 {
			maxSponsors = parsed
		}
	}
	body := map[string]any{
		"chat_id": partnerInt64String(
			firstNonEmpty(partnerString(params.Variables, "chat_id"), params.Identity.PlatformUserID),
		),
		"user_id":       partnerInt64String(params.Identity.PlatformUserID),
		"language_code": params.Locale,
		"is_premium":    params.Identity.IsPremium,
		"action":        partnerConfigSetting(params.Config.Settings, "action", "task"),
		"max_sponsors":  maxSponsors,
		"get_links":     1,
	}
	if value := partnerString(params.Variables, "first_name"); value != "" {
		body["first_name"] = value
	}
	if value := firstNonEmpty(partnerString(params.Variables, "username"), partnerString(params.Variables, "tg_login")); value != "" {
		body["username"] = value
	}
	var response subGramSponsorsResponse
	if err := p.client().postJSON(ctx, "/get-sponsors", map[string]string{
		"Auth": partnerSecret(params.Config.Secret),
	}, body, &response); err != nil {
		return nil, err
	}
	result := make([]PartnerExternalTask, 0, len(response.Additional.Sponsors))
	for _, sponsor := range response.Additional.Sponsors {
		if sponsor.Link == "" || !sponsor.AvailableNow || sponsor.Status == "subscribed" {
			continue
		}
		externalType := firstNonEmpty(sponsor.Type, "resource")
		adsID := stringifyPartnerID(sponsor.AdsID)
		externalID := adsID + ":" + sponsor.ResourceID
		publicPayload := partnerMarshal(map[string]any{
			"ads_id": adsID, "resource_id": sponsor.ResourceID, "link": sponsor.Link,
			"button_text":   firstNonEmpty(sponsor.ButtonText, "Подписаться"),
			"resource_logo": sponsor.ResourceLogo, "resource_name": sponsor.ResourceName,
			"subgram_status": sponsor.Status, "available_now": sponsor.AvailableNow,
		})
		privatePayload := partnerMarshal(map[string]any{
			"ads_id": adsID, "resource_id": sponsor.ResourceID, "link": sponsor.Link,
		})
		result = append(result, PartnerExternalTask{
			ExternalID: externalID, ExternalType: externalType,
			PublicPayload: publicPayload, PrivatePayload: privatePayload,
		})
	}
	return result, nil
}

func (p SubGramProvider) CheckPartnerTask(
	ctx context.Context,
	params PartnerCheckProviderParams,
) (PartnerCheckResult, error) {
	var private struct {
		AdsID string `json:"ads_id"`
		Link  string `json:"link"`
	}
	_ = json.Unmarshal(params.Issue.PrivatePayload, &private)
	body := map[string]any{"user_id": partnerInt64String(params.Identity.PlatformUserID)}
	if private.Link != "" {
		body["links"] = []string{private.Link}
	}
	if private.AdsID != "" {
		if parsed, err := strconv.ParseInt(private.AdsID, 10, 64); err == nil {
			body["ads_ids"] = []int64{parsed}
		}
	}
	var response subGramSubscriptionsResponse
	if err := p.client().postJSON(ctx, "/get-user-subscriptions", map[string]string{
		"Auth": partnerSecret(params.Config.Secret),
	}, body, &response); err != nil {
		return PartnerCheckResult{}, err
	}
	status := "not_found"
	for _, sponsor := range response.Additional.Sponsors {
		if private.Link != "" && sponsor.Link != "" && sponsor.Link != private.Link {
			continue
		}
		status = sponsor.Status
		break
	}
	allowNotgetted := partnerConfigSetting(params.Config.Settings, "allow_notgetted", "false") == "true"
	completed := status == "subscribed" || (status == "notgetted" && allowNotgetted)
	payload := partnerMarshal(map[string]any{
		"provider": "subgram", "status": status, "completed": completed,
	})
	return PartnerCheckResult{Completed: completed, Status: status, Payload: payload}, nil
}

func (p SubGramProvider) client() partnerHTTPClient {
	baseURL := p.BaseURL
	if baseURL == "" {
		baseURL = defaultSubGramBaseURL
	}
	return partnerHTTPClient{client: p.Client, timeout: p.Timeout, baseURL: baseURL}
}

func stringifyPartnerID(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return strconv.FormatInt(int64(toFloat64(typed)), 10)
	}
}

func toFloat64(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case uint64:
		return float64(typed)
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

var _ PartnerProvider = SubGramProvider{}
