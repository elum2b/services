package payment

import (
	"crypto/md5"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/elum2b/services/payment/adapters/vkma"
	"github.com/elum2b/services/payment/repository"
	"github.com/elum2b/services/payment/service/product"
)

func TestVKMAOdnoklassnikiPurchase(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "ok-case",
		AssetCode:       vkma.OKAssetCode,
		ListAmountMinor: 49,
		UserLimit:       1,
		UserInterval:    "ONCE",
	})
	if _, err := env.api.Admin.CreateCatalogPrice(env.ctx, product.CreatePriceParams{
		WorkspaceID:     testWorkspaceID,
		ProductID:       productID,
		AssetCode:       vkma.AssetCode,
		ListAmountMinor: 7,
	}); err != nil {
		t.Fatal(err)
	}

	const okPlatformID int64 = 3
	const userID = "123456789012"
	const vkSecret = "vk-test"
	const applicationKey = "ok-public"
	adapter := env.api.Adapters.VKMA

	quote := url.Values{
		"app_id":            {"3003"},
		"notification_type": {"get_item_test"},
		"item":              {productID},
		"lang":              {"ru_RU"},
		"user_id":           {userID},
	}
	for _, tc := range []struct {
		site string
		want uint64
	}{{"", 7}, {"VK", 7}, {"OK", 49}} {
		if tc.site != "" {
			quote.Set("site", tc.site)
		}
		quoteParams := vkma.VKNotificationParams{
			WorkspaceID:  testWorkspaceID,
			Body:         signVKMAForm(t, quote, vkSecret),
			Secrets:      map[string]string{"3003": vkSecret},
			OKPlatformID: okPlatformID,
		}
		result, err := adapter.HandleVKNotification(env.ctx, quoteParams)
		if err != nil || result.(*vkma.ItemResponse).Price != tc.want {
			t.Fatalf("item quote for %q = %+v, %v", tc.site, result, err)
		}
		if tc.site == "OK" {
			quoteParams.Body = strings.Replace(quoteParams.Body, "site=OK", "site=VK", 1)
			if _, err := adapter.HandleVKNotification(env.ctx, quoteParams); !errors.Is(err, vkma.ErrNotificationSignature) {
				t.Fatalf("modified site error = %v", err)
			}
		}
	}

	fields := url.Values{
		"amount":           {"48"},
		"application_key":  {applicationKey},
		"call_id":          {"1710089670220"},
		"extra_attributes": {`{"launcher":"on"}`},
		"method":           {"callbacks.payment"},
		"product_code":     {productID},
		"transaction_id":   {"ok-test-123"},
		"transaction_time": {"2026-11-23 10:01:02"},
		"uid":              {userID},
	}
	params := vkma.OKNotificationParams{
		WorkspaceID: testWorkspaceID,
		AppID:       3003,
		PlatformID:  okPlatformID,
		Secret:      vkSecret,
		RawQuery:    signVKMAForm(t, fields, vkSecret),
	}
	if ok, err := adapter.HandleOKNotification(env.ctx, params); ok || !errors.Is(err, repository.ErrPaymentMismatch) {
		t.Fatalf("underpayment = %v, %v", ok, err)
	}

	fields.Set("amount", "49")
	params.RawQuery = signVKMAForm(t, fields, vkSecret)
	tampered := params
	tampered.RawQuery = strings.Replace(params.RawQuery, "amount=49", "amount=1", 1)
	if ok, err := adapter.HandleOKNotification(env.ctx, tampered); ok || !errors.Is(err, vkma.ErrNotificationSignature) {
		t.Fatalf("modified amount = %v, %v", ok, err)
	}

	tampered.RawQuery = strings.Replace(params.RawQuery, "application_key=ok-public", "application_key=other", 1)
	if ok, err := adapter.HandleOKNotification(env.ctx, tampered); ok || !errors.Is(err, vkma.ErrNotificationSignature) {
		t.Fatalf("modified application key = %v, %v", ok, err)
	}

	pinned := params
	pinned.ApplicationKey = "another-app"
	if ok, err := adapter.HandleOKNotification(env.ctx, pinned); ok || !errors.Is(err, vkma.ErrNotificationSignature) {
		t.Fatalf("different pinned application key = %v, %v", ok, err)
	}

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if ok, err := adapter.HandleOKNotification(env.ctx, params); !ok || err != nil {
				t.Errorf("confirmation = %v, %v", ok, err)
			}
		})
	}
	wg.Wait()

	var orderID uint64
	var orderCount int
	if err := env.db.QueryRowContext(env.ctx, `
SELECT COALESCE(MIN(id), 0), COUNT(*) FROM payment_order
WHERE workspace_id = $1 AND product_id = $2`, testWorkspaceID, productID).Scan(&orderID, &orderCount); err != nil {
		t.Fatal(err)
	}
	if orderCount != 1 {
		t.Fatalf("expected one order after retries and underpayment, got %d", orderCount)
	}

	var platformID int64
	var platformUserID, asset string
	if err := env.db.QueryRowContext(env.ctx, `
SELECT platform_id, platform_user_id, asset_code FROM payment_order WHERE id = $1`, orderID).
		Scan(&platformID, &platformUserID, &asset); err != nil {
		t.Fatal(err)
	}
	if platformID != okPlatformID || platformUserID != userID || asset != vkma.OKAssetCode {
		t.Fatalf("wrong order identity or currency: %d/%s %s", platformID, platformUserID, asset)
	}
	assertOrderStatus(t, env.ctx, env.db, orderID, "fulfilled")
	assertCallbackEvent(t, env.ctx, env.db, "payment.order.fulfilled", orderID, 1)

	fields.Set("uid", "someone-else")
	conflict := params
	conflict.RawQuery = signVKMAForm(t, fields, vkSecret)
	if ok, err := adapter.HandleOKNotification(env.ctx, conflict); ok || !errors.Is(err, repository.ErrPaymentMismatch) {
		t.Fatalf("conflicting transaction = %v, %v", ok, err)
	}
}

func signVKMAForm(t *testing.T, fields url.Values, secret string) string {
	t.Helper()
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var message strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&message, "%s=%s", key, fields.Get(key))
	}
	message.WriteString(secret)
	return fmt.Sprintf("%s&sig=%x", fields.Encode(), md5.Sum([]byte(message.String())))
}
