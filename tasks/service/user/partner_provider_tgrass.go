package user

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

const defaultTgrassBaseURL = "https://tgrass.space"

type TgrassProvider struct {
	Client  *http.Client
	BaseURL string
	Timeout time.Duration
}

type tgrassOffer struct {
	Name       *string `json:"name"`
	Link       string  `json:"link"`
	Subscribed bool    `json:"subscribed"`
	Type       string  `json:"type"`
	ChannelID  *string `json:"channel_id"`
	OfferID    int64   `json:"offer_id"`
}

type tgrassOffersResponse struct {
	Status      string        `json:"status"`
	Description string        `json:"description"`
	Offers      []tgrassOffer `json:"offers"`
}

type tgrassCheckResponse struct {
	Status string `json:"status"`
	IsFake bool   `json:"is_fake"`
}

func (p TgrassProvider) ListPartnerTasks(
	ctx context.Context,
	params PartnerListProviderParams,
) ([]PartnerExternalTask, error) {
	body := map[string]any{
		"tg_user_id": partnerInt64String(params.Identity.PlatformUserID),
		"is_premium": params.Identity.IsPremium,
		"lang":       params.Locale,
	}
	if body["lang"] == "" {
		body["lang"] = partnerString(params.Variables, "lang")
	}
	if value := partnerString(params.Variables, "tg_login"); value != "" {
		body["tg_login"] = value
	}
	if value := partnerString(params.Variables, "username"); value != "" && body["tg_login"] == nil {
		body["tg_login"] = value
	}
	if value := partnerString(params.Variables, "gender"); value != "" {
		body["gender"] = value
	}
	if params.Limit > 0 {
		body["offers_limit"] = params.Limit
	}
	var response tgrassOffersResponse
	path := "/offers"
	if partnerConfigSetting(params.Config.Settings, "list_endpoint", "offers") == "tasks" {
		path = "/tasks"
	}
	if err := p.client().postJSON(ctx, path, map[string]string{
		"Auth": partnerSecret(params.Config.Secret),
	}, body, &response); err != nil {
		return nil, err
	}
	if response.Status == "no_offers" {
		return []PartnerExternalTask{}, nil
	}
	result := make([]PartnerExternalTask, 0, len(response.Offers))
	for _, offer := range response.Offers {
		if offer.Link == "" || offer.OfferID == 0 || offer.Subscribed {
			continue
		}
		externalType := strings.TrimSpace(offer.Type)
		if externalType == "" {
			externalType = "offer"
		}
		name := ""
		if offer.Name != nil {
			name = *offer.Name
		}
		publicPayload := partnerMarshal(map[string]any{
			"offer_id": offer.OfferID, "channel_id": offer.ChannelID, "name": name,
			"link": offer.Link, "button_text": tgrassButtonText(externalType),
			"tgrass_status": response.Status, "subscribed": offer.Subscribed,
		})
		privatePayload := partnerMarshal(map[string]any{
			"offer_id": offer.OfferID, "link": offer.Link, "type": externalType,
		})
		result = append(result, PartnerExternalTask{
			ExternalID: strconv.FormatInt(offer.OfferID, 10), ExternalType: externalType,
			PublicPayload: publicPayload, PrivatePayload: privatePayload,
		})
	}
	return result, nil
}

func (p TgrassProvider) CheckPartnerTask(
	ctx context.Context,
	params PartnerCheckProviderParams,
) (PartnerCheckResult, error) {
	var private struct {
		OfferID int64 `json:"offer_id"`
	}
	_ = json.Unmarshal(params.Issue.PrivatePayload, &private)
	if private.OfferID == 0 {
		if parsed, err := strconv.ParseInt(params.Issue.ExternalID, 10, 64); err == nil {
			private.OfferID = parsed
		}
	}
	var response tgrassCheckResponse
	if err := p.client().postJSON(ctx, "/check", map[string]string{
		"Auth": partnerSecret(params.Config.Secret),
	}, map[string]any{
		"tg_user_id": partnerInt64String(params.Identity.PlatformUserID),
		"offer_id":   private.OfferID,
	}, &response); err != nil {
		return PartnerCheckResult{}, err
	}
	completed := response.Status == "subscribed" && !response.IsFake
	payload := partnerMarshal(map[string]any{
		"provider": "tgrass", "status": response.Status, "is_fake": response.IsFake,
		"completed": completed,
	})
	return PartnerCheckResult{Completed: completed, Status: tgrassInternalStatus(response), Payload: payload}, nil
}

func (p TgrassProvider) client() partnerHTTPClient {
	baseURL := p.BaseURL
	if baseURL == "" {
		baseURL = defaultTgrassBaseURL
	}
	return partnerHTTPClient{client: p.Client, timeout: p.Timeout, baseURL: baseURL}
}

func tgrassButtonText(externalType string) string {
	switch externalType {
	case "channel", "folder":
		return "Подписаться"
	default:
		return "Перейти"
	}
}

func tgrassInternalStatus(response tgrassCheckResponse) string {
	if response.IsFake {
		return "fake"
	}
	if response.Status == "" {
		return "unknown"
	}
	return response.Status
}

var _ PartnerProvider = TgrassProvider{}
