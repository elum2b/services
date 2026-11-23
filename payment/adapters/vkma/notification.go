package vkma

import (
	"context"
	"errors"
	"net/url"

	"github.com/elum-utils/sign/vkmashop"

	"github.com/elum2b/services/payment/repository"
)

var (
	ErrNotificationSignature = errors.New("invalid payment notification signature")
	ErrInvalidNotification   = errors.New("invalid payment notification")
)

type VKNotificationParams struct {
	WorkspaceID  string
	Body         string
	Secrets      map[string]string
	OKPlatformID int64
	// OKCatalogAppID maps a verified linked OK app ID to the catalog app.
	OKCatalogAppID int64
}

// HandleVKNotification handles the shared form POST. The signed site field
// selects the currency for get_item. VK settlement uses the existing flow.
func (a *VKMA) HandleVKNotification(ctx context.Context, params VKNotificationParams) (any, error) {
	fields, err := notificationFields(params.Body)
	if err != nil {
		return nil, err
	}
	if params.Secrets[fields.Get("app_id")] == "" {
		return nil, ErrNotificationSignature
	}
	verified, ok := vkmashop.Verify(params.Body, params.Secrets)
	if !ok {
		return nil, ErrNotificationSignature
	}

	switch fields.Get("site") {
	case "", "VK":
		return a.HandleRequest(ctx, Request{WorkspaceID: params.WorkspaceID, Params: *verified})
	case "OK":
		if params.OKPlatformID <= 0 || params.OKPlatformID == PlatformID ||
			verified.AppID <= 0 || fields.Get("user_id") == "" ||
			(verified.NotificationType != vkmashop.GetItem &&
				verified.NotificationType != vkmashop.GetItemTest) {
			return nil, ErrInvalidNotification
		}
		appID := int64(verified.AppID)
		if params.OKCatalogAppID > 0 {
			appID = params.OKCatalogAppID
		}
		ctx, cancel := a.withContext(ctx)
		defer cancel()

		return a.getItem(ctx, repository.ProductGetParams{
			WorkspaceID:    params.WorkspaceID,
			AppID:          appID,
			PlatformID:     params.OKPlatformID,
			PlatformUserID: fields.Get("user_id"),
			ProductID:      productID(*verified),
			AssetCode:      OKAssetCode,
			Locale:         normalizeLocale(verified.Lang),
		})
	default:
		return nil, ErrInvalidNotification
	}
}

func notificationFields(raw string) (url.Values, error) {
	fields, err := url.ParseQuery(raw)
	if err != nil {
		return nil, ErrInvalidNotification
	}
	for key, values := range fields {
		if key == "" || len(values) != 1 {
			return nil, ErrInvalidNotification
		}
	}
	return fields, nil
}
