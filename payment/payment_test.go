package payment

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/elum-utils/sign/vkmashop"
	services "github.com/elum2b/services"
	"github.com/elum2b/services/internal/testsupport"
	utils "github.com/elum2b/services/internal/utils"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/payment/adapters/platega"
	"github.com/elum2b/services/payment/adapters/telegramstars"
	paymentton "github.com/elum2b/services/payment/adapters/ton"
	paymentvkma "github.com/elum2b/services/payment/adapters/vkma"
	"github.com/elum2b/services/payment/adapters/yookassa"
	"github.com/elum2b/services/payment/repository"
	"github.com/elum2b/services/payment/service/admin"
	paymentasset "github.com/elum2b/services/payment/service/asset"
	"github.com/elum2b/services/payment/service/checkout"
	"github.com/elum2b/services/payment/service/operational"
	"github.com/elum2b/services/payment/service/product"
	paymentrefund "github.com/elum2b/services/payment/service/refund"
	"github.com/elum2b/services/payment/service/subscription"
	"github.com/elum2b/services/payment/service/user"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
	json "github.com/goccy/go-json"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/xssnick/tonutils-go/address"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPaymentProviderAdaptersNilReceiverReturnsNotInitialized(t *testing.T) {
	t.Run("platega", func(t *testing.T) {
		var adapter *platega.Platega

		if _, err := adapter.CreatePayment(context.Background(), platega.CreatePaymentParams{}); !errors.Is(err, platega.ErrNotInitialized) {
			t.Fatalf("CreatePayment error = %v, want %v", err, platega.ErrNotInitialized)
		}
		if _, err := adapter.HandleWebhook(context.Background(), platega.WebhookRequest{}); !errors.Is(err, platega.ErrNotInitialized) {
			t.Fatalf("HandleWebhook error = %v, want %v", err, platega.ErrNotInitialized)
		}
		if _, err := adapter.SyncPayment(context.Background(), platega.SyncPaymentParams{}); !errors.Is(err, platega.ErrNotInitialized) {
			t.Fatalf("SyncPayment error = %v, want %v", err, platega.ErrNotInitialized)
		}
		if _, err := adapter.GetH2H(context.Background(), platega.GetH2HParams{}); !errors.Is(err, platega.ErrNotInitialized) {
			t.Fatalf("GetH2H error = %v, want %v", err, platega.ErrNotInitialized)
		}
		if _, err := adapter.Execute(context.Background(), platega.RefundParams{}); !errors.Is(err, platega.ErrNotInitialized) {
			t.Fatalf("Execute error = %v, want %v", err, platega.ErrNotInitialized)
		}
	})

	t.Run("yookassa", func(t *testing.T) {
		var adapter *yookassa.YooKassa

		if _, err := adapter.CreatePayment(context.Background(), yookassa.CreatePaymentParams{}); !errors.Is(err, yookassa.ErrNotInitialized) {
			t.Fatalf("CreatePayment error = %v, want %v", err, yookassa.ErrNotInitialized)
		}
		if _, err := adapter.HandleWebhook(context.Background(), yookassa.WebhookRequest{}); !errors.Is(err, yookassa.ErrNotInitialized) {
			t.Fatalf("HandleWebhook error = %v, want %v", err, yookassa.ErrNotInitialized)
		}
		if _, err := adapter.SyncPayment(context.Background(), yookassa.SyncPaymentParams{}); !errors.Is(err, yookassa.ErrNotInitialized) {
			t.Fatalf("SyncPayment error = %v, want %v", err, yookassa.ErrNotInitialized)
		}
		if _, err := adapter.Execute(context.Background(), yookassa.RefundParams{}); !errors.Is(err, yookassa.ErrNotInitialized) {
			t.Fatalf("Execute error = %v, want %v", err, yookassa.ErrNotInitialized)
		}
	})
}

func TestPaymentYooKassaGetPaymentClientContract(t *testing.T) {
	var receivedPath string
	var receivedUsername string
	var receivedPassword string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedPath = request.URL.Path
		receivedUsername, receivedPassword, _ = request.BasicAuth()
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{
			"id":"payment-1",
			"status":"pending",
			"amount":{"value":"10.00","currency":"RUB"}
		}`)
	}))
	t.Cleanup(server.Close)

	client := yookassa.NewClient(yookassa.Credentials{
		ShopID:     "shop-id",
		SecretKey:  "secret-key",
		APIBaseURL: server.URL,
		HTTPClient: server.Client(),
	})
	if _, err := client.GetPayment(context.Background(), "payment-1"); err != nil {
		t.Fatalf("get YooKassa payment: %v", err)
	}
	if receivedPath != "/v3/payments/payment-1" {
		t.Fatalf("request path = %q", receivedPath)
	}
	if receivedUsername != "shop-id" || receivedPassword != "secret-key" {
		t.Fatalf("basic auth username=%q password=%q", receivedUsername, receivedPassword)
	}

	failedServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "provider rejected request", http.StatusBadRequest)
	}))
	t.Cleanup(failedServer.Close)
	failedClient := yookassa.NewClient(yookassa.Credentials{
		ShopID:     "shop-id",
		SecretKey:  "secret-key",
		APIBaseURL: failedServer.URL,
		HTTPClient: failedServer.Client(),
	})
	_, err := failedClient.GetPayment(context.Background(), "payment-2")
	if err == nil {
		t.Fatal("expected provider error")
	}
	if !strings.Contains(err.Error(), "get payment") {
		t.Fatalf("provider error = %v", err)
	}
	_ = errors.Unwrap(err)
}

func TestPaymentTONAdapterValidationAndRefundExecutor(t *testing.T) {
	env := setupPaymentIntegrationTest(t)

	if _, err := env.api.Adapters.TON.StartSubscriber(env.ctx, paymentton.SubscriberParams{
		WorkspaceID:   "invalid-workspace",
		Network:       "mainnet",
		WalletAddress: "wallet",
	}); err == nil {
		t.Fatal("invalid subscriber workspace must fail")
	}
	if _, err := env.api.Adapters.TON.StartSubscriber(env.ctx, paymentton.SubscriberParams{
		WorkspaceID: testWorkspaceID,
		Network:     "mainnet",
	}); !errors.Is(err, paymentton.ErrWalletAddressRequired) {
		t.Fatalf("empty subscriber wallet error = %v", err)
	}

	if _, err := env.api.Adapters.TON.Execute(env.ctx, paymentton.RefundParams{}); !errors.Is(err, paymentton.ErrRefundUnsupported) {
		t.Fatalf("refund without executor error = %v", err)
	}
	result, err := env.api.Adapters.TON.Execute(env.ctx, paymentton.RefundParams{
		IdempotencyKey: "refund-key",
		Executor: func(_ context.Context, params paymentton.RefundParams) (paymentton.RefundResult, error) {
			if params.IdempotencyKey != "refund-key" {
				return paymentton.RefundResult{}, errors.New("unexpected idempotency key")
			}

			return paymentton.RefundResult{
				ProviderRefundID: "ton-refund-id",
				Status:           "succeeded",
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("execute TON refund: %v", err)
	}
	if result.ProviderRefundID != "ton-refund-id" || result.Status != "succeeded" {
		t.Fatalf("unexpected TON refund result: %#v", result)
	}
}

func TestPaymentSubscriptionProviderIDIsScopedByWorkspace(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	workspaceA := testsupport.WorkspaceID("subscription-owner-a")
	workspaceB := testsupport.WorkspaceID("subscription-owner-b")
	productA := createPaymentProduct(t, env, testProductOptions{WorkspaceID: workspaceA})
	productB := createPaymentProduct(t, env, testProductOptions{WorkspaceID: workspaceB})
	providerSubscriptionID := uniquePaymentID("workspace-subscription")
	startedAt := time.Now().UTC()

	firstID, err := env.api.Admin.UpsertSubscription(env.ctx, admin.SubscriptionUpsertParams{
		WorkspaceID:            workspaceA,
		ProviderCode:           "yookassa",
		ProviderSubscriptionID: providerSubscriptionID,
		AppID:                  1,
		PlatformID:             1,
		PlatformUserID:         "user-a",
		ProductID:              productA,
		Status:                 "active",
		StartedAt:              startedAt,
	})
	if err != nil {
		t.Fatalf("create workspace A subscription: %v", err)
	}

	secondID, err := env.api.Admin.UpsertSubscription(env.ctx, admin.SubscriptionUpsertParams{
		WorkspaceID:            workspaceB,
		ProviderCode:           "yookassa",
		ProviderSubscriptionID: providerSubscriptionID,
		AppID:                  2,
		PlatformID:             1,
		PlatformUserID:         "user-b",
		ProductID:              productB,
		Status:                 "active",
		StartedAt:              startedAt,
	})
	if err != nil {
		t.Fatalf("create workspace B subscription: %v", err)
	}
	if secondID == firstID {
		t.Fatalf("workspace subscriptions share id %d", firstID)
	}

	var workspaceID string
	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT workspace_id FROM payment_subscription WHERE id = $1",
		firstID,
	).Scan(&workspaceID); err != nil {
		t.Fatalf("read subscription owner: %v", err)
	}
	if workspaceID != workspaceA {
		t.Fatalf("subscription workspace = %s, want %s", workspaceID, workspaceA)
	}
}

func TestPaymentCreateOrderRejectsItemSnapshotOverflow(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ListAmountMinor: 1,
	})
	now := time.Now().UTC()
	if err := env.api.Admin.SaveProduct(env.ctx, product.UpsertParams{
		WorkspaceID:    testWorkspaceID,
		ID:             productID,
		TitleKey:       productID + ".title",
		QuantityMode:   "flexible",
		GlobalInterval: "UNLIMITED",
		UserInterval:   "UNLIMITED",
		AvailableFrom:  timePointer(now.Add(-time.Hour)),
		AvailableUntil: timePointer(now.Add(time.Hour)),
		IsVisible:      true,
	}); err != nil {
		t.Fatalf("make product flexible: %v", err)
	}
	var itemID string
	if err := env.db.QueryRowContext(env.ctx, `
		SELECT item_id
		FROM payment_product_item
		WHERE workspace_id = $1 AND product_id = $2
	`, testWorkspaceID, productID).Scan(&itemID); err != nil {
		t.Fatalf("read product item: %v", err)
	}
	if err := env.api.Admin.AttachProductItem(env.ctx, product.AddItemParams{
		WorkspaceID: testWorkspaceID,
		ProductID:   productID,
		ItemID:      itemID,
		Quantity:    2,
	}); err != nil {
		t.Fatalf("update product item quantity: %v", err)
	}

	_, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 1, 1, "overflow-order"),
		ProductID: productID,
		Quantity:  math.MaxInt64,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if !errors.Is(err, repository.ErrPaymentAmountOverflow) {
		t.Fatalf("item snapshot overflow error = %v, want ErrPaymentAmountOverflow", err)
	}

	var count int
	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT COUNT(*) FROM payment_order WHERE workspace_id = $1 AND product_id = $2",
		testWorkspaceID,
		productID,
	).Scan(&count); err != nil {
		t.Fatalf("count overflow orders: %v", err)
	}
	if count != 0 {
		t.Fatalf("overflow order count = %d, want 0", count)
	}
}

func TestPaymentPublicServiceContractsDoNotExposePersistenceTypes(t *testing.T) {
	serviceTypes := []reflect.Type{
		reflect.TypeOf((*admin.Admin)(nil)),
		reflect.TypeOf((*paymentasset.Asset)(nil)),
		reflect.TypeOf((*operational.Operational)(nil)),
		reflect.TypeOf((*paymentrefund.Refund)(nil)),
		reflect.TypeOf((*user.User)(nil)),
	}

	for _, serviceType := range serviceTypes {
		for index := 0; index < serviceType.NumMethod(); index++ {
			method := serviceType.Method(index)
			assertNoPaymentPersistenceType(t, serviceType.String()+"."+method.Name, method.Type, map[reflect.Type]bool{})
		}
	}

	encoded, err := json.Marshal(repository.AdminTONWalletModel{
		WorkspaceID:      testWorkspaceID,
		NetworkConfigUrl: repository.NullableString{},
	})
	if err != nil {
		t.Fatalf("marshal public admin model: %v", err)
	}
	if !strings.Contains(string(encoded), `"network_config_url":null`) {
		t.Fatalf("nullable persistence value leaked into public JSON: %s", encoded)
	}
}

func assertNoPaymentPersistenceType(
	t *testing.T,
	path string,
	typeValue reflect.Type,
	seen map[reflect.Type]bool,
) {
	t.Helper()
	if seen[typeValue] {
		return
	}
	seen[typeValue] = true

	if typeValue.PkgPath() == "github.com/elum2b/services/payment/sqlc" ||
		typeValue.PkgPath() == "database/sql" {
		t.Fatalf("public payment contract %s exposes persistence type %s", path, typeValue)
	}

	switch typeValue.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		assertNoPaymentPersistenceType(t, path, typeValue.Elem(), seen)
	case reflect.Map:
		assertNoPaymentPersistenceType(t, path, typeValue.Key(), seen)
		assertNoPaymentPersistenceType(t, path, typeValue.Elem(), seen)
	case reflect.Func:
		for index := 1; index < typeValue.NumIn(); index++ {
			assertNoPaymentPersistenceType(t, path, typeValue.In(index), seen)
		}
		for index := 0; index < typeValue.NumOut(); index++ {
			assertNoPaymentPersistenceType(t, path, typeValue.Out(index), seen)
		}
	case reflect.Struct:
		if !strings.HasPrefix(typeValue.PkgPath(), "github.com/elum2b/services/payment") {
			return
		}
		for index := 0; index < typeValue.NumField(); index++ {
			field := typeValue.Field(index)
			assertNoPaymentPersistenceType(t, path+"."+field.Name, field.Type, seen)
		}
	}
}

func TestFetchDexScreenerPricesBatchesAddressesAndSelectsLiquidity(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/tokens/v1/ton/token-a,token-b" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`[
			{"baseToken":{"address":"token-a"},"priceUsd":"1.25","liquidity":{"usd":100}},
			{"baseToken":{"address":"token-a"},"priceUsd":"1.30","liquidity":{"usd":500}},
			{"baseToken":{"address":"token-b"},"priceUsd":"0.004","liquidity":{"usd":250}},
			{"baseToken":{"address":"unexpected"},"priceUsd":"999","liquidity":{"usd":999999}}
		]`)),
			Request: r,
		}, nil
	})}

	prices, err := fetchDexScreenerPrices(
		context.Background(),
		client,
		"https://dex.example",
		"ton",
		[]repository.DueAssetRateUpdate{
			{AssetCode: "token-a", AssetKind: "crypto_jetton", SourceTokenAddress: "token-a"},
			{AssetCode: "token-b", AssetKind: "crypto_jetton", SourceTokenAddress: "token-b"},
			{AssetCode: "token-a", AssetKind: "crypto_jetton", SourceTokenAddress: "token-a"},
		},
	)
	if err != nil {
		t.Fatalf("fetch prices: %v", err)
	}
	if prices["token-a"] != 1_300_000 {
		t.Fatalf("unexpected token-a price: %d", prices["token-a"])
	}
	if prices["token-b"] != 4_000 {
		t.Fatalf("unexpected token-b price: %d", prices["token-b"])
	}
	if _, ok := prices["unexpected"]; ok {
		t.Fatal("unexpected token must not be returned")
	}
}

func TestFetchDexScreenerPricesCalculatesNativeTONFromUSDTPair(t *testing.T) {
	const usdtAddress = "EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs"

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/tokens/v1/ton/"+usdtAddress {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`[
				{
					"baseToken":{"address":"` + usdtAddress + `"},
					"quoteToken":{"address":"` + tonNativeDexScreenerAddress + `"},
					"priceNative":"0.6599",
					"priceUsd":"0.9984",
					"liquidity":{"usd":6152967.9}
				}
			]`)),
			Request: r,
		}, nil
	})}

	prices, err := fetchDexScreenerPrices(
		context.Background(),
		client,
		"https://dex.example",
		"ton",
		[]repository.DueAssetRateUpdate{
			{
				AssetCode:          "TON",
				AssetKind:          "crypto_native",
				SourceChainID:      "ton",
				SourceTokenAddress: usdtAddress,
			},
		},
	)
	if err != nil {
		t.Fatalf("fetch native TON price: %v", err)
	}
	if prices["TON"] != 1_512_957 {
		t.Fatalf("native TON price = %d, want 1512957 micro-USDT", prices["TON"])
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSelectDexScreenerPricesRejectsQuoteAndInvalidPrice(t *testing.T) {
	pairs := []dexScreenerPair{{PriceUSD: "0"}}
	pairs[0].BaseToken.Address = "token-a"

	prices := selectDexScreenerPrices(pairs, map[string]struct{}{"token-a": {}})
	if len(prices) != 0 {
		t.Fatalf("expected invalid price to be ignored: %#v", prices)
	}
}

func TestSelectDexScreenerPricesCalculatesQuoteTokenUSDPrice(t *testing.T) {
	pair := dexScreenerPair{
		PriceUSD:    "1.0019",
		PriceNative: "0.5788",
	}
	pair.BaseToken.Address = "usdt"
	pair.QuoteToken.Address = "ton"
	pair.Liquidity = &struct {
		USD float64 `json:"usd"`
	}{USD: 6_525_315}

	prices := selectDexScreenerPrices(
		[]dexScreenerPair{pair},
		map[string]struct{}{"ton": {}},
	)
	if prices["ton"] != 1_730_996 {
		t.Fatalf("unexpected TON price: %d", prices["ton"])
	}
}

func TestMergeContextsCancelsOnLifecycleDone(t *testing.T) {
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	methodCtx := context.Background()

	ctx, cancel := mergeContexts(lifecycleCtx, methodCtx)
	defer cancel()

	lifecycleCancel()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected merged context to be canceled by lifecycle context")
	}
}

func TestMergeContextsCancelsOnMethodDone(t *testing.T) {
	lifecycleCtx := context.Background()
	methodCtx, methodCancel := context.WithCancel(context.Background())

	ctx, cancel := mergeContexts(lifecycleCtx, methodCtx)
	defer cancel()

	methodCancel()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected merged context to be canceled by method context")
	}
}

func TestIsReady(t *testing.T) {
	var nilService *Payment
	if nilService.IsReady() {
		t.Fatal("nil payment must not be ready")
	}
	service := New(DatabaseParams{})
	if service.IsReady() {
		t.Fatal("uninitialized payment must not be ready")
	}
	ctx, cancel := context.WithCancel(context.Background())
	service.rootCtx, service.Admin, service.Operational, service.User = ctx, &admin.Admin{}, &operational.Operational{}, &user.User{}
	service.Adapters = &Adapters{}
	if !service.IsReady() {
		t.Fatal("initialized payment must be ready")
	}
	cancel()
	if service.IsReady() {
		t.Fatal("closed payment must not be ready")
	}
}

const (
	paymentPostgresHost     = "localhost"
	paymentPostgresPort     = 5432
	paymentPostgresUsername = "postgres"
	paymentPostgresPassword = "RBTX0DXKbagvCy2XCAi4qHt0cjeSD6bU"

	mysqlControlHost     = paymentPostgresHost
	mysqlControlUsername = paymentPostgresUsername
	mysqlControlPassword = paymentPostgresPassword

	paymentTestDB = "payment_test"
)

func TestBootstrapRealPostgres(t *testing.T) {
	dbName := paymentTestDB

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminDB, err := openPaymentPostgres("postgres")
	if err != nil {
		t.Fatalf("open admin postgres connection: %v", err)
	}
	defer adminDB.Close()

	if err := recreatePaymentTestDatabase(ctx, adminDB, dbName); err != nil {
		t.Fatalf("recreate database %s: %v", dbName, err)
	}

	appDB, err := openPaymentPostgres(dbName)
	if err != nil {
		t.Fatalf("open payment postgres connection: %v", err)
	}
	defer appDB.Close()

	db, err := sqlwrap.New(appDB)
	if err != nil {
		t.Fatalf("create sql client: %v", err)
	}

	repo := repository.NewPaymentRepository(db)
	if err := repo.Bootstrap(ctx, filepath.Join("sqlc", "schema.sql")); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	payments, err := NewWithDatabase(ctx, appDB, paymentTestOptions())
	if err != nil {
		t.Fatalf("create payment service: %v", err)
	}
	defer payments.Close()
	if payments.User == nil {
		t.Fatal("payment user service is nil")
	}
	if payments.Admin == nil {
		t.Fatal("payment admin service is nil")
	}
	if payments.Adapters == nil {
		t.Fatal("payment adapters are nil")
	}
	if payments.Adapters.VKMA == nil {
		t.Fatal("payment vkma service is nil")
	}
	if payments.Adapters.YooKassa == nil {
		t.Fatal("payment yookassa service is nil")
	}
	if payments.Adapters.Platega == nil {
		t.Fatal("payment platega service is nil")
	}
	if payments.Adapters.TON == nil {
		t.Fatal("payment ton service is nil")
	}
	if payments.Adapters.TelegramStars == nil {
		t.Fatal("payment telegram stars service is nil")
	}

	providers, err := repo.ListProviders(ctx)
	if err != nil {
		t.Fatalf("list providers: %v", err)
	}
	if len(providers) < 5 {
		t.Fatalf("expected seeded providers, got %d", len(providers))
	}

	assets, err := repo.ListAssets(ctx)
	if err != nil {
		t.Fatalf("list assets: %v", err)
	}
	if len(assets) < 6 {
		t.Fatalf("expected seeded assets, got %d", len(assets))
	}

	if _, err := repo.GetProviderAsset(ctx, "yookassa", "RUB"); err != nil {
		t.Fatalf("get yookassa/RUB provider asset: %v", err)
	}
	if _, err := repo.GetProviderAsset(ctx, "ton", "DOGS_TON"); err != nil {
		t.Fatalf("get ton/DOGS_TON provider asset: %v", err)
	}
	if _, err := repo.GetProviderAsset(ctx, "ton", "NOT_TON"); err != nil {
		t.Fatalf("get ton/NOT_TON provider asset: %v", err)
	}
	if _, err := repo.GetProviderAsset(ctx, "ton", "MAJOR_TON"); err != nil {
		t.Fatalf("get ton/MAJOR_TON provider asset: %v", err)
	}
}

func TestRunCreatesDatabaseSchemaAndCallbackTable(t *testing.T) {
	const database = "payment_run_bootstrap_test"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminDB, err := openPaymentPostgres("postgres")
	if err != nil {
		t.Fatalf("open admin postgres connection: %v", err)
	}
	defer adminDB.Close()
	if err := recreatePaymentTestDatabase(ctx, adminDB, database); err != nil {
		t.Fatalf("recreate test database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+quoteIdentifier(database))
	})
	appDB, err := openPaymentPostgres(database)
	if err != nil {
		t.Fatalf("open payment postgres connection: %v", err)
	}
	defer appDB.Close()

	service := New(DatabaseParams{
		User:     paymentPostgresUsername,
		Password: paymentPostgresPassword,
		Database: database,
		Host:     paymentPostgresHost,
		Port:     paymentPostgresPort,
	})
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- service.Run(runCtx)
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		var tableCount int
		err := appDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM information_schema.tables
WHERE table_schema = 'public'
  AND table_name IN ('payment_product', 'payment_clb_event')`).Scan(&tableCount)
		if err == nil && tableCount == 2 {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("Run returned during bootstrap: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			stop()
			t.Fatalf("Run did not complete payment bootstrap: tables=%d err=%v", tableCount, err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Run must still be blocked after the complete schema is ready.
	select {
	case err := <-done:
		t.Fatalf("Run returned before cancellation: %v", err)
	default:
	}

	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}

type completedPaymentFixture struct {
	OrderID           uint64
	AttemptID         uint64
	ProviderCode      string
	ProviderPaymentID string
	AssetCode         string
	AmountMinor       uint64
}

func createCompletedPaymentFixture(
	t *testing.T,
	env paymentTestEnv,
	appID int64,
	suffix string,
) completedPaymentFixture {
	t.Helper()

	productID := createPaymentProduct(t, env, testProductOptions{
		AssetCode:       "RUB",
		ListAmountMinor: 1000,
	})
	order, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(testWorkspaceID, appID, 1, suffix),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	providerPaymentID := uniquePaymentID(suffix)
	attempt, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          paymentAttemptIdentity(order),
		OrderID:           order.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &providerPaymentID,
	})
	if err != nil {
		t.Fatalf("create attempt: %v", err)
	}
	if _, err := env.api.Operational.CompleteAttempt(env.ctx, checkout.CompleteAttemptParams{
		WorkspaceID:       testWorkspaceID,
		AttemptID:         attempt.ID,
		ProviderCode:      attempt.ProviderCode,
		ProviderPaymentID: &providerPaymentID,
		AmountMinor:       attempt.AmountMinor,
		AssetCode:         attempt.AssetCode,
	}); err != nil {
		t.Fatalf("complete attempt: %v", err)
	}

	return completedPaymentFixture{
		OrderID:           order.ID,
		AttemptID:         attempt.ID,
		ProviderCode:      attempt.ProviderCode,
		ProviderPaymentID: providerPaymentID,
		AssetCode:         attempt.AssetCode,
		AmountMinor:       attempt.AmountMinor,
	}
}

func TestPaymentCompleteAttemptRejectsMismatchedReplay(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	fixture := createCompletedPaymentFixture(t, env, 7001, "replay-mismatch")
	wrongProviderPaymentID := fixture.ProviderPaymentID + "-forged"

	_, err := env.api.Operational.CompleteAttempt(env.ctx, checkout.CompleteAttemptParams{
		WorkspaceID:       testWorkspaceID,
		AttemptID:         fixture.AttemptID,
		ProviderCode:      "platega",
		ProviderPaymentID: &wrongProviderPaymentID,
		AmountMinor:       fixture.AmountMinor + 1,
		AssetCode:         "VOTE",
	})
	if !errors.Is(err, repository.ErrPaymentMismatch) {
		t.Fatalf("completed replay with forged fields must be rejected, got %v", err)
	}
}

func TestPaymentCompleteAttemptRejectsAnotherWorkspace(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		AssetCode:       "RUB",
		ListAmountMinor: 1000,
	})
	order, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 7002, 1, "workspace-scope"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create scoped order: %v", err)
	}
	providerPaymentID := uniquePaymentID("workspace-scope")
	attempt, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          paymentAttemptIdentity(order),
		OrderID:           order.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &providerPaymentID,
	})
	if err != nil {
		t.Fatalf("create scoped attempt: %v", err)
	}

	_, err = env.api.Operational.CompleteAttempt(env.ctx, checkout.CompleteAttemptParams{
		WorkspaceID:       testsupport.WorkspaceID("another-payment-workspace"),
		AttemptID:         attempt.ID,
		ProviderCode:      attempt.ProviderCode,
		ProviderPaymentID: &providerPaymentID,
		AmountMinor:       attempt.AmountMinor,
		AssetCode:         attempt.AssetCode,
	})
	if !errors.Is(err, repository.ErrPaymentMismatch) {
		t.Fatalf("cross-workspace completion error = %v, want payment mismatch", err)
	}
	assertOrderStatus(t, env.ctx, env.db, order.ID, "pending_payment")
	assertAttemptStatus(t, env.ctx, env.db, attempt.ID, "pending")
}

func TestPaymentCreateAttemptRequiresOrderOwner(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		AssetCode:       "RUB",
		ListAmountMinor: 1000,
	})
	owner := paymentTestIdentity(testWorkspaceID, 7010, 1, "attempt-owner")
	order, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  owner,
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	foreignPaymentID := uniquePaymentID("foreign-attempt")
	_, err = env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          paymentTestIdentity(testWorkspaceID, 7010, 1, "another-user"),
		OrderID:           order.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &foreignPaymentID,
	})
	if !errors.Is(err, repository.ErrPaymentMismatch) {
		t.Fatalf("foreign attempt must be rejected, got %v", err)
	}

	ownerPaymentID := uniquePaymentID("owner-attempt")
	if _, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          owner,
		OrderID:           order.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &ownerPaymentID,
	}); err != nil {
		t.Fatalf("owner attempt: %v", err)
	}

	recipient := paymentTestIdentity(testWorkspaceID, 7010, 1, "gift-recipient")
	payer := services.Identity{
		WorkspaceID:    testWorkspaceID,
		AppID:          recipient.AppID,
		PlatformID:     1,
		PlatformUserID: "gift-payer",
	}
	giftOrder, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity: recipient,
		Payer: &services.Actor{
			PlatformID:     payer.PlatformID,
			PlatformUserID: payer.PlatformUserID,
		},
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create direct gift order: %v", err)
	}
	if giftOrder.PlatformUserID != recipient.PlatformUserID {
		t.Fatalf("gift recipient = %q, want %q", giftOrder.PlatformUserID, recipient.PlatformUserID)
	}
	if giftOrder.PayerPlatformUserID == nil || *giftOrder.PayerPlatformUserID != payer.PlatformUserID {
		t.Fatalf("gift payer = %v, want %q", giftOrder.PayerPlatformUserID, payer.PlatformUserID)
	}

	giftPaymentID := uniquePaymentID("direct-gift-attempt")
	if _, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          payer,
		OrderID:           giftOrder.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &giftPaymentID,
	}); err != nil {
		t.Fatalf("gift payer attempt: %v", err)
	}
}

func TestPaymentAdminRejectsTerminalLifecycleRegression(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	fixture := createCompletedPaymentFixture(t, env, 7011, "terminal-regression")

	err := env.api.Admin.UpdatePaymentAttemptStatus(env.ctx, admin.AttemptStatusParams{
		WorkspaceID: testWorkspaceID,
		ID:          fixture.AttemptID,
		Status:      "pending",
	})
	if !errors.Is(err, repository.ErrAttemptStateInvalid) {
		t.Fatalf("succeeded attempt regression must be rejected, got %v", err)
	}

	var attemptStatus string
	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT status::text FROM payment_attempt WHERE id = $1",
		fixture.AttemptID,
	).Scan(&attemptStatus); err != nil {
		t.Fatalf("read attempt status: %v", err)
	}
	if attemptStatus != "succeeded" {
		t.Fatalf("attempt status = %q, want succeeded", attemptStatus)
	}

	var fulfillmentID uint64
	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT id FROM payment_fulfillment WHERE order_id = $1",
		fixture.OrderID,
	).Scan(&fulfillmentID); err != nil {
		t.Fatalf("read fulfillment: %v", err)
	}
	_, err = env.api.Admin.UpdateFulfillmentStatus(env.ctx, admin.FulfillmentStatusParams{
		WorkspaceID: testWorkspaceID,
		ID:          fulfillmentID,
		Status:      "failed",
		Message:     "must not overwrite success",
	})
	if !errors.Is(err, repository.ErrFulfillmentStateInvalid) {
		t.Fatalf("succeeded fulfillment regression must be rejected, got %v", err)
	}

	var fulfillmentStatus string
	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT status::text FROM payment_fulfillment WHERE id = $1",
		fulfillmentID,
	).Scan(&fulfillmentStatus); err != nil {
		t.Fatalf("read fulfillment status: %v", err)
	}
	if fulfillmentStatus != "succeeded" {
		t.Fatalf("fulfillment status = %q, want succeeded", fulfillmentStatus)
	}
}

func TestPaymentAdminCannotBypassOperationalCompletion(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		AssetCode:       "RUB",
		ListAmountMinor: 1000,
	})
	owner := paymentTestIdentity(testWorkspaceID, 7012, 1, "admin-completion-owner")
	order, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  owner,
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	paymentID := uniquePaymentID("admin-completion")
	attempt, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          owner,
		OrderID:           order.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &paymentID,
	})
	if err != nil {
		t.Fatalf("create attempt: %v", err)
	}

	err = env.api.Admin.UpdatePaymentAttemptStatus(env.ctx, admin.AttemptStatusParams{
		WorkspaceID: testWorkspaceID,
		ID:          attempt.ID,
		Status:      "succeeded",
	})
	if !errors.Is(err, repository.ErrAttemptStateInvalid) {
		t.Fatalf("admin completion bypass must be rejected, got %v", err)
	}

	var attemptStatus string
	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT status::text FROM payment_attempt WHERE id = $1",
		attempt.ID,
	).Scan(&attemptStatus); err != nil {
		t.Fatalf("read attempt status: %v", err)
	}
	if attemptStatus != "pending" {
		t.Fatalf("attempt status = %q, want pending", attemptStatus)
	}

	var fulfillmentCount int
	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT COUNT(*) FROM payment_fulfillment WHERE order_id = $1",
		order.ID,
	).Scan(&fulfillmentCount); err != nil {
		t.Fatalf("count fulfillments: %v", err)
	}
	if fulfillmentCount != 0 {
		t.Fatalf("admin status update created %d fulfillments", fulfillmentCount)
	}
}

func TestPaymentRefundRejectsCumulativeAmountAbovePayment(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	fixture := createCompletedPaymentFixture(t, env, 7002, "refund-total")
	params := admin.RefundCreateParams{
		WorkspaceID:  testWorkspaceID,
		OrderID:      fixture.OrderID,
		AttemptID:    fixture.AttemptID,
		ProviderCode: fixture.ProviderCode,
		AmountMinor:  fixture.AmountMinor,
		AssetCode:    fixture.AssetCode,
		Status:       "pending",
	}

	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := env.api.Admin.CreateRefund(env.ctx, params)
			results <- err
		}()
	}
	group.Wait()
	close(results)

	var succeeded, rejected int
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, repository.ErrPaymentMismatch):
			rejected++
		default:
			t.Fatalf("unexpected concurrent refund error: %v", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("concurrent full refunds: succeeded=%d rejected=%d", succeeded, rejected)
	}
}

func TestPaymentProviderRefundIDCannotMoveBetweenOrders(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	first := createCompletedPaymentFixture(t, env, 7013, "refund-owner-first")
	second := createCompletedPaymentFixture(t, env, 7014, "refund-owner-second")
	repo := repository.NewPaymentRepository(env.client)
	providerRefundID := uniquePaymentID("shared-provider-refund")
	firstEventID := uniquePaymentID("first-refund-event")
	secondEventID := uniquePaymentID("second-refund-event")

	if _, err := repo.ApplyProviderRefund(env.ctx, repository.ProviderRefundParams{
		WorkspaceID:       testWorkspaceID,
		ProviderCode:      first.ProviderCode,
		ProviderPaymentID: first.ProviderPaymentID,
		ProviderRefundID:  providerRefundID,
		Event: repository.EventCreateParams{
			ProviderCode:    first.ProviderCode,
			ProviderEventID: &firstEventID,
			EventType:       "refund.succeeded",
			PayloadHash:     sha256Hex(firstEventID),
		},
	}); err != nil {
		t.Fatalf("apply first provider refund: %v", err)
	}

	if _, err := repo.ApplyProviderRefund(env.ctx, repository.ProviderRefundParams{
		WorkspaceID:       testWorkspaceID,
		ProviderCode:      second.ProviderCode,
		ProviderPaymentID: second.ProviderPaymentID,
		ProviderRefundID:  providerRefundID,
		Event: repository.EventCreateParams{
			ProviderCode:    second.ProviderCode,
			ProviderEventID: &secondEventID,
			EventType:       "refund.succeeded",
			PayloadHash:     sha256Hex(secondEventID),
		},
	}); !errors.Is(err, repository.ErrPaymentMismatch) {
		t.Fatalf("provider refund id moved to another order: %v", err)
	}

	assertOrderStatus(t, env.ctx, env.db, first.OrderID, "refunded")
	assertOrderStatus(t, env.ctx, env.db, second.OrderID, "fulfilled")

	var refundCount int
	if err := env.db.QueryRowContext(
		env.ctx,
		`SELECT COUNT(*) FROM payment_refund WHERE workspace_id = $1 AND provider_refund_id = $2`,
		testWorkspaceID,
		providerRefundID,
	).Scan(&refundCount); err != nil {
		t.Fatalf("count provider refunds: %v", err)
	}
	if refundCount != 1 {
		t.Fatalf("provider refund rows = %d, want 1", refundCount)
	}
}

func TestPaymentRefundedOrderRejectsDifferentProviderRefundID(t *testing.T) {

	env := setupPaymentIntegrationTest(t)
	fixture := createCompletedPaymentFixture(t, env, 7015, "refund-id-immutable")
	repo := repository.NewPaymentRepository(env.client)
	firstRefundID := uniquePaymentID("provider-refund-first")
	firstEventID := uniquePaymentID("provider-refund-first-event")

	first, err := repo.ApplyProviderRefund(env.ctx, repository.ProviderRefundParams{
		WorkspaceID:       testWorkspaceID,
		ProviderCode:      fixture.ProviderCode,
		ProviderPaymentID: fixture.ProviderPaymentID,
		ProviderRefundID:  firstRefundID,
		Event: repository.EventCreateParams{
			ProviderCode:    fixture.ProviderCode,
			ProviderEventID: &firstEventID,
			EventType:       "refund.succeeded",
			PayloadHash:     sha256Hex(firstEventID),
		},
	})
	if err != nil {
		t.Fatalf("apply first provider refund: %v", err)
	}

	secondRefundID := uniquePaymentID("provider-refund-second")
	secondEventID := uniquePaymentID("provider-refund-second-event")
	if _, err := repo.ApplyProviderRefund(env.ctx, repository.ProviderRefundParams{
		WorkspaceID:       testWorkspaceID,
		ProviderCode:      fixture.ProviderCode,
		ProviderPaymentID: fixture.ProviderPaymentID,
		ProviderRefundID:  secondRefundID,
		Event: repository.EventCreateParams{
			ProviderCode:    fixture.ProviderCode,
			ProviderEventID: &secondEventID,
			EventType:       "refund.succeeded",
			PayloadHash:     sha256Hex(secondEventID),
		},
	}); !errors.Is(err, repository.ErrPaymentMismatch) {
		t.Fatalf("changed provider refund id error = %v, want ErrPaymentMismatch", err)
	}

	var refundCount int
	var storedRefundID string
	if err := env.db.QueryRowContext(env.ctx, `
SELECT COUNT(*), MIN(provider_refund_id)
FROM payment_refund
WHERE workspace_id = $1 AND order_id = $2
`, testWorkspaceID, fixture.OrderID).Scan(&refundCount, &storedRefundID); err != nil {
		t.Fatalf("read immutable provider refund: %v", err)
	}
	if refundCount != 1 || storedRefundID != firstRefundID {
		t.Fatalf(
			"refund rows=%d provider_refund_id=%q, want one row with %q",
			refundCount,
			storedRefundID,
			firstRefundID,
		)
	}
	if first.RefundID == 0 {
		t.Fatal("first provider refund id was not returned")
	}

}

func TestPaymentOrderRejectsTerminalStatusRegression(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	fixture := createCompletedPaymentFixture(t, env, 7003, "terminal-regression")

	for _, status := range []string{"canceled", "expired", "failed", "refunded", "chargebacked"} {
		if _, err := env.api.Admin.UpdateOrderStatus(
			env.ctx,
			testWorkspaceID,
			fixture.OrderID,
			status,
		); !errors.Is(err, repository.ErrOrderStateInvalid) {
			t.Fatalf("fulfilled order must not transition directly to %s, got %v", status, err)
		}
	}
	assertOrderStatus(t, env.ctx, env.db, fixture.OrderID, "fulfilled")
}

func TestPaymentSuccessfulAdminRefundRevokesFulfillment(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	fixture := createCompletedPaymentFixture(t, env, 7004, "refund-compensation")
	refundService := paymentrefund.New(env.ctx, env.client, map[string]paymentrefund.ProviderRefundFunc{
		"yookassa": func(
			_ context.Context,
			_ paymentrefund.ProviderRefundParams,
		) (paymentrefund.ProviderRefundResult, error) {
			return paymentrefund.ProviderRefundResult{
				ProviderRefundID: uniquePaymentID("provider-refund"),
				Status:           "succeeded",
			}, nil
		},
	})
	t.Cleanup(func() { _ = refundService.Close() })

	if _, err := refundService.Execute(env.ctx, paymentrefund.Params{
		WorkspaceID:    testWorkspaceID,
		OrderID:        fixture.OrderID,
		AttemptID:      fixture.AttemptID,
		IdempotencyKey: "refund-compensation-regression",
		Reason:         "regression test",
	}); err != nil {
		t.Fatalf("execute refund: %v", err)
	}

	var fulfillmentStatus string
	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT status::text FROM payment_fulfillment WHERE order_id = $1",
		fixture.OrderID,
	).Scan(&fulfillmentStatus); err != nil {
		t.Fatalf("read fulfillment: %v", err)
	}
	if fulfillmentStatus != "revoked" {
		t.Fatalf("successful refund left fulfillment active: %s", fulfillmentStatus)
	}

	var callbacks int
	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT COUNT(*) FROM payment_clb_event WHERE workspace_id = $1 AND event_type = 'payment.order.refunded'",
		testWorkspaceID,
	).Scan(&callbacks); err != nil {
		t.Fatalf("count refund callbacks: %v", err)
	}
	if callbacks != 1 {
		t.Fatalf("successful refund callbacks = %d, want 1", callbacks)
	}
}

func TestPaymentAdminRefundStatusUsesCompensationFlow(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	fixture := createCompletedPaymentFixture(t, env, 7006, "refund-status-compensation")
	refundID, err := env.api.Admin.CreateRefund(env.ctx, admin.RefundCreateParams{
		WorkspaceID:  testWorkspaceID,
		OrderID:      fixture.OrderID,
		AttemptID:    fixture.AttemptID,
		ProviderCode: fixture.ProviderCode,
		AmountMinor:  fixture.AmountMinor,
		AssetCode:    fixture.AssetCode,
		Status:       "pending",
	})
	if err != nil {
		t.Fatalf("create pending refund: %v", err)
	}
	if _, err := env.api.Admin.UpdateRefundStatus(env.ctx, admin.RefundStatusParams{
		WorkspaceID: testWorkspaceID,
		ID:          refundID,
		Status:      "succeeded",
		Reason:      "manual provider confirmation",
	}); err != nil {
		t.Fatalf("finalize admin refund: %v", err)
	}

	assertOrderStatus(t, env.ctx, env.db, fixture.OrderID, "refunded")
	var fulfillmentStatus string
	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT status::text FROM payment_fulfillment WHERE order_id = $1",
		fixture.OrderID,
	).Scan(&fulfillmentStatus); err != nil {
		t.Fatalf("read fulfillment: %v", err)
	}
	if fulfillmentStatus != "revoked" {
		t.Fatalf("manual refund status left fulfillment active: %s", fulfillmentStatus)
	}
	if _, err := env.api.Admin.UpdateRefundStatus(env.ctx, admin.RefundStatusParams{
		WorkspaceID: testWorkspaceID,
		ID:          refundID,
		Status:      "failed",
	}); !errors.Is(err, repository.ErrOrderStateInvalid) {
		t.Fatalf("succeeded refund must not regress to failed, got %v", err)
	}
}

func TestPaymentAdminRefundRequiresWorkspaceScope(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	fixture := createCompletedPaymentFixture(t, env, 7005, "refund-scope")
	providerCalled := false
	refundService := paymentrefund.New(env.ctx, env.client, map[string]paymentrefund.ProviderRefundFunc{
		"yookassa": func(
			_ context.Context,
			_ paymentrefund.ProviderRefundParams,
		) (paymentrefund.ProviderRefundResult, error) {
			providerCalled = true
			return paymentrefund.ProviderRefundResult{}, nil
		},
	})
	t.Cleanup(func() { _ = refundService.Close() })

	if _, err := refundService.Execute(env.ctx, paymentrefund.Params{
		OrderID:   fixture.OrderID,
		AttemptID: fixture.AttemptID,
		Reason:    "regression test",
	}); !errors.Is(err, services.ErrIdentityWorkspaceRequired) {
		t.Fatalf("refund with empty workspace must fail validation, got %v", err)
	}
	if providerCalled {
		t.Fatal("refund provider called before workspace validation")
	}
}

func openPaymentPostgres(database string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		paymentPostgresHost,
		paymentPostgresPort,
		paymentPostgresUsername,
		paymentPostgresPassword,
		database,
	)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func openMySQL(_ string, dbName string) (*sql.DB, error) {
	if dbName == "" {
		dbName = "postgres"
	}
	return openPaymentPostgres(dbName)
}

func paymentTestDSN(t interface{ Helper() }) string {
	t.Helper()
	return ""
}

func recreatePaymentTestDatabase(ctx context.Context, db *sql.DB, dbName string) error {
	if _, err := db.ExecContext(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1", dbName); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoteIdentifier(dbName)); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, "CREATE DATABASE "+quoteIdentifier(dbName))
	return err
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func paymentTestIdentity(workspaceID string, appID int64, platformID int64, platformUserID string) services.Identity {
	return services.Identity{
		WorkspaceID:    workspaceID,
		AppID:          appID,
		PlatformID:     platformID,
		PlatformUserID: platformUserID,
	}
}

func paymentAttemptIdentity(order *checkout.Order) services.Identity {
	platformID := order.PlatformID
	platformUserID := order.PlatformUserID
	if order.PayerPlatformID != nil && order.PayerPlatformUserID != nil {
		platformID = int64(*order.PayerPlatformID)
		platformUserID = *order.PayerPlatformUserID
	}

	return paymentTestIdentity(
		order.WorkspaceID,
		order.AppID,
		platformID,
		platformUserID,
	)
}

func TestPaymentAssetCRUD(t *testing.T) {
	env := setupPaymentIntegrationTest(t)

	chain := "ton"
	network := "mainnet"
	contract := "EQ_TEST_ASSET_CRUD"
	minAmount := int64(1)

	if err := env.api.Operational.UpsertAsset(env.ctx, operational.AssetUpsertParams{
		Code:            "CRUD_TON",
		Title:           "CRUD Token",
		AssetKind:       string(paymentsqlc.PaymentAssetAssetKindCryptoJetton),
		Scale:           9,
		Chain:           &chain,
		Network:         &network,
		ContractAddress: &contract,
		IsActive:        true,
	}); err != nil {
		t.Fatalf("upsert asset: %v", err)
	}

	if err := env.api.Operational.UpsertProviderAsset(env.ctx, operational.ProviderAssetUpsertParams{
		ProviderCode:   "ton",
		AssetCode:      "CRUD_TON",
		MinAmountMinor: &minAmount,
		IsActive:       true,
	}); err != nil {
		t.Fatalf("upsert provider asset: %v", err)
	}

	providerAsset, err := env.api.Admin.GetProviderAsset(env.ctx, "ton", "CRUD_TON")
	if err != nil {
		t.Fatalf("get provider asset: %v", err)
	}
	if !providerAsset.IsActive || !providerAsset.MinAmountMinor.Valid || providerAsset.MinAmountMinor.Int64 != minAmount {
		t.Fatalf("unexpected provider asset: %#v", providerAsset)
	}

	if err := env.api.Operational.UpsertAsset(env.ctx, operational.AssetUpsertParams{
		Code:            "CRUD_TON",
		Title:           "CRUD Token Updated",
		AssetKind:       string(paymentsqlc.PaymentAssetAssetKindCryptoJetton),
		Scale:           6,
		Chain:           &chain,
		Network:         &network,
		ContractAddress: &contract,
		IsActive:        true,
	}); err != nil {
		t.Fatalf("update asset: %v", err)
	}

	assets, err := env.api.User.ListAssets(env.ctx, user.ListAssetsParams{})
	if err != nil {
		t.Fatalf("list assets: %v", err)
	}
	found := false
	for _, row := range assets {
		if row.Code == "CRUD_TON" {
			found = row.Title == "CRUD Token Updated" && row.Scale == 6
			break
		}
	}
	if !found {
		t.Fatal("expected updated CRUD_TON asset in list")
	}

	if rows, err := env.api.Operational.DeleteProviderAsset(env.ctx, "ton", "CRUD_TON"); err != nil || rows != 1 {
		t.Fatalf("delete provider asset rows=%d err=%v", rows, err)
	}
	if rows, err := env.api.Operational.DeleteAsset(env.ctx, "CRUD_TON"); err != nil || rows != 1 {
		t.Fatalf("delete asset rows=%d err=%v", rows, err)
	}
}

func TestPaymentAdminCatalogSurface(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	now := time.Now().UTC()
	groupCode := uniquePaymentID("admin-group")
	productID := uniquePaymentID("admin-product")
	itemID := uniquePaymentID("admin-item")
	titleKey := productID + ".title"
	descriptionKey := productID + ".description"
	startsAt := now.Add(-time.Hour)
	endsAt := now.Add(time.Hour)

	providers, err := env.api.Admin.ListProviders(env.ctx)
	if err != nil {
		t.Fatalf("list providers: %v", err)
	}
	if len(providers) == 0 {
		t.Fatal("expected seeded payment providers")
	}
	provider, err := env.api.Admin.GetProvider(env.ctx, "ton")
	if err != nil {
		t.Fatalf("get provider: %v", err)
	}
	if provider.Code != "ton" {
		t.Fatalf("provider code = %q, want ton", provider.Code)
	}

	assets, err := env.api.Admin.ListAssets(env.ctx)
	if err != nil {
		t.Fatalf("list assets: %v", err)
	}
	if len(assets) == 0 {
		t.Fatal("expected seeded payment assets")
	}
	asset, err := env.api.Admin.GetAsset(env.ctx, "RUB")
	if err != nil {
		t.Fatalf("get asset: %v", err)
	}
	if asset.Code != "RUB" {
		t.Fatalf("asset code = %q, want RUB", asset.Code)
	}

	providerAssets, err := env.api.Admin.ListProviderAssets(env.ctx, admin.ProviderAssetListParams{
		ProviderCode: "ton",
		AssetCode:    "TON",
	})
	if err != nil {
		t.Fatalf("list provider assets: %v", err)
	}
	if len(providerAssets) != 1 {
		t.Fatalf("provider asset count = %d, want 1", len(providerAssets))
	}

	if _, err := env.api.Operational.UpdateAssetRate(env.ctx, operational.UpdateAssetRateParams{
		AssetCode:              "TON",
		ReferenceAssetCode:     "RUB",
		ReferencePerAssetMinor: 12500,
		Source:                 "test",
		ObservedAt:             now,
	}); err != nil {
		t.Fatalf("update asset rate: %v", err)
	}
	if err := env.api.Operational.ConfigureAssetRateAutoUpdate(env.ctx, operational.ConfigureAssetRateAutoUpdateParams{
		AssetCode:          "TON",
		ReferenceAssetCode: "RUB",
		Enabled:            true,
		Source:             operational.AssetRateSourceDexScreener,
		SourceChainID:      "ton",
	}); err != nil {
		t.Fatalf("configure asset rate auto update: %v", err)
	}
	rate, err := env.api.Admin.GetAssetRate(env.ctx, "TON", "RUB")
	if err != nil {
		t.Fatalf("get asset rate: %v", err)
	}
	if rate.ReferencePerAssetMinor != 12500 || !rate.AutoUpdateEnabled {
		t.Fatalf("unexpected asset rate: %#v", rate)
	}
	rates, err := env.api.Admin.ListAssetRates(env.ctx, admin.AssetRateListParams{
		AssetCode:          "TON",
		ReferenceAssetCode: "RUB",
	})
	if err != nil {
		t.Fatalf("list asset rates: %v", err)
	}
	if len(rates) != 1 {
		t.Fatalf("asset rate count = %d, want 1", len(rates))
	}

	if err := env.api.Admin.UpsertProductGroup(env.ctx, admin.ProductGroupUpsertParams{
		WorkspaceID:    testWorkspaceID,
		Code:           groupCode,
		TitleKey:       utils.Ref(groupCode + ".title"),
		DescriptionKey: utils.Ref(groupCode + ".description"),
		Position:       10,
		IsActive:       true,
	}); err != nil {
		t.Fatalf("upsert product group: %v", err)
	}
	group, err := env.api.Admin.GetProductGroup(env.ctx, testWorkspaceID, groupCode)
	if err != nil {
		t.Fatalf("get product group: %v", err)
	}
	if group.Code != groupCode || group.Position != 10 {
		t.Fatalf("unexpected product group: %#v", group)
	}
	groups, err := env.api.Admin.ListProductGroups(env.ctx, admin.ProductGroupListParams{
		WorkspaceID: testWorkspaceID,
	})
	if err != nil {
		t.Fatalf("list product groups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("product group count = %d, want 1", len(groups))
	}

	if err := env.api.Admin.UpsertLocalization(env.ctx, admin.LocalizationUpsertParams{
		WorkspaceID:     testWorkspaceID,
		Locale:          "ru",
		LocalizationKey: titleKey,
		Value:           "Административный товар",
	}); err != nil {
		t.Fatalf("upsert localization: %v", err)
	}
	localization, err := env.api.Admin.GetLocalization(env.ctx, testWorkspaceID, "ru", titleKey)
	if err != nil {
		t.Fatalf("get localization: %v", err)
	}
	if localization.Value != "Административный товар" {
		t.Fatalf("localization value = %q", localization.Value)
	}
	localizations, err := env.api.Admin.ListLocalizations(env.ctx, admin.LocalizationListParams{
		WorkspaceID: testWorkspaceID,
		Locale:      "ru",
	})
	if err != nil {
		t.Fatalf("list localizations: %v", err)
	}
	if len(localizations) != 1 {
		t.Fatalf("localization count = %d, want 1", len(localizations))
	}

	if err := env.api.Admin.UpsertProduct(env.ctx, admin.ProductUpsertParams{
		WorkspaceID:    testWorkspaceID,
		ID:             productID,
		GroupCode:      utils.Ref(groupCode),
		TitleKey:       titleKey,
		DescriptionKey: utils.Ref(descriptionKey),
		QuantityMode:   "fixed",
		Position:       20,
		GlobalInterval: "UNLIMITED",
		UserInterval:   "UNLIMITED",
		AvailableFrom:  &startsAt,
		AvailableUntil: &endsAt,
		IsVisible:      true,
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}
	productValue, err := env.api.Admin.GetProduct(env.ctx, testWorkspaceID, productID)
	if err != nil {
		t.Fatalf("get product: %v", err)
	}
	if productValue.ID != productID || productValue.Position != 20 {
		t.Fatalf("unexpected product: %#v", productValue)
	}
	products, err := env.api.Admin.ListProducts(env.ctx, admin.ProductListParams{
		WorkspaceID: testWorkspaceID,
		GroupCode:   groupCode,
	})
	if err != nil {
		t.Fatalf("list products: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("product count = %d, want 1", len(products))
	}

	if err := env.api.Admin.UpsertProductItem(env.ctx, admin.ProductItemUpsertParams{
		WorkspaceID: testWorkspaceID,
		ProductID:   productID,
		ItemID:      itemID,
		RewardType:  "quantity",
		Quantity:    100,
		Scale:       2,
	}); err != nil {
		t.Fatalf("upsert product item: %v", err)
	}
	if err := env.api.Admin.UpsertProductItem(env.ctx, admin.ProductItemUpsertParams{
		WorkspaceID: testWorkspaceID,
		ProductID:   productID,
		ItemID:      itemID,
		RewardType:  "quantity",
		Quantity:    0,
	}); !errors.Is(err, repository.ErrInvalidItemQuantity) {
		t.Fatalf("zero quantity error = %v, want ErrInvalidItemQuantity", err)
	}
	items, err := env.api.Admin.ListProductItems(env.ctx, admin.ProductItemListParams{
		WorkspaceID: testWorkspaceID,
		ProductID:   productID,
		ItemID:      itemID,
	})
	if err != nil {
		t.Fatalf("list product items: %v", err)
	}
	if len(items) != 1 || items[0].Scale != 2 {
		t.Fatalf("unexpected product items: %#v", items)
	}

	priceID, err := env.api.Admin.CreatePrice(env.ctx, admin.ProductPriceCreateParams{
		WorkspaceID:         testWorkspaceID,
		ProductID:           productID,
		AssetCode:           "RUB",
		ListAmountMinor:     1000,
		DiscountAmountMinor: 100,
		IsPromotion:         true,
		StartsAt:            &startsAt,
		EndsAt:              &endsAt,
	})
	if err != nil {
		t.Fatalf("create price: %v", err)
	}
	price, err := env.api.Admin.GetPrice(env.ctx, testWorkspaceID, priceID)
	if err != nil {
		t.Fatalf("get price: %v", err)
	}
	if price.ListAmountMinor != 1000 {
		t.Fatalf("price amount = %d, want 1000", price.ListAmountMinor)
	}
	rows, err := env.api.Admin.UpdatePrice(env.ctx, admin.ProductPriceUpdateParams{
		ID:                  priceID,
		WorkspaceID:         testWorkspaceID,
		AssetCode:           "RUB",
		ListAmountMinor:     1200,
		DiscountAmountMinor: 200,
		IsPromotion:         true,
		StartsAt:            &startsAt,
		EndsAt:              &endsAt,
	})
	if err != nil || rows != 1 {
		t.Fatalf("update price rows=%d err=%v", rows, err)
	}
	prices, err := env.api.Admin.ListPrices(env.ctx, admin.PriceListParams{
		WorkspaceID: testWorkspaceID,
		ProductID:   productID,
		AssetCode:   "RUB",
	})
	if err != nil {
		t.Fatalf("list prices: %v", err)
	}
	if len(prices) != 1 || prices[0].ListAmountMinor != 1200 {
		t.Fatalf("unexpected prices: %#v", prices)
	}

	if _, err := env.api.Admin.GetProduct(env.ctx, testsupport.WorkspaceID("payment-catalog-other"), productID); err == nil {
		t.Fatal("cross-workspace product read must fail")
	}
	if rows, err := env.api.Admin.DeletePrice(env.ctx, testWorkspaceID, priceID); err != nil || rows != 1 {
		t.Fatalf("delete price rows=%d err=%v", rows, err)
	}
	if rows, err := env.api.Admin.DeleteProductItem(env.ctx, testWorkspaceID, productID, itemID); err != nil || rows != 1 {
		t.Fatalf("delete product item rows=%d err=%v", rows, err)
	}
	if rows, err := env.api.Admin.DeleteProduct(env.ctx, testWorkspaceID, productID); err != nil || rows != 1 {
		t.Fatalf("delete product rows=%d err=%v", rows, err)
	}
	if rows, err := env.api.Admin.DeleteLocalization(env.ctx, testWorkspaceID, "ru", titleKey); err != nil || rows != 1 {
		t.Fatalf("delete localization rows=%d err=%v", rows, err)
	}
	if rows, err := env.api.Admin.DeleteProductGroup(env.ctx, testWorkspaceID, groupCode); err != nil || rows != 1 {
		t.Fatalf("delete product group rows=%d err=%v", rows, err)
	}
}

func TestPaymentAdminCommerceSurface(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	fixture := createCompletedPaymentFixture(t, env, 7301, "admin-commerce")

	order, err := env.api.Admin.GetOrder(env.ctx, admin.OrderRefParams{
		WorkspaceID: testWorkspaceID,
		ID:          fixture.OrderID,
	})
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if uint64(order.ID) != fixture.OrderID || order.Status != "fulfilled" {
		t.Fatalf("unexpected order: %#v", order)
	}
	orderByPublicID, err := env.api.Admin.GetOrderByPublicID(env.ctx, admin.OrderPublicRefParams{
		WorkspaceID: testWorkspaceID,
		PublicID:    order.PublicID,
	})
	if err != nil {
		t.Fatalf("get order by public id: %v", err)
	}
	if orderByPublicID.ID != order.ID {
		t.Fatalf("order by public id = %d, want %d", orderByPublicID.ID, order.ID)
	}
	if _, err := env.api.Admin.GetOrder(env.ctx, admin.OrderRefParams{
		WorkspaceID: testsupport.WorkspaceID("payment-commerce-other"),
		ID:          fixture.OrderID,
	}); err == nil {
		t.Fatal("cross-workspace order read must fail")
	}

	attempt, err := env.api.Admin.GetPaymentAttempt(env.ctx, admin.AttemptRefParams{
		WorkspaceID: testWorkspaceID,
		ID:          fixture.AttemptID,
	})
	if err != nil {
		t.Fatalf("get payment attempt: %v", err)
	}
	if uint64(attempt.ID) != fixture.AttemptID || attempt.Status != "succeeded" {
		t.Fatalf("unexpected payment attempt: %#v", attempt)
	}

	fulfillments, err := env.api.Admin.ListFulfillments(env.ctx, admin.FulfillmentListParams{
		WorkspaceID: testWorkspaceID,
		OrderID:     fixture.OrderID,
	})
	if err != nil {
		t.Fatalf("list fulfillments: %v", err)
	}
	if len(fulfillments) != 1 {
		t.Fatalf("fulfillment count = %d, want 1", len(fulfillments))
	}
	fulfillmentID := uint64(fulfillments[0].ID)
	fulfillment, err := env.api.Admin.GetFulfillment(env.ctx, admin.FulfillmentRefParams{
		WorkspaceID: testWorkspaceID,
		ID:          fulfillmentID,
	})
	if err != nil {
		t.Fatalf("get fulfillment: %v", err)
	}
	if fulfillment.Status != "succeeded" {
		t.Fatalf("fulfillment status = %q, want succeeded", fulfillment.Status)
	}
	fulfillmentItems, err := env.api.Admin.ListFulfillmentItems(env.ctx, admin.FulfillmentItemListParams{
		WorkspaceID:   testWorkspaceID,
		FulfillmentID: fulfillmentID,
	})
	if err != nil {
		t.Fatalf("list fulfillment items: %v", err)
	}
	if len(fulfillmentItems) != 1 || fulfillmentItems[0].Quantity != 1 {
		t.Fatalf("unexpected fulfillment items: %#v", fulfillmentItems)
	}

	key, err := env.api.Admin.CreateProductKey(env.ctx, admin.CreateProductKeyParams{
		WorkspaceID:    testWorkspaceID,
		AppID:          7301,
		PlatformID:     1,
		PlatformUserID: "admin-key-owner",
		ProductID:      order.ProductID,
		MaxUses:        2,
	})
	if err != nil {
		t.Fatalf("create purchase key: %v", err)
	}
	if key == "" {
		t.Fatal("expected non-empty purchase key")
	}
	keys, err := env.api.Admin.ListPurchaseKeys(env.ctx, admin.PurchaseKeyListParams{
		WorkspaceID:    testWorkspaceID,
		ProductID:      order.ProductID,
		PlatformUserID: "admin-key-owner",
	})
	if err != nil {
		t.Fatalf("list purchase keys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("purchase key count = %d, want 1", len(keys))
	}
	purchaseKey, err := env.api.Admin.GetPurchaseKey(env.ctx, testWorkspaceID, uint64(keys[0].ID))
	if err != nil {
		t.Fatalf("get purchase key: %v", err)
	}
	if purchaseKey.Status != "active" {
		t.Fatalf("purchase key status = %q, want active", purchaseKey.Status)
	}
	rows, err := env.api.Admin.UpdatePurchaseKeyStatus(
		env.ctx,
		testWorkspaceID,
		uint64(purchaseKey.ID),
		"canceled",
	)
	if err != nil || rows != 1 {
		t.Fatalf("cancel purchase key rows=%d err=%v", rows, err)
	}

	providerSubscriptionID := uniquePaymentID("admin-subscription")
	orderID := int64(fixture.OrderID)
	attemptID := int64(fixture.AttemptID)
	subscriptionID, err := env.api.Admin.UpsertSubscription(env.ctx, admin.SubscriptionUpsertParams{
		WorkspaceID:            testWorkspaceID,
		ProviderCode:           fixture.ProviderCode,
		ProviderSubscriptionID: providerSubscriptionID,
		AppID:                  7301,
		PlatformID:             1,
		PlatformUserID:         "admin-subscription-owner",
		ProductID:              order.ProductID,
		OrderID:                &orderID,
		AttemptID:              &attemptID,
		Status:                 "active",
		StartedAt:              time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("upsert subscription: %v", err)
	}
	subscriptions, err := env.api.Admin.ListSubscriptions(env.ctx, admin.SubscriptionListParams{
		WorkspaceID:  testWorkspaceID,
		ProviderCode: fixture.ProviderCode,
		ProductID:    order.ProductID,
	})
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	if len(subscriptions) != 1 {
		t.Fatalf("subscription count = %d, want 1", len(subscriptions))
	}
	subscriptionValue, err := env.api.Admin.GetSubscription(env.ctx, testWorkspaceID, subscriptionID)
	if err != nil {
		t.Fatalf("get subscription: %v", err)
	}
	if subscriptionValue.ProviderSubscriptionID != providerSubscriptionID {
		t.Fatalf("provider subscription id = %q", subscriptionValue.ProviderSubscriptionID)
	}
	subscriptionByProvider, err := env.api.Admin.GetSubscriptionByProviderID(
		env.ctx,
		admin.SubscriptionProviderRefParams{
			WorkspaceID:            testWorkspaceID,
			ProviderCode:           fixture.ProviderCode,
			ProviderSubscriptionID: providerSubscriptionID,
		},
	)
	if err != nil {
		t.Fatalf("get subscription by provider id: %v", err)
	}
	if subscriptionByProvider.ID != subscriptionValue.ID {
		t.Fatalf("subscription by provider = %d, want %d", subscriptionByProvider.ID, subscriptionValue.ID)
	}

	refundID, err := env.api.Admin.CreateRefund(env.ctx, admin.RefundCreateParams{
		WorkspaceID:  testWorkspaceID,
		OrderID:      fixture.OrderID,
		AttemptID:    fixture.AttemptID,
		ProviderCode: fixture.ProviderCode,
		AmountMinor:  1,
		AssetCode:    fixture.AssetCode,
		Status:       "created",
	})
	if err != nil {
		t.Fatalf("create refund: %v", err)
	}
	refunds, err := env.api.Admin.ListRefunds(env.ctx, admin.RefundListParams{
		WorkspaceID:  testWorkspaceID,
		OrderID:      fixture.OrderID,
		ProviderCode: fixture.ProviderCode,
	})
	if err != nil {
		t.Fatalf("list refunds: %v", err)
	}
	if len(refunds) != 1 {
		t.Fatalf("refund count = %d, want 1", len(refunds))
	}
	refundValue, err := env.api.Admin.GetRefund(env.ctx, admin.RefundRefParams{
		WorkspaceID: testWorkspaceID,
		ID:          refundID,
	})
	if err != nil {
		t.Fatalf("get refund: %v", err)
	}
	if refundValue.AmountMinor != 1 {
		t.Fatalf("refund amount = %d, want 1", refundValue.AmountMinor)
	}

	eventID, err := env.api.Operational.CreateEvent(env.ctx, checkout.CreateEventParams{
		WorkspaceID:  testWorkspaceID,
		ProviderCode: fixture.ProviderCode,
		AttemptID:    &attemptID,
		OrderID:      &orderID,
		EventType:    "admin_test",
		PayloadHash:  uniquePaymentID("event-hash"),
	})
	if err != nil {
		t.Fatalf("create payment event: %v", err)
	}
	event, err := env.api.Admin.GetPaymentEvent(env.ctx, admin.EventRefParams{
		WorkspaceID: testWorkspaceID,
		ID:          eventID,
	})
	if err != nil {
		t.Fatalf("get payment event: %v", err)
	}
	if event.ProcessingStatus != "new" {
		t.Fatalf("payment event status = %q, want new", event.ProcessingStatus)
	}
	if err := env.api.Admin.UpdatePaymentEventProcessingStatus(env.ctx, admin.EventStatusParams{
		WorkspaceID: testWorkspaceID,
		ID:          eventID,
		Status:      "processed",
	}); err != nil {
		t.Fatalf("update payment event status: %v", err)
	}
}

func TestPaymentAdminCallbackControls(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	createEvent := func(suffix string) uint64 {
		t.Helper()

		id, err := env.api.callbacks.CreateEvent(env.ctx, callbackutil.CreateParams{
			WorkspaceID:    testWorkspaceID,
			SourceService:  "payment",
			EventType:      "payment.test",
			EventKey:       uniquePaymentID("callback-" + suffix),
			IdempotencyKey: uniquePaymentID("callback-idempotency-" + suffix),
			Payload:        []byte(`{"ok":true}`),
		})
		if err != nil {
			t.Fatalf("create callback event %s: %v", suffix, err)
		}

		return id
	}

	retryID := createEvent("retry")
	okID := createEvent("ok")
	rejectID := createEvent("reject")
	expiredID := createEvent("expired")

	events, err := env.api.Admin.ListCallbackEvents(env.ctx, admin.CallbackEventListParams{
		WorkspaceID:   testWorkspaceID,
		SourceService: "payment",
		EventType:     "payment.test",
	})
	if err != nil {
		t.Fatalf("list callback events: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("callback event count = %d, want 4", len(events))
	}
	event, err := env.api.Admin.GetCallbackEvent(env.ctx, testWorkspaceID, retryID)
	if err != nil {
		t.Fatalf("get callback event: %v", err)
	}
	if event.ID != retryID {
		t.Fatalf("callback event id = %d, want %d", event.ID, retryID)
	}
	if rows, err := env.api.Admin.RetryCallbackEventNow(env.ctx, testWorkspaceID, retryID); err != nil || rows != 1 {
		t.Fatalf("retry callback rows=%d err=%v", rows, err)
	}
	if rows, err := env.api.Admin.MarkCallbackEventOK(env.ctx, testWorkspaceID, okID); err != nil || rows != 1 {
		t.Fatalf("mark callback ok rows=%d err=%v", rows, err)
	}
	if rows, err := env.api.Admin.MarkCallbackEventReject(env.ctx, testWorkspaceID, rejectID, "test rejection"); err != nil || rows != 1 {
		t.Fatalf("mark callback reject rows=%d err=%v", rows, err)
	}

	if _, err := env.db.ExecContext(env.ctx, `
		UPDATE payment_clb_event
		SET status = 'processing',
			locked_by = 'expired-worker',
			locked_until = now() - interval '1 minute'
		WHERE id = $1
	`, expiredID); err != nil {
		t.Fatalf("prepare expired callback lease: %v", err)
	}
	if rows, err := env.api.Admin.ResetExpiredCallbackProcessing(env.ctx, testWorkspaceID); err != nil || rows != 1 {
		t.Fatalf("reset expired callback rows=%d err=%v", rows, err)
	}
	if _, err := env.api.Admin.GetCallbackEvent(
		env.ctx,
		testsupport.WorkspaceID("payment-callback-other"),
		retryID,
	); err == nil {
		t.Fatal("cross-workspace callback read must fail")
	}
}

func TestPaymentAdminProviderTransactionSurface(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	sourceKey := uniquePaymentID("provider-source")
	externalTransactionID := uniquePaymentID("provider-transaction")

	rows, err := env.api.Admin.UpsertProviderCursor(env.ctx, admin.ProviderCursorUpsertParams{
		WorkspaceID:    testWorkspaceID,
		ProviderCode:   "ton",
		Network:        "mainnet",
		SourceKey:      sourceKey,
		CursorValue:    "100",
		CursorSequence: 100,
	})
	if err != nil || rows != 1 {
		t.Fatalf("upsert provider cursor rows=%d err=%v", rows, err)
	}
	cursor, err := env.api.Admin.GetProviderCursor(env.ctx, testWorkspaceID, "ton", "mainnet", sourceKey)
	if err != nil {
		t.Fatalf("get provider cursor: %v", err)
	}
	if cursor.CursorSequence != 100 {
		t.Fatalf("cursor sequence = %d, want 100", cursor.CursorSequence)
	}
	cursors, err := env.api.Admin.ListProviderCursors(env.ctx, admin.ProviderCursorListParams{
		WorkspaceID:  testWorkspaceID,
		ProviderCode: "ton",
		Network:      "mainnet",
	})
	if err != nil {
		t.Fatalf("list provider cursors: %v", err)
	}
	if len(cursors) != 1 {
		t.Fatalf("provider cursor count = %d, want 1", len(cursors))
	}

	repo := repository.NewPaymentRepository(env.client)
	t.Cleanup(func() { _ = repo.Close() })
	transactionID, err := repo.CreateProviderTransaction(env.ctx, paymentsqlc.CreateProviderTransactionParams{
		WorkspaceID:           testWorkspaceID,
		ProviderCode:          "ton",
		Network:               "mainnet",
		SourceKey:             sourceKey,
		AssetCode:             "TON",
		ExternalTransactionID: externalTransactionID,
		SequenceNumber:        101,
		SourceAddress:         "source-wallet",
		DestinationAddress:    "destination-wallet",
		AmountMinor:           1000,
		PaymentReference:      "payment-reference",
		Status:                paymentsqlc.PaymentProviderTransactionStatusNew,
		OccurredAt:            time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create provider transaction: %v", err)
	}
	transactions, err := env.api.Admin.ListProviderTransactions(env.ctx, admin.ProviderTransactionListParams{
		WorkspaceID:  testWorkspaceID,
		ProviderCode: "ton",
		Network:      "mainnet",
		SourceKey:    sourceKey,
	})
	if err != nil {
		t.Fatalf("list provider transactions: %v", err)
	}
	if len(transactions) != 1 {
		t.Fatalf("provider transaction count = %d, want 1", len(transactions))
	}
	transaction, err := env.api.Admin.GetProviderTransaction(env.ctx, testWorkspaceID, transactionID)
	if err != nil {
		t.Fatalf("get provider transaction: %v", err)
	}
	if transaction.ExternalTransactionID != externalTransactionID {
		t.Fatalf("external transaction id = %q", transaction.ExternalTransactionID)
	}
	transactionByExternalID, err := env.api.Admin.GetProviderTransactionByExternalID(
		env.ctx,
		testWorkspaceID,
		"ton",
		"mainnet",
		sourceKey,
		externalTransactionID,
	)
	if err != nil {
		t.Fatalf("get provider transaction by external id: %v", err)
	}
	if transactionByExternalID.ID != transaction.ID {
		t.Fatalf("provider transaction by external id = %d, want %d", transactionByExternalID.ID, transaction.ID)
	}
	rows, err = env.api.Admin.UpdateProviderTransactionStatus(
		env.ctx,
		testWorkspaceID,
		transactionID,
		"matched",
		"",
	)
	if err != nil || rows != 1 {
		t.Fatalf("update provider transaction rows=%d err=%v", rows, err)
	}
	if _, err := env.api.Admin.GetProviderTransaction(
		env.ctx,
		testsupport.WorkspaceID("payment-provider-transaction-other"),
		transactionID,
	); err == nil {
		t.Fatal("cross-workspace provider transaction read must fail")
	}
}

func TestPaymentAdminProductLimitCounterSurface(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		UserLimit:         1,
		UserInterval:      "DAY",
		UserIntervalCount: 1,
	})
	identity := paymentTestIdentity(testWorkspaceID, 7401, 1, "limit-counter-user")

	if _, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  identity,
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	}); err != nil {
		t.Fatalf("create limited order: %v", err)
	}

	counters, err := env.api.Admin.ListProductLimitCounters(env.ctx, admin.ProductLimitCounterListParams{
		WorkspaceID:    testWorkspaceID,
		ProductID:      productID,
		PlatformID:     identity.PlatformID,
		PlatformUserID: identity.PlatformUserID,
	})
	if err != nil {
		t.Fatalf("list product limit counters: %v", err)
	}
	if len(counters) != 1 {
		t.Fatalf("product limit counter count = %d, want 1", len(counters))
	}
	if counters[0].ReservedCount != 1 || counters[0].PaidCount != 0 {
		t.Fatalf("unexpected product limit counter: %#v", counters[0])
	}

	rows, err := env.api.Admin.DeleteProductLimitCounter(env.ctx, admin.ProductLimitCounterDeleteParams{
		WorkspaceID:    counters[0].WorkspaceID,
		PlatformID:     counters[0].PlatformID,
		ProductID:      counters[0].ProductID,
		CounterScope:   counters[0].CounterScope,
		PlatformUserID: counters[0].PlatformUserID,
		WindowStart:    counters[0].WindowStart,
		WindowEnd:      counters[0].WindowEnd,
	})
	if err != nil || rows != 1 {
		t.Fatalf("delete product limit counter rows=%d err=%v", rows, err)
	}
	counters, err = env.api.Admin.ListProductLimitCounters(env.ctx, admin.ProductLimitCounterListParams{
		WorkspaceID: testWorkspaceID,
		ProductID:   productID,
	})
	if err != nil {
		t.Fatalf("list product limit counters after delete: %v", err)
	}
	if len(counters) != 0 {
		t.Fatalf("product limit counters after delete = %#v", counters)
	}
}

func TestPaymentOperationalAndLegacyCatalogServices(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	providerCode := uniquePaymentID("lp")
	assetCode := strings.ToUpper(uniquePaymentID("la"))

	if err := env.api.Operational.UpsertProvider(env.ctx, operational.ProviderUpsertParams{
		Code:             providerCode,
		Title:            "Legacy provider",
		ProviderKind:     "fiat_gateway",
		SupportsCreate:   true,
		SupportsRedirect: true,
		SupportsWebhook:  true,
		SupportsRefund:   true,
		IsActive:         true,
	}); err != nil {
		t.Fatalf("operational upsert provider: %v", err)
	}
	provider, err := env.api.Admin.GetProvider(env.ctx, providerCode)
	if err != nil {
		t.Fatalf("get operational provider: %v", err)
	}
	if provider.Title != "Legacy provider" {
		t.Fatalf("provider title = %q", provider.Title)
	}

	if err := env.api.asset.Upsert(env.ctx, paymentasset.UpsertParams{
		Code:      assetCode,
		Title:     "Legacy asset",
		AssetKind: "fiat",
		Scale:     2,
		IsActive:  true,
	}); err != nil {
		t.Fatalf("legacy asset upsert: %v", err)
	}
	if err := env.api.asset.UpsertProvider(env.ctx, paymentasset.ProviderUpsertParams{
		ProviderCode: providerCode,
		AssetCode:    assetCode,
		IsActive:     true,
	}); err != nil {
		t.Fatalf("legacy provider asset upsert: %v", err)
	}
	providerAsset, err := env.api.asset.GetProvider(env.ctx, providerCode, assetCode)
	if err != nil {
		t.Fatalf("legacy get provider asset: %v", err)
	}
	if providerAsset.ProviderCode != providerCode || providerAsset.AssetCode != assetCode {
		t.Fatalf("unexpected legacy provider asset: %#v", providerAsset)
	}
	if rows, err := env.api.asset.DeleteProvider(env.ctx, providerCode, assetCode); err != nil || rows != 1 {
		t.Fatalf("legacy delete provider asset rows=%d err=%v", rows, err)
	}
	if rows, err := env.api.asset.Delete(env.ctx, assetCode); err != nil || rows != 1 {
		t.Fatalf("legacy delete asset rows=%d err=%v", rows, err)
	}
	if rows, err := env.api.Operational.DeleteProvider(env.ctx, providerCode); err != nil || rows != 1 {
		t.Fatalf("operational delete provider rows=%d err=%v", rows, err)
	}

	if _, err := env.api.Operational.UpdateAssetRate(env.ctx, operational.UpdateAssetRateParams{
		AssetCode:              "TON",
		ReferenceAssetCode:     repository.USDTAssetCode,
		ReferencePerAssetMinor: 5_000_000,
		Source:                 "test",
		ObservedAt:             time.Now().UTC(),
	}); err != nil {
		t.Fatalf("update USDT rate: %v", err)
	}
	assetRates, err := env.api.asset.ListUSDTPrices(env.ctx)
	if err != nil {
		t.Fatalf("legacy list USDT prices: %v", err)
	}
	if len(assetRates) != 1 || assetRates[0].AssetCode != "TON" {
		t.Fatalf("unexpected legacy USDT prices: %#v", assetRates)
	}
	userRates, err := env.api.User.ListUSDTPrices(env.ctx, user.ListUSDTPricesParams{})
	if err != nil {
		t.Fatalf("user list USDT prices: %v", err)
	}
	if len(userRates) != 1 || userRates[0].AssetCode != "TON" {
		t.Fatalf("unexpected user USDT prices: %#v", userRates)
	}
}

func TestPaymentLegacyProductDeleteSurface(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	now := time.Now().UTC()
	groupCode := uniquePaymentID("legacy-product-group")
	productID := uniquePaymentID("legacy-product")
	itemID := uniquePaymentID("legacy-product-item")
	localizationKey := productID + ".title"
	startsAt := now.Add(-time.Hour)
	endsAt := now.Add(time.Hour)

	if err := env.api.product.UpsertGroup(env.ctx, product.UpsertGroupParams{
		WorkspaceID: testWorkspaceID,
		Code:        groupCode,
		Position:    1,
		IsActive:    true,
	}); err != nil {
		t.Fatalf("legacy upsert group: %v", err)
	}
	if err := env.api.product.Create(env.ctx, product.UpsertParams{
		WorkspaceID:    testWorkspaceID,
		ID:             productID,
		GroupCode:      utils.Ref(groupCode),
		TitleKey:       localizationKey,
		QuantityMode:   "fixed",
		GlobalInterval: "UNLIMITED",
		UserInterval:   "UNLIMITED",
		AvailableFrom:  &startsAt,
		AvailableUntil: &endsAt,
		IsVisible:      true,
	}); err != nil {
		t.Fatalf("legacy create product: %v", err)
	}
	if err := env.api.product.UpsertLocalization(env.ctx, product.UpsertLocalizationParams{
		WorkspaceID:     testWorkspaceID,
		Locale:          "ru",
		LocalizationKey: localizationKey,
		Value:           "Legacy product",
	}); err != nil {
		t.Fatalf("legacy upsert localization: %v", err)
	}
	if err := env.api.product.AddItem(env.ctx, product.AddItemParams{
		WorkspaceID: testWorkspaceID,
		ProductID:   productID,
		ItemID:      itemID,
		Quantity:    1,
	}); err != nil {
		t.Fatalf("legacy add item: %v", err)
	}
	priceID, err := env.api.product.CreatePrice(env.ctx, product.CreatePriceParams{
		WorkspaceID:     testWorkspaceID,
		ProductID:       productID,
		AssetCode:       "RUB",
		ListAmountMinor: 100,
		StartsAt:        &startsAt,
		EndsAt:          &endsAt,
	})
	if err != nil {
		t.Fatalf("legacy create price: %v", err)
	}

	if rows, err := env.api.product.DeletePrice(env.ctx, testWorkspaceID, priceID); err != nil || rows != 1 {
		t.Fatalf("legacy delete price rows=%d err=%v", rows, err)
	}
	if rows, err := env.api.product.RemoveItem(env.ctx, testWorkspaceID, productID, itemID); err != nil || rows != 1 {
		t.Fatalf("legacy remove item rows=%d err=%v", rows, err)
	}
	if rows, err := env.api.product.DeleteLocalization(env.ctx, testWorkspaceID, "ru", localizationKey); err != nil || rows != 1 {
		t.Fatalf("legacy delete localization rows=%d err=%v", rows, err)
	}
	if rows, err := env.api.product.Delete(env.ctx, testWorkspaceID, productID); err != nil || rows != 1 {
		t.Fatalf("legacy delete product rows=%d err=%v", rows, err)
	}
	if rows, err := env.api.product.DeleteGroup(env.ctx, testWorkspaceID, groupCode); err != nil || rows != 1 {
		t.Fatalf("legacy delete group rows=%d err=%v", rows, err)
	}
}

type paymentTestEnv struct {
	ctx    context.Context
	db     *sql.DB
	client *sqlwrap.Client
	api    *Payment
}

type paymentTestStorage struct {
	values map[string][]byte
}

func (s *paymentTestStorage) GetWithTTL(key string) ([]byte, time.Duration, error) {
	return s.values[key], time.Minute, nil
}

func (s *paymentTestStorage) Set(key string, value []byte, _ time.Duration) error {
	s.values[key] = append([]byte(nil), value...)
	return nil
}

func (s *paymentTestStorage) Delete(key string) error {
	delete(s.values, key)
	return nil
}

func (s *paymentTestStorage) Reset() error {
	s.values = make(map[string][]byte)
	return nil
}

func (s *paymentTestStorage) Close() error {
	return nil
}

type paymentTestMutex struct {
	locked map[string]bool
}

func (m *paymentTestMutex) Lock(key string) error {
	m.locked[key] = true
	return nil
}

func (m *paymentTestMutex) Unlock(key string) error {
	delete(m.locked, key)
	return nil
}

type paymentTestCodec struct{}

func (paymentTestCodec) Marshal(value any) ([]byte, error) {
	return json.Marshal(value)
}

func (paymentTestCodec) Unmarshal(data []byte, value any) error {
	return json.Unmarshal(data, value)
}

const testWorkspaceID = "00000000-0000-0000-0000-000000000001"

func TestPaymentOptionsDelegateCacheMutexAndCodec(t *testing.T) {
	storage := &paymentTestStorage{values: make(map[string][]byte)}
	mutex := &paymentTestMutex{locked: make(map[string]bool)}
	options := toSQLWrapOptions(Options{
		Cache:        storage,
		CacheEnabled: true,
		Codec:        paymentTestCodec{},
		Mutex:        mutex,
	})

	if err := options.Cache.Set("key", []byte("value"), time.Minute); err != nil {
		t.Fatalf("cache set: %v", err)
	}
	value, ttl, err := options.Cache.GetWithTTL("key")
	if err != nil {
		t.Fatalf("cache get: %v", err)
	}
	if string(value) != "value" || ttl != time.Minute {
		t.Fatalf("cache result value=%q ttl=%s", value, ttl)
	}
	if err := options.Cache.Delete("key"); err != nil {
		t.Fatalf("cache delete: %v", err)
	}
	if err := options.Cache.Set("key", []byte("value"), time.Minute); err != nil {
		t.Fatalf("cache set before reset: %v", err)
	}
	if err := options.Cache.Reset(); err != nil {
		t.Fatalf("cache reset: %v", err)
	}
	if err := options.Cache.Close(); err != nil {
		t.Fatalf("cache close: %v", err)
	}

	if err := options.Mutex.Lock("key"); err != nil {
		t.Fatalf("mutex lock: %v", err)
	}
	if !mutex.locked["key"] {
		t.Fatal("custom mutex did not receive lock")
	}
	if err := options.Mutex.Unlock("key"); err != nil {
		t.Fatalf("mutex unlock: %v", err)
	}
	if mutex.locked["key"] {
		t.Fatal("custom mutex did not receive unlock")
	}

	encoded, err := options.Codec.Marshal(map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("codec marshal: %v", err)
	}
	var decoded map[string]string
	if err := options.Codec.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("codec unmarshal: %v", err)
	}
	if decoded["key"] != "value" {
		t.Fatalf("decoded value = %q", decoded["key"])
	}
}

func TestPaymentOptionAdaptersAreNilSafe(t *testing.T) {
	storage := wrapStorage{}
	if value, ttl, err := storage.GetWithTTL("key"); err != nil || value != nil || ttl != 0 {
		t.Fatalf("nil storage get value=%v ttl=%s err=%v", value, ttl, err)
	}
	if err := storage.Set("key", []byte("value"), time.Minute); err != nil {
		t.Fatalf("nil storage set: %v", err)
	}
	if err := storage.Delete("key"); err != nil {
		t.Fatalf("nil storage delete: %v", err)
	}
	if err := storage.Reset(); err != nil {
		t.Fatalf("nil storage reset: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("nil storage close: %v", err)
	}

	mutex := wrapMutex{}
	if err := mutex.Lock("key"); err != nil {
		t.Fatalf("nil mutex lock: %v", err)
	}
	if err := mutex.Unlock("key"); err != nil {
		t.Fatalf("nil mutex unlock: %v", err)
	}

	codec := wrapCodec{}
	if value, err := codec.Marshal(struct{}{}); err != nil || value != nil {
		t.Fatalf("nil codec marshal value=%v err=%v", value, err)
	}
	if err := codec.Unmarshal(nil, &struct{}{}); err != nil {
		t.Fatalf("nil codec unmarshal: %v", err)
	}
}

func TestPaymentRunBlocksUntilContextCanceled(t *testing.T) {
	setupPaymentIntegrationTest(t)

	service := New(DatabaseParams{
		User:     mysqlControlUsername,
		Password: mysqlControlPassword,
		Database: paymentTestDB,
		Host:     mysqlControlHost,
		Port:     paymentPostgresPort,
	})
	if err := service.OnCallback(context.Background(), func(ctx Context) error {
		return ctx.Successful()
	},
		WithCallbackWorkerID("payment-test-worker"),
		WithCallbackBatchSize(10),
		WithCallbackLeaseTimeout(time.Second),
		WithCallbackIdleDelay(10*time.Millisecond),
	); err != nil {
		t.Fatalf("register callback: %v", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.Run(runCtx)
	}()

	select {
	case err := <-done:
		t.Fatalf("Run returned before cancellation: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := service.OnCallback(context.Background(), func(Context) error {
		return nil
	}); err == nil {
		t.Fatal("callback registration after Run must fail")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}

func TestPaymentCatalogCheckoutAndGiftCycle(t *testing.T) {
	// Test database setup.
	// Create the MySQL database connection and bootstrap the payment schema.
	// Verify every following block runs through the same initialized payment API.
	env := setupPaymentIntegrationTest(t)
	productID := fmt.Sprintf("test_product_%d", time.Now().UnixNano())
	groupCode := fmt.Sprintf("test_group_%d", time.Now().UnixNano())
	itemID := fmt.Sprintf("test_item_%d", time.Now().UnixNano())
	productTitleKey := productID + ".title"
	productDescriptionKey := productID + ".description"
	itemTitleKey := itemID + ".title"
	itemDescriptionKey := itemID + ".description"
	now := time.Now()
	availableFrom := now.Add(-time.Hour)
	availableUntil := now.Add(time.Hour)
	priceStartsAt := now.Add(-time.Hour)
	priceEndsAt := now.Add(time.Hour)
	periodSeconds := int64(86400)

	// Product creation.
	// Create a product group and product through the public Product CRUD API.
	// Verify checkout tests do not depend on direct SQL catalog seeding.
	if err := env.api.Admin.SaveProductGroup(env.ctx, product.UpsertGroupParams{
		WorkspaceID:    testWorkspaceID,
		Code:           groupCode,
		TitleKey:       utils.Ref(groupCode + ".title"),
		DescriptionKey: utils.Ref(groupCode + ".description"),
		Position:       1,
		IsActive:       true,
	}); err != nil {
		t.Fatalf("upsert product group: %v", err)
	}

	if err := env.api.Admin.SaveProduct(env.ctx, product.UpsertParams{
		WorkspaceID:    testWorkspaceID,
		ID:             productID,
		GroupCode:      utils.Ref(groupCode),
		TitleKey:       productTitleKey,
		DescriptionKey: utils.Ref(productDescriptionKey),
		ImageURL:       utils.Ref("https://example.com/product.png"),
		PeriodSeconds:  &periodSeconds,
		Position:       1,
		GlobalInterval: "UNLIMITED",
		UserInterval:   "UNLIMITED",
		AvailableFrom:  &availableFrom,
		AvailableUntil: &availableUntil,
		IsVisible:      true,
	}); err != nil {
		t.Fatalf("create product: %v", err)
	}
	// Localization creation.
	// Add product and item translations for the locales used by checkout.
	// Verify product previews resolve localized titles and descriptions.
	localizations := []product.UpsertLocalizationParams{
		{WorkspaceID: testWorkspaceID, Locale: "ru", LocalizationKey: productTitleKey, Value: "Тестовый товар"},
		{WorkspaceID: testWorkspaceID, Locale: "ru", LocalizationKey: productDescriptionKey, Value: "Описание тестового товара"},
		{WorkspaceID: testWorkspaceID, Locale: "ru", LocalizationKey: itemTitleKey, Value: "Премиум"},
		{WorkspaceID: testWorkspaceID, Locale: "ru", LocalizationKey: itemDescriptionKey, Value: "Премиум описание"},
		{WorkspaceID: testWorkspaceID, Locale: "en", LocalizationKey: productTitleKey, Value: "Test product"},
	}
	for _, localization := range localizations {
		if err := env.api.Admin.SaveLocalization(env.ctx, localization); err != nil {
			t.Fatalf("upsert localization %s/%s: %v", localization.Locale, localization.LocalizationKey, err)
		}
	}
	// Item and price setup.
	// Attach an item reward to the product and create the payable RUB price.
	// Verify fulfillment and price calculation use catalog data from the API.
	if err := env.api.Admin.AttachProductItem(env.ctx, product.AddItemParams{
		WorkspaceID: testWorkspaceID,
		ProductID:   productID,
		ItemID:      itemID,
		Quantity:    0,
	}); err == nil {
		t.Fatal("expected zero product item quantity to fail")
	}

	if err := env.api.Admin.AttachProductItem(env.ctx, product.AddItemParams{
		WorkspaceID: testWorkspaceID,
		ProductID:   productID,
		ItemID:      itemID,
		Quantity:    2,
	}); err != nil {
		t.Fatalf("add item to product: %v", err)
	}

	priceID, err := env.api.Admin.CreateCatalogPrice(env.ctx, product.CreatePriceParams{
		WorkspaceID:         testWorkspaceID,
		ProductID:           productID,
		AssetCode:           "RUB",
		ListAmountMinor:     1000,
		DiscountAmountMinor: 100,
		StartsAt:            &priceStartsAt,
		EndsAt:              &priceEndsAt,
	})
	if err != nil {
		t.Fatalf("create product price: %v", err)
	}
	if priceID == 0 {
		t.Fatal("expected created price id")
	}

	updatedRows, err := env.api.Admin.UpdateCatalogPrice(env.ctx, product.UpdatePriceParams{
		ID:                  priceID,
		WorkspaceID:         testWorkspaceID,
		AssetCode:           "RUB",
		ListAmountMinor:     1100,
		DiscountAmountMinor: 200,
		StartsAt:            &priceStartsAt,
		EndsAt:              &priceEndsAt,
	})
	if err != nil {
		t.Fatalf("update product price: %v", err)
	}
	if updatedRows != 1 {
		t.Fatalf("unexpected updated price rows: %d", updatedRows)
	}
	// Regular payment flow.
	// Create an order, payment attempt, provider event, and complete fulfillment.
	// Verify a normal purchase becomes fulfilled and completion is idempotent.
	item, err := env.api.User.GetProduct(env.ctx, product.GetParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 1001, 1, "buyer-regular"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("get product: %v", err)
	}
	if item.Price.PayableAmountMinor != 900 {
		t.Fatalf("unexpected payable amount: %d", item.Price.PayableAmountMinor)
	}
	if len(item.Items) != 1 || item.Items[0].Quantity != 2 {
		t.Fatalf("unexpected product items: %#v", item.Items)
	}

	products, err := env.api.User.ListProducts(env.ctx, user.ListProductsParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 1001, 1, "buyer-regular"),
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("list user products: %v", err)
	}
	var listedProduct *user.ProductModel
	for index := range products {
		if products[index].ID == productID {
			listedProduct = &products[index]
			break
		}
	}
	if listedProduct == nil {
		t.Fatalf("created product %q is missing from user catalog", productID)
	}
	if listedProduct.Title != "Тестовый товар" || listedProduct.Price.PayableAmountMinor != 900 {
		t.Fatalf("unexpected listed product: %#v", listedProduct)
	}
	if len(listedProduct.Items) != 1 || listedProduct.Items[0].Quantity != 2 {
		t.Fatalf("unexpected listed product items: %#v", listedProduct.Items)
	}
	groupProducts, err := env.api.User.ListProducts(env.ctx, user.ListProductsParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 1001, 1, "buyer-regular"),
		GroupCode: groupCode,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("list user products by group: %v", err)
	}
	if len(groupProducts) != 1 || groupProducts[0].ID != productID {
		t.Fatalf("unexpected grouped products: %#v", groupProducts)
	}
	missingGroupProducts, err := env.api.User.ListProducts(env.ctx, user.ListProductsParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 1001, 1, "buyer-regular"),
		GroupCode: "missing_group",
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("list user products by missing group: %v", err)
	}
	if len(missingGroupProducts) != 0 {
		t.Fatalf("missing group products = %#v", missingGroupProducts)
	}

	internalUserID := int64(501)
	order, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:       paymentTestIdentity(testWorkspaceID, 1001, 1, "buyer-regular"),
		InternalUserID: &internalUserID,
		ProductID:      productID,
		AssetCode:      "RUB",
		Locale:         "ru",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if order.Status != "draft" {
		t.Fatalf("unexpected order status: %s", order.Status)
	}

	providerPaymentID := uniquePaymentID("regular")
	attempt, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          paymentAttemptIdentity(order),
		OrderID:           order.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &providerPaymentID,
	})
	if err != nil {
		t.Fatalf("create attempt: %v", err)
	}
	if attempt.AmountMinor != order.PayableAmountMinor {
		t.Fatalf("attempt amount mismatch: got %d want %d", attempt.AmountMinor, order.PayableAmountMinor)
	}

	eventID := fmt.Sprintf("evt_%s", providerPaymentID)
	if _, err := env.api.Operational.CreateEvent(env.ctx, checkout.CreateEventParams{
		WorkspaceID:       testWorkspaceID,
		ProviderCode:      "yookassa",
		AttemptID:         utils.Ref(int64(attempt.ID)),
		OrderID:           utils.Ref(int64(order.ID)),
		ProviderEventID:   &eventID,
		ProviderPaymentID: &providerPaymentID,
		EventType:         "succeeded",
		EventStatus:       utils.Ref("succeeded"),
		PayloadHash:       sha256Hex(providerPaymentID),
		SignatureValid:    utils.Ref(true),
	}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	completed, err := env.api.Operational.CompleteAttempt(env.ctx, checkout.CompleteAttemptParams{
		WorkspaceID:       testWorkspaceID,
		AttemptID:         attempt.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &providerPaymentID,
		AmountMinor:       attempt.AmountMinor,
		AssetCode:         "RUB",
	})
	if err != nil {
		t.Fatalf("complete attempt: %v", err)
	}
	if completed.FulfillmentID == nil {
		t.Fatal("expected fulfillment id")
	}
	if rows, err := env.api.Admin.UpdateFulfillmentStatus(env.ctx, admin.FulfillmentStatusParams{
		WorkspaceID: testWorkspaceID,
		ID:          *completed.FulfillmentID,
		Status:      "succeeded",
	}); err != nil || rows != 1 {
		t.Fatalf("update fulfillment status: rows=%d err=%v", rows, err)
	}

	assertOrderStatus(t, env.ctx, env.db, order.ID, "fulfilled")
	assertAttemptStatus(t, env.ctx, env.db, attempt.ID, "succeeded")
	assertFulfillmentItemCount(t, env.ctx, env.db, *completed.FulfillmentID, 2)
	assertCallbackEvent(t, env.ctx, env.db, CallbackEventPaymentOrderFulfilled, order.ID, 1)
	assertOnCallbackSuccessful(t, env, order, attempt, *completed.FulfillmentID, providerPaymentID, itemID)
	assertAdminPaymentReadMethods(t, env, productID, order.ID, attempt.ID)
	assertPaymentPurchaseStats(t, env, productID, 1, 1, 1, 900)

	again, err := env.api.Operational.CompleteAttempt(env.ctx, checkout.CompleteAttemptParams{
		WorkspaceID:       testWorkspaceID,
		AttemptID:         attempt.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &providerPaymentID,
		AmountMinor:       attempt.AmountMinor,
		AssetCode:         "RUB",
	})
	if err != nil {
		t.Fatalf("complete attempt again: %v", err)
	}
	if !again.AlreadyDone {
		t.Fatal("expected second completion to be idempotent")
	}
	if again.FulfillmentID == nil || *again.FulfillmentID != *completed.FulfillmentID {
		t.Fatalf(
			"expected idempotent completion to return fulfillment %d, got %v",
			*completed.FulfillmentID,
			again.FulfillmentID,
		)
	}
	assertCallbackEvent(t, env.ctx, env.db, CallbackEventPaymentOrderFulfilled, order.ID, 1)
	assertCallbackStatus(t, env.ctx, env.db, CallbackEventPaymentOrderFulfilled, order.ID, "ok")
	assertPaymentPurchaseStats(t, env, productID, 1, 1, 1, 900)
	// Gift payment flow.
	// Create a hidden recipient key and let another user pay for that product.
	// Verify recipient privacy, payer tracking, key usage, and gift fulfillment.
	recipientInternalID := int64(701)
	key, err := env.api.Admin.CreateProductKey(env.ctx, product.CreateKeyParams{
		WorkspaceID:    testWorkspaceID,
		AppID:          2002,
		PlatformID:     1,
		PlatformUserID: "recipient-hidden",
		InternalUserID: &recipientInternalID,
		ProductID:      productID,
		MaxUses:        1,
	})
	if err != nil {
		t.Fatalf("create purchase key: %v", err)
	}

	giftProduct, err := env.api.User.GetProductByKey(env.ctx, product.GetByKeyParams{
		Key:       key,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("get product by key: %v", err)
	}
	if giftProduct.ID != productID {
		t.Fatalf("unexpected keyed product: %s", giftProduct.ID)
	}
	if giftProduct.Price.AssetCode != "RUB" {
		t.Fatalf("unexpected keyed product asset: %s", giftProduct.Price.AssetCode)
	}

	payerPlatformID := int64(1)
	payerPlatformUserID := "payer-visible"
	payerInternalID := int64(702)
	giftOrder, err := env.api.User.CreateOrderByKey(env.ctx, checkout.CreateOrderByKeyParams{
		Key: key,
		Payer: &user.Actor{
			PlatformID:     payerPlatformID,
			PlatformUserID: payerPlatformUserID,
			InternalUserID: &payerInternalID,
		},
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create order by key: %v", err)
	}
	if giftOrder.PlatformUserID != "recipient-hidden" {
		t.Fatalf("expected hidden recipient on order, got %s", giftOrder.PlatformUserID)
	}
	if giftOrder.PayerPlatformUserID == nil || *giftOrder.PayerPlatformUserID != payerPlatformUserID {
		t.Fatalf("expected payer on order, got %#v", giftOrder.PayerPlatformUserID)
	}

	giftProviderPaymentID := uniquePaymentID("gift")
	giftAttempt, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          paymentAttemptIdentity(giftOrder),
		OrderID:           giftOrder.ID,
		ProviderCode:      "platega",
		ProviderPaymentID: &giftProviderPaymentID,
	})
	if err != nil {
		t.Fatalf("create gift attempt: %v", err)
	}

	giftCompleted, err := env.api.Operational.CompleteAttempt(env.ctx, checkout.CompleteAttemptParams{
		WorkspaceID:       testWorkspaceID,
		AttemptID:         giftAttempt.ID,
		ProviderCode:      "platega",
		ProviderPaymentID: &giftProviderPaymentID,
		AmountMinor:       giftAttempt.AmountMinor,
		AssetCode:         "RUB",
	})
	if err != nil {
		t.Fatalf("complete gift attempt: %v", err)
	}
	if giftCompleted.FulfillmentID == nil {
		t.Fatal("expected gift fulfillment id")
	}

	assertOrderStatus(t, env.ctx, env.db, giftOrder.ID, "fulfilled")
	assertPurchaseKeyUsed(t, env.ctx, env.db, key)
	assertFulfillmentItemCount(t, env.ctx, env.db, *giftCompleted.FulfillmentID, 2)
	assertPaymentPurchaseStats(t, env, productID, 2, 2, 2, 1800)

	for index, status := range []string{"canceled", "expired", "failed"} {
		statusOrder, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
			Identity: paymentTestIdentity(
				testWorkspaceID,
				int64(1100+index),
				1,
				fmt.Sprintf("status-%s", status),
			),
			ProductID: productID,
			AssetCode: "RUB",
			Locale:    "ru",
		})
		if err != nil {
			t.Fatalf("create order for %s stats: %v", status, err)
		}
		updated, err := env.api.Admin.UpdateOrderStatus(
			env.ctx,
			testWorkspaceID,
			statusOrder.ID,
			status,
		)
		if err != nil {
			t.Fatalf("update draft order to %s for daily stats: %v", status, err)
		}
		if updated != 1 {
			t.Fatalf("expected one order updated to %s, got %d", status, updated)
		}
	}
	overview, err := env.api.Admin.ListDailyOverview(
		env.ctx, testWorkspaceID, now.Add(-24*time.Hour), now.Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf("list complete payment daily overview: %v", err)
	}
	if len(overview) != 1 {
		t.Fatalf("unexpected complete payment daily overview: %#v", overview)
	}
	today := overview[0]
	if today.OrdersCreated != 5 ||
		today.DraftOrders != 5 ||
		today.PendingPaymentOrders != 2 ||
		today.PaidOrders != 2 ||
		today.FulfilledOrders != 2 ||
		today.CanceledOrders != 1 ||
		today.ExpiredOrders != 1 ||
		today.RefundedOrders != 0 ||
		today.ChargebackedOrders != 0 ||
		today.FailedOrders != 1 {
		t.Fatalf("daily overview does not contain every order status: %#v", today)
	}
}

func TestPaymentImportExportCycle(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	sourceWorkspace := "00000000-0000-0000-0000-000000000101"
	targetWorkspace := "00000000-0000-0000-0000-000000000102"
	groupCode := "export_group_" + suffix
	productID := "export_product_" + suffix
	itemID := "export_item_" + suffix
	productTitleKey := productID + ".title"
	productDescriptionKey := productID + ".description"
	itemTitleKey := itemID + ".title"
	itemDescriptionKey := itemID + ".description"
	now := time.Now()
	availableFrom := now.Add(-time.Hour)
	availableUntil := now.Add(time.Hour)
	priceStartsAt := now.Add(-time.Hour)
	priceEndsAt := now.Add(time.Hour)
	walletAddress := "UQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAJKZ"
	walletConfigURL := "https://example.com/payment-ton.config.json"
	expectedWalletAddress, err := paymentton.NormalizeWalletAddress(walletAddress, paymentton.NetworkMainnet)
	if err != nil {
		t.Fatalf("normalize wallet: %v", err)
	}

	if err := env.api.Admin.SaveProductGroup(env.ctx, product.UpsertGroupParams{
		WorkspaceID: sourceWorkspace, Code: groupCode, TitleKey: utils.Ref(groupCode + ".title"),
		DescriptionKey: utils.Ref(groupCode + ".description"), Position: 1, IsActive: true,
	}); err != nil {
		t.Fatalf("upsert product group: %v", err)
	}
	if err := env.api.Admin.SaveProduct(env.ctx, product.UpsertParams{
		WorkspaceID: sourceWorkspace, ID: productID, GroupCode: utils.Ref(groupCode),
		TitleKey: productTitleKey, DescriptionKey: utils.Ref(productDescriptionKey),
		QuantityMode: "fixed", Position: 1, GlobalInterval: "UNLIMITED", UserInterval: "UNLIMITED",
		AvailableFrom: &availableFrom, AvailableUntil: &availableUntil, IsVisible: true,
	}); err != nil {
		t.Fatalf("upsert product: %v", err)
	}
	for _, localization := range []product.UpsertLocalizationParams{
		{WorkspaceID: sourceWorkspace, Locale: "ru", LocalizationKey: groupCode + ".title", Value: "Группа"},
		{WorkspaceID: sourceWorkspace, Locale: "ru", LocalizationKey: groupCode + ".description", Value: "Описание группы"},
		{WorkspaceID: sourceWorkspace, Locale: "ru", LocalizationKey: productTitleKey, Value: "Товар"},
		{WorkspaceID: sourceWorkspace, Locale: "ru", LocalizationKey: productDescriptionKey, Value: "Описание товара"},
		{WorkspaceID: sourceWorkspace, Locale: "ru", LocalizationKey: itemTitleKey, Value: "Награда"},
		{WorkspaceID: sourceWorkspace, Locale: "ru", LocalizationKey: itemDescriptionKey, Value: "Описание награды"},
	} {
		if err := env.api.Admin.SaveLocalization(env.ctx, localization); err != nil {
			t.Fatalf("upsert localization: %v", err)
		}
	}
	if err := env.api.Admin.AttachProductItem(env.ctx, product.AddItemParams{
		WorkspaceID: sourceWorkspace, ProductID: productID, ItemID: itemID, Quantity: 25, Scale: 2,
	}); err != nil {
		t.Fatalf("attach product item: %v", err)
	}
	if _, err := env.api.Admin.CreateCatalogPrice(env.ctx, product.CreatePriceParams{
		WorkspaceID: sourceWorkspace, ProductID: productID, AssetCode: "RUB",
		ListAmountMinor: 1000, DiscountAmountMinor: 100, StartsAt: &priceStartsAt, EndsAt: &priceEndsAt,
	}); err != nil {
		t.Fatalf("create price: %v", err)
	}
	if err := env.api.Admin.SaveTONWallet(env.ctx, admin.TONWalletUpsertParams{
		WorkspaceID:      sourceWorkspace,
		Network:          paymentton.NetworkMainnet,
		WalletAddress:    walletAddress,
		NetworkConfigURL: &walletConfigURL,
		IsEnabled:        true,
	}); err != nil {
		t.Fatalf("save ton wallet: %v", err)
	}

	pkg, err := env.api.Admin.Export(env.ctx, sourceWorkspace, admin.ExportRequest{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(pkg.TONWallets) != 1 || pkg.TONWallets[0].WalletAddress != expectedWalletAddress {
		t.Fatalf("unexpected exported ton wallets: %+v", pkg.TONWallets)
	}
	if _, err := env.api.Admin.Import(env.ctx, targetWorkspace, admin.ImportRequest{
		Package: pkg, ConflictStrategy: repository.ImportConflictUpdate,
	}); err != nil {
		t.Fatalf("import: %v", err)
	}
	imported, err := env.api.Admin.Export(env.ctx, targetWorkspace, admin.ExportRequest{})
	if err != nil {
		t.Fatalf("export imported: %v", err)
	}
	if len(imported.Groups) != 1 || len(imported.Groups[0].Products) != 1 ||
		len(imported.Groups[0].Products[0].Items) != 1 || len(imported.Groups[0].Products[0].Prices) != 1 ||
		imported.Groups[0].Products[0].Items[0].Scale != 2 || len(imported.TONWallets) != 1 {
		t.Fatalf("unexpected imported package: %+v", imported)
	}
	importedWallet, err := env.api.Admin.GetTONWallet(env.ctx, targetWorkspace)
	if err != nil {
		t.Fatalf("get imported ton wallet: %v", err)
	}
	if importedWallet.Network != paymentton.NetworkMainnet || importedWallet.WalletAddress != expectedWalletAddress ||
		!importedWallet.NetworkConfigUrl.Valid || importedWallet.NetworkConfigUrl.String != walletConfigURL || !importedWallet.IsEnabled {
		t.Fatalf("unexpected imported ton wallet: %+v", importedWallet)
	}

	pkg.Groups[0].Localization = nil
	pkg.Groups[0].Products[0].Localization = nil
	pkg.Groups[0].Products[0].Items = nil
	pkg.Groups[0].Products[0].Prices = nil
	if _, err := env.api.Admin.Import(env.ctx, targetWorkspace, admin.ImportRequest{
		Package:          pkg,
		ConflictStrategy: repository.ImportConflictUpdate,
	}); err != nil {
		t.Fatalf("replace imported payment catalog: %v", err)
	}
	replaced, err := env.api.Admin.Export(env.ctx, targetWorkspace, admin.ExportRequest{})
	if err != nil {
		t.Fatalf("export replaced payment catalog: %v", err)
	}
	if len(replaced.Groups) != 1 ||
		len(replaced.Groups[0].Localization) != 0 ||
		len(replaced.Groups[0].Products) != 1 ||
		len(replaced.Groups[0].Products[0].Localization) != 0 ||
		len(replaced.Groups[0].Products[0].Items) != 0 ||
		len(replaced.Groups[0].Products[0].Prices) != 0 {
		t.Fatalf("update_existing kept removed payment children: %+v", replaced.Groups)
	}
}

func setupPaymentIntegrationTest(t testing.TB) paymentTestEnv {
	return setupPaymentIntegrationTestWithOptions(t, paymentTestOptions())
}

func setupPaymentIntegrationTestWithOptions(t testing.TB, options Options) paymentTestEnv {
	t.Helper()

	dsn := paymentTestDSN(t)
	dbName := paymentTestDB
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	adminDB, err := openMySQL(dsn, "")
	if err != nil {
		t.Fatalf("open admin mysql connection: %v", err)
	}
	t.Cleanup(func() { adminDB.Close() })

	if err := recreatePaymentTestDatabase(ctx, adminDB, dbName); err != nil {
		t.Fatalf("recreate database %s: %v", dbName, err)
	}

	appDB, err := openMySQL(dsn, dbName)
	if err != nil {
		t.Fatalf("open payment mysql connection: %v", err)
	}
	t.Cleanup(func() { appDB.Close() })

	client, err := sqlwrap.New(appDB, paymentTestSQLOptions())
	if err != nil {
		t.Fatalf("create sql client: %v", err)
	}

	repo := repository.NewPaymentRepository(client)
	if err := repo.Bootstrap(ctx, filepath.Join("sqlc", "schema.sql")); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	api, err := NewWithDatabase(ctx, appDB, options)
	if err != nil {
		t.Fatalf("create payment service: %v", err)
	}
	t.Cleanup(func() { _ = api.Close() })

	return paymentTestEnv{
		ctx:    ctx,
		db:     appDB,
		client: client,
		api:    api,
	}
}

func setupExistingPaymentIntegrationTest(t testing.TB) paymentTestEnv {
	t.Helper()

	dsn := paymentTestDSN(t)
	dbName := paymentTestDB
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	appDB, err := openMySQL(dsn, dbName)
	if err != nil {
		t.Fatalf("open existing payment mysql connection: %v", err)
	}
	t.Cleanup(func() { appDB.Close() })

	client, err := sqlwrap.New(appDB, paymentTestSQLOptions())
	if err != nil {
		t.Fatalf("create sql client: %v", err)
	}
	api, err := NewWithDatabase(ctx, appDB, paymentTestOptions())
	if err != nil {
		t.Fatalf("create payment service: %v", err)
	}
	t.Cleanup(func() { _ = api.Close() })

	return paymentTestEnv{
		ctx:    ctx,
		db:     appDB,
		client: client,
		api:    api,
	}
}

func paymentTestSQLOptions() sqlwrap.Options {
	return sqlwrap.Options{
		CacheEnabled:  true,
		CacheSize:     100_000,
		CacheTTLCheck: time.Minute,
	}
}

func paymentTestOptions() Options {
	return Options{
		CacheEnabled:        true,
		CacheSize:           100_000,
		CacheTTLCheck:       time.Minute,
		CacheL1Delay:        time.Minute,
		DisablePriceUpdater: true,
	}
}

func assertOrderStatus(t *testing.T, ctx context.Context, db *sql.DB, orderID uint64, want string) {
	t.Helper()
	var got string
	if err := db.QueryRowContext(ctx, "SELECT status FROM payment_order WHERE id = $1", orderID).Scan(&got); err != nil {
		t.Fatalf("select order status: %v", err)
	}
	if got != want {
		t.Fatalf("unexpected order status: got %s want %s", got, want)
	}
}

func assertAttemptStatus(t *testing.T, ctx context.Context, db *sql.DB, attemptID uint64, want string) {
	t.Helper()
	var got string
	if err := db.QueryRowContext(ctx, "SELECT status FROM payment_attempt WHERE id = $1", attemptID).Scan(&got); err != nil {
		t.Fatalf("select attempt status: %v", err)
	}
	if got != want {
		t.Fatalf("unexpected attempt status: got %s want %s", got, want)
	}
}

func assertFulfillmentItemCount(t *testing.T, ctx context.Context, db *sql.DB, fulfillmentID uint64, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(SUM(quantity), 0) FROM payment_fulfillment_item WHERE fulfillment_id = $1", fulfillmentID).Scan(&got); err != nil {
		t.Fatalf("select fulfillment item count: %v", err)
	}
	if got != want {
		t.Fatalf("unexpected fulfillment item count: got %d want %d", got, want)
	}
}

func assertCallbackEvent(t *testing.T, ctx context.Context, db *sql.DB, eventType string, orderID uint64, want int) {
	t.Helper()
	var got int
	var idempotencyKey string
	eventKey := fmt.Sprintf("%s:%d", eventType, orderID)
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(MAX(idempotency_key), '')
FROM payment_clb_event
WHERE source_service = 'payment' AND event_type = $1 AND event_key = $2`,
		eventType, eventKey,
	).Scan(&got, &idempotencyKey); err != nil {
		t.Fatalf("select callback event: %v", err)
	}
	if got != want {
		t.Fatalf("unexpected callback event count: got %d want %d", got, want)
	}
	if want > 0 && idempotencyKey != eventKey {
		t.Fatalf("unexpected callback idempotency key: got %s want %s", idempotencyKey, eventKey)
	}
}

func assertOnCallbackSuccessful(
	t *testing.T,
	env paymentTestEnv,
	order *checkout.Order,
	attempt *checkout.Attempt,
	fulfillmentID uint64,
	providerPaymentID string,
	itemID string,
) {
	t.Helper()
	ctx, cancel := context.WithCancel(env.ctx)
	handled := 0
	err := env.api.OnCallback(ctx, func(callback Context) error {
		handled++
		if callback.EventType != CallbackEventPaymentOrderFulfilled {
			t.Fatalf("unexpected callback event type: got %s want %s", callback.EventType, CallbackEventPaymentOrderFulfilled)
		}
		if callback.EventKey != fmt.Sprintf("%s:%d", CallbackEventPaymentOrderFulfilled, order.ID) {
			t.Fatalf("unexpected callback event key: %s", callback.EventKey)
		}
		if callback.IdempotencyKey != callback.EventKey {
			t.Fatalf("unexpected callback idempotency key: got %s want %s", callback.IdempotencyKey, callback.EventKey)
		}
		if callback.PaymentFulfilled == nil {
			t.Fatal("expected payment fulfilled callback payload")
		}
		payload := *callback.PaymentFulfilled
		if payload.OrderID != order.ID ||
			payload.AttemptID != attempt.ID ||
			payload.FulfillmentID != fulfillmentID ||
			payload.WorkspaceID != order.WorkspaceID ||
			payload.AppID != order.AppID ||
			payload.PlatformID != order.PlatformID ||
			payload.PlatformUserID != order.PlatformUserID ||
			payload.ProductID != order.ProductID ||
			payload.Quantity != order.Quantity ||
			payload.ProviderCode != attempt.ProviderCode ||
			payload.ProviderPaymentID != providerPaymentID ||
			payload.AssetCode != attempt.AssetCode ||
			payload.AmountMinor != attempt.AmountMinor {
			t.Fatalf("unexpected callback payload: %#v", payload)
		}
		if len(payload.Rewards) != 1 ||
			payload.Rewards[0].Key != itemID ||
			payload.Rewards[0].Type != "quantity" ||
			payload.Rewards[0].Quantity != 2*int64(order.Quantity) ||
			payload.Rewards[0].Unit != nil {
			t.Fatalf("unexpected callback rewards: %#v", payload.Rewards)
		}
		if err := callback.Successful(); err != nil {
			return err
		}
		cancel()
		return nil
	}, WithCallbackBatchSize(1), WithCallbackIdleDelay(time.Millisecond))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("on callback: %v", err)
	}
	if handled != 1 {
		t.Fatalf("unexpected callback handled count: got %d want 1", handled)
	}
}

func assertCallbackStatus(t *testing.T, ctx context.Context, db *sql.DB, eventType string, orderID uint64, want string) {
	t.Helper()
	var got string
	eventKey := fmt.Sprintf("%s:%d", eventType, orderID)
	if err := db.QueryRowContext(ctx, "SELECT status FROM payment_clb_event WHERE source_service = 'payment' AND event_type = $1 AND event_key = $2", eventType, eventKey).Scan(&got); err != nil {
		t.Fatalf("select callback status: %v", err)
	}
	if got != want {
		t.Fatalf("unexpected callback status: got %s want %s", got, want)
	}
}

func assertPaymentPurchaseStats(t *testing.T, env paymentTestEnv, productID string, purchaseCount, purchaseQuantity, uniqueBuyers, grossAmount uint64) {
	t.Helper()
	stats, err := env.api.Admin.GetStats(env.ctx, testWorkspaceID)
	if err != nil {
		t.Fatalf("get payment stats: %v", err)
	}
	if stats.ProductsTotal != 1 || stats.PurchaseCount != purchaseCount ||
		stats.PurchaseQuantity != purchaseQuantity || stats.UniqueBuyers != uniqueBuyers {
		t.Fatalf("unexpected payment stats: %#v", stats)
	}
	if len(stats.Assets) != 1 || stats.Assets[0].AssetCode != "RUB" ||
		stats.Assets[0].GrossAmountMinor != grossAmount {
		t.Fatalf("unexpected payment asset stats: %#v", stats.Assets)
	}

	productStats, err := env.api.Admin.GetProductStats(env.ctx, testWorkspaceID, productID)
	if err != nil {
		t.Fatalf("get payment product stats: %v", err)
	}
	if productStats.PurchaseCount != purchaseCount || productStats.PurchaseQuantity != purchaseQuantity {
		t.Fatalf("unexpected payment product stats: %#v", productStats)
	}

	now := time.Now()
	if err := env.api.Admin.RefreshDailyStats(env.ctx, testWorkspaceID, now.Add(-24*time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatalf("refresh payment daily stats: %v", err)
	}
	daily, err := env.api.Admin.ListDailyStats(
		env.ctx, testWorkspaceID, productID, now.Add(-24*time.Hour), now.Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf("list payment daily stats: %v", err)
	}
	if len(daily) != 1 || daily[0].PurchaseCount != purchaseCount ||
		daily[0].PurchaseQuantity != purchaseQuantity || daily[0].GrossAmountMinor != grossAmount {
		t.Fatalf("unexpected payment daily stats: %#v", daily)
	}

	overview, err := env.api.Admin.ListDailyOverview(
		env.ctx, testWorkspaceID, now.Add(-24*time.Hour), now.Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf("list payment daily overview: %v", err)
	}
	if len(overview) != 1 ||
		overview[0].ProductsTotal != 1 ||
		overview[0].VisibleProducts != 1 ||
		overview[0].PurchaseCount != purchaseCount ||
		overview[0].PurchaseQuantity != purchaseQuantity ||
		overview[0].UniqueBuyers != uniqueBuyers ||
		overview[0].FulfilledOrders != purchaseCount {
		t.Fatalf("unexpected payment daily overview: %#v", overview)
	}
}

func assertAdminPaymentReadMethods(t *testing.T, env paymentTestEnv, productID string, orderID uint64, attemptID uint64) {
	t.Helper()
	products, err := env.api.Admin.ListProducts(env.ctx, admin.ProductListParams{
		WorkspaceID: testWorkspaceID,
	})
	if err != nil {
		t.Fatalf("admin list products: %v", err)
	}
	if len(products) == 0 {
		t.Fatal("expected admin products")
	}

	orders, err := env.api.Admin.ListOrders(env.ctx, admin.OrderListParams{
		WorkspaceID: testWorkspaceID,
		ProductID:   productID,
	})
	if err != nil {
		t.Fatalf("admin list orders: %v", err)
	}
	if len(orders) == 0 || uint64(orders[0].ID) != orderID {
		t.Fatalf("unexpected admin orders: %#v", orders)
	}

	attempts, err := env.api.Admin.ListPaymentAttempts(env.ctx, admin.AttemptListParams{
		WorkspaceID: testWorkspaceID,
		OrderID:     orderID,
	})
	if err != nil {
		t.Fatalf("admin list attempts: %v", err)
	}
	if len(attempts) == 0 || uint64(attempts[0].ID) != attemptID {
		t.Fatalf("unexpected admin attempts: %#v", attempts)
	}

	events, err := env.api.Admin.ListPaymentEvents(env.ctx, admin.EventListParams{
		WorkspaceID: testWorkspaceID,
	})
	if err != nil {
		t.Fatalf("admin list payment events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected admin payment events")
	}

	callbacks, err := env.api.Admin.ListCallbackEvents(env.ctx, admin.CallbackEventListParams{
		WorkspaceID:   testWorkspaceID,
		SourceService: "payment",
	})
	if err != nil {
		t.Fatalf("admin list callback events: %v", err)
	}
	if len(callbacks) == 0 {
		t.Fatal("expected admin callback events")
	}
}

func assertPurchaseKeyUsed(t *testing.T, ctx context.Context, db *sql.DB, key string) {
	t.Helper()
	var status string
	var usedCount int
	if err := db.QueryRowContext(ctx, "SELECT status, used_count FROM payment_purchase_key WHERE key_hash = $1", sha256Hex(key)).Scan(&status, &usedCount); err != nil {
		t.Fatalf("select purchase key: %v", err)
	}
	if status != "used" || usedCount != 1 {
		t.Fatalf("unexpected purchase key state: status=%s used=%d", status, usedCount)
	}
}

func uniquePaymentID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func TestPaymentGlobalDynamicPricingAcrossWorkspaces(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	now := time.Now()
	secondWorkspaceID := "00000000-0000-0000-0000-000000000002"

	firstProductID := createPaymentProduct(t, env, testProductOptions{
		AssetCode:       "RUB",
		ListAmountMinor: 100,
	})
	secondProductID := createPaymentProduct(t, env, testProductOptions{
		WorkspaceID:     secondWorkspaceID,
		AssetCode:       "RUB",
		ListAmountMinor: 100,
	})
	fixedProductID := createPaymentProduct(t, env, testProductOptions{
		AssetCode:       "TON",
		ListAmountMinor: 1_000_000_000,
	})

	if _, err := env.api.Operational.UpdateAssetRate(env.ctx, operational.UpdateAssetRateParams{
		AssetCode:              "TON",
		ReferenceAssetCode:     repository.USDTAssetCode,
		ReferencePerAssetMinor: 2_000_000,
		Source:                 "integration-test",
		ObservedAt:             now,
	}); err != nil {
		t.Fatalf("seed global TON rate: %v", err)
	}

	referenceAsset := repository.USDTAssetCode
	referenceList := uint64(1_000_000)
	referenceDiscount := uint64(0)
	coefficient := "1"
	startsAt := now.Add(-time.Hour)
	endsAt := now.Add(time.Hour)
	for workspaceID, productID := range map[string]string{
		testWorkspaceID:   firstProductID,
		secondWorkspaceID: secondProductID,
	} {
		if _, err := env.api.Admin.CreateCatalogPrice(env.ctx, product.CreatePriceParams{
			WorkspaceID:                  workspaceID,
			ProductID:                    productID,
			AssetCode:                    "TON",
			PricingMode:                  repository.PricingModeDynamic,
			ReferenceAssetCode:           &referenceAsset,
			ReferenceListAmountMinor:     &referenceList,
			ReferenceDiscountAmountMinor: &referenceDiscount,
			Coefficient:                  &coefficient,
			StartsAt:                     &startsAt,
			EndsAt:                       &endsAt,
		}); err != nil {
			t.Fatalf("create dynamic TON price for %s: %v", workspaceID, err)
		}
	}

	result, err := env.api.Operational.UpdateAssetRate(env.ctx, operational.UpdateAssetRateParams{
		AssetCode:              "TON",
		ReferenceAssetCode:     repository.USDTAssetCode,
		ReferencePerAssetMinor: 4_000_000,
		Source:                 "integration-test",
		ObservedAt:             now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("update global TON rate: %v", err)
	}
	if result.UpdatedPrices != 2 || result.AffectedProducts != 2 || result.AffectedWorkspaces != 2 {
		t.Fatalf("unexpected global update result: %#v", result)
	}

	for workspaceID, productID := range map[string]string{
		testWorkspaceID:   firstProductID,
		secondWorkspaceID: secondProductID,
	} {
		item, err := env.api.User.GetProduct(env.ctx, user.GetProductParams{
			Identity:  paymentTestIdentity(workspaceID, 1001, 1, "dynamic-user"),
			ProductID: productID,
			AssetCode: "TON",
			Locale:    "ru",
		})
		if err != nil {
			t.Fatalf("get dynamic product for %s: %v", workspaceID, err)
		}
		if item.Price.PayableAmountMinor != 250_000_000 {
			t.Fatalf("unexpected dynamic price for %s: %d", workspaceID, item.Price.PayableAmountMinor)
		}
	}

	fixed, err := env.api.User.GetProduct(env.ctx, user.GetProductParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 1001, 1, "fixed-user"),
		ProductID: fixedProductID,
		AssetCode: "TON",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("get fixed TON product: %v", err)
	}
	if fixed.Price.PayableAmountMinor != 1_000_000_000 {
		t.Fatalf("fixed TON price changed: %d", fixed.Price.PayableAmountMinor)
	}

	rate, err := env.api.User.GetUSDTPrice(env.ctx, user.GetUSDTPriceParams{AssetCode: "TON"})
	if err != nil {
		t.Fatalf("get global TON rate: %v", err)
	}
	if rate.USDTPerAssetMinor != 4_000_000 {
		t.Fatalf("unexpected global TON rate: %d", rate.USDTPerAssetMinor)
	}
}

func TestPaymentPriceUpdaterStopsWithService(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	if err := env.api.Close(); err != nil {
		t.Fatalf("close initial payment service: %v", err)
	}
	if _, err := env.db.ExecContext(env.ctx, "DELETE FROM payment_asset_rate"); err != nil {
		t.Fatalf("clear asset rates: %v", err)
	}

	var requests atomic.Int32
	httpClient := &http.Client{Transport: paymentTestRateRoundTrip(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		tokenPath := request.URL.EscapedPath()
		tokenPath = tokenPath[strings.LastIndex(tokenPath, "/")+1:]
		tokenPath, _ = url.PathUnescape(tokenPath)
		addresses := strings.Split(tokenPath, ",")
		var body strings.Builder
		body.WriteByte('[')
		for index, address := range addresses {
			if index > 0 {
				body.WriteByte(',')
			}
			body.WriteString(`{"baseToken":{"address":`)
			body.WriteString(strconv.Quote(address))
			body.WriteString(`},"quoteToken":{"address":"EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c"},"priceNative":"2","priceUsd":"2","liquidity":{"usd":1000000}}`)
		}
		body.WriteByte(']')
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body.String())),
			Request:    request,
		}, nil
	})}
	service, err := NewWithDatabase(env.ctx, env.db, Options{
		PriceUpdateHTTPClient: httpClient,
		PriceUpdateInterval:   10 * time.Millisecond,
		PriceUpdateBaseURL:    "https://dex.example",
	})
	if err != nil {
		t.Fatalf("create payment service with updater: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for requests.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("price updater did not request rate")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for {
		rate, rateErr := service.User.GetUSDTPrice(env.ctx, user.GetUSDTPriceParams{AssetCode: "DOGS_TON"})
		if rateErr == nil && rate.USDTPerAssetMinor == 2_000_000 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("automatic DOGS_TON rate was not stored: rate=%#v err=%v", rate, rateErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for {
		rate, rateErr := service.User.GetUSDTPrice(env.ctx, user.GetUSDTPriceParams{AssetCode: "TON"})
		if rateErr == nil && rate.USDTPerAssetMinor == 1_000_000 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("automatic native TON rate was not stored: rate=%#v err=%v", rate, rateErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	usdtRate, err := service.User.GetUSDTPrice(env.ctx, user.GetUSDTPriceParams{AssetCode: repository.USDTAssetCode})
	if err != nil {
		t.Fatalf("get automatic USDT rate: %v", err)
	}
	if usdtRate.USDTPerAssetMinor != 1_000_000 {
		t.Fatalf("unexpected automatic USDT rate: %d", usdtRate.USDTPerAssetMinor)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("close payment service: %v", err)
	}
	requestCount := requests.Load()
	time.Sleep(50 * time.Millisecond)
	if requests.Load() != requestCount {
		t.Fatalf("price updater continued after Close: before=%d after=%d", requestCount, requests.Load())
	}
}

type paymentTestRateRoundTrip func(*http.Request) (*http.Response, error)

func (f paymentTestRateRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestPaymentStarsTopupExampleImportExport(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	req := loadPaymentImportExample(t, "stars_topup_import.json")
	now := time.Now().UTC()

	for assetCode, referencePerAssetMinor := range map[string]uint64{
		"TON":       1_530_000,
		"NOT_TON":   1_000_000,
		"DOGS_TON":  1_000_000,
		"MAJOR_TON": 1_000_000,
		"UTYA_TON":  1_000_000,
	} {
		if _, err := env.api.Operational.UpdateAssetRate(env.ctx, operational.UpdateAssetRateParams{
			AssetCode:              assetCode,
			ReferenceAssetCode:     repository.USDTAssetCode,
			ReferencePerAssetMinor: referencePerAssetMinor,
			Source:                 "integration-test",
			ObservedAt:             now,
		}); err != nil {
			t.Fatalf("seed %s rate: %v", assetCode, err)
		}
	}

	preview, err := env.api.Admin.PreviewImport(env.ctx, testWorkspaceID, req.Package)
	if err != nil {
		t.Fatalf("preview import: %v", err)
	}
	if preview.Counts.Groups != 1 || preview.Counts.Products != 1 ||
		preview.Counts.ProductItems != 1 || preview.Counts.Prices != 7 || preview.Counts.Localizations != 4 {
		t.Fatalf("unexpected preview counts: %+v", preview.Counts)
	}

	result, err := env.api.Admin.Import(env.ctx, testWorkspaceID, req)
	if err != nil {
		t.Fatalf("import example: %v", err)
	}
	if result.Imported.Groups != 1 || result.Imported.Products != 1 ||
		result.Imported.ProductItems != 1 || result.Imported.Prices != 7 || result.Imported.Localizations != 8 {
		t.Fatalf("unexpected import counts: %+v", result.Imported)
	}

	exported, err := env.api.Admin.Export(env.ctx, testWorkspaceID, repository.ExportRequest{})
	if err != nil {
		t.Fatalf("export after import: %v", err)
	}
	group := findExportGroup(t, exported, "topup")
	if group.TitleKey == nil || *group.TitleKey != "payment.group.topup.title" {
		t.Fatalf("unexpected group title key: %#v", group.TitleKey)
	}
	if len(group.Products) != 1 {
		t.Fatalf("expected one product in group, got %d", len(group.Products))
	}
	product := group.Products[0]
	if product.ID != "topup.stars.flexible" {
		t.Fatalf("unexpected product id: %s", product.ID)
	}
	if product.QuantityMode != "flexible" {
		t.Fatalf("unexpected quantity mode: %s", product.QuantityMode)
	}
	if product.GlobalLimit != 0 || product.UserLimit != 0 ||
		product.GlobalInterval != "UNLIMITED" || product.UserInterval != "UNLIMITED" {
		t.Fatalf("unexpected limits: global=%d/%s user=%d/%s",
			product.GlobalLimit, product.GlobalInterval, product.UserLimit, product.UserInterval)
	}
	if len(product.Items) != 1 {
		t.Fatalf("expected one product item, got %d", len(product.Items))
	}
	if product.Items[0].ItemID != "stars" || product.Items[0].Quantity != 100 || product.Items[0].Scale != 2 {
		t.Fatalf("unexpected product item: %+v", product.Items[0])
	}

	usdtOrder, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 7007, 2, "dynamic-usdt-user"),
		ProductID: "topup.stars.flexible",
		Quantity:  10,
		AssetCode: repository.USDTAssetCode,
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create USDT order: %v", err)
	}
	if usdtOrder.PayableAmountMinor != 150_000 {
		t.Fatalf("USDT amount = %d, want 150000 micro-USDT", usdtOrder.PayableAmountMinor)
	}

	tonOrder, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 7007, 2, "dynamic-ton-user"),
		ProductID: "topup.stars.flexible",
		Quantity:  10,
		AssetCode: "TON",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create dynamic TON order: %v", err)
	}
	if tonOrder.PayableAmountMinor != 98_039_220 {
		t.Fatalf(
			"dynamic TON amount = %d, want 98039220 nanoTON for 0.15 USDT at 1.53 USDT/TON",
			tonOrder.PayableAmountMinor,
		)
	}

	prices := indexExportPrices(product.Prices)
	assertFixedExamplePrice(t, prices["XTR"], "XTR", 1)
	assertFixedExamplePrice(t, prices["USDT_TON"], "USDT_TON", 15000)
	for _, code := range []string{"TON", "NOT_TON", "DOGS_TON", "MAJOR_TON", "UTYA_TON"} {
		assertDynamicExamplePrice(t, prices[code], code)
	}

	exportedJSON, err := json.Marshal(exported)
	if err != nil {
		t.Fatalf("marshal exported package: %v", err)
	}
	var exportedRoot map[string]any
	if err := json.Unmarshal(exportedJSON, &exportedRoot); err != nil {
		t.Fatalf("unmarshal exported package: %v", err)
	}
	if _, ok := exportedRoot["items"]; ok {
		t.Fatalf("payment export must not expose root items, got: %#v", exportedRoot["items"])
	}
	if _, ok := exportedRoot["references"]; ok {
		t.Fatal("payment export must not expose root references")
	}
	exportedContent := string(exportedJSON)
	if strings.Contains(exportedContent, "title_key") || strings.Contains(exportedContent, "description_key") {
		t.Fatalf("payment export must not expose localization keys: %s", exportedContent)
	}

	var localItemTable *string
	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT to_regclass('payment_item')::text",
	).Scan(&localItemTable); err != nil {
		t.Fatalf("inspect local payment item table: %v", err)
	}
	if localItemTable != nil {
		t.Fatalf("payment must not own local item catalog, found table %q", *localItemTable)
	}

	order, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 7007, 2, "opaque-item-user"),
		ProductID: "topup.stars.flexible",
		Quantity:  3,
		AssetCode: "XTR",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create order with opaque reward key: %v", err)
	}
	providerPaymentID := "opaque-item-payment"
	attempt, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          paymentAttemptIdentity(order),
		OrderID:           order.ID,
		ProviderCode:      "telegram_stars",
		ProviderPaymentID: &providerPaymentID,
	})
	if err != nil {
		t.Fatalf("create attempt with opaque reward key: %v", err)
	}
	completed, err := env.api.Operational.CompleteAttempt(env.ctx, checkout.CompleteAttemptParams{
		WorkspaceID:       testWorkspaceID,
		AttemptID:         attempt.ID,
		ProviderCode:      attempt.ProviderCode,
		ProviderPaymentID: &providerPaymentID,
		AmountMinor:       attempt.AmountMinor,
		AssetCode:         attempt.AssetCode,
	})
	if err != nil {
		t.Fatalf("complete attempt with opaque reward key: %v", err)
	}
	if completed.FulfillmentID == nil {
		t.Fatal("expected fulfillment for opaque reward key")
	}

	var quantity int64
	var scale int16
	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT quantity, scale FROM payment_fulfillment_item WHERE fulfillment_id = $1 AND item_id = $2",
		*completed.FulfillmentID,
		"stars",
	).Scan(&quantity, &scale); err != nil {
		t.Fatalf("read opaque fulfillment reward: %v", err)
	}
	if quantity != 300 || scale != 2 {
		t.Fatalf("unexpected opaque fulfillment reward: quantity=%d scale=%d", quantity, scale)
	}
}

func TestPaymentImportRejectsDynamicPriceWithPendingRate(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	req := loadPaymentImportExample(t, "stars_topup_import.json")

	if _, err := env.db.ExecContext(env.ctx, `
INSERT INTO payment_asset_rate (
    asset_code,
    reference_asset_code,
    reference_per_asset_minor,
    source,
    observed_at
)
VALUES ('TON', 'USDT_TON', 1, 'pending', now())
ON CONFLICT (asset_code, reference_asset_code) DO UPDATE SET
    reference_per_asset_minor = EXCLUDED.reference_per_asset_minor,
    source = EXCLUDED.source,
    observed_at = EXCLUDED.observed_at
`); err != nil {
		t.Fatalf("seed pending TON rate: %v", err)
	}

	_, err := env.api.Admin.Import(env.ctx, testWorkspaceID, req)
	if !errors.Is(err, repository.ErrAssetRateNotFound) {
		t.Fatalf("import error = %v, want %v", err, repository.ErrAssetRateNotFound)
	}
}

func loadPaymentImportExample(t *testing.T, name string) repository.ImportRequest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("examples", name))
	if err != nil {
		t.Fatalf("read payment example %s: %v", name, err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw payment example %s: %v", name, err)
	}
	pkg, ok := raw["package"].(map[string]any)
	if !ok {
		t.Fatalf("payment example %s must contain package object", name)
	}
	if _, ok := pkg["items"]; ok {
		t.Fatalf("payment example %s must not contain root items", name)
	}
	if _, ok := pkg["references"]; ok {
		t.Fatalf("payment example %s must not contain root references", name)
	}
	content := string(data)
	if strings.Contains(content, "title_key") || strings.Contains(content, "description_key") {
		t.Fatalf("payment example %s must not expose localization keys", name)
	}
	var req repository.ImportRequest
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("unmarshal payment example %s: %v", name, err)
	}
	return req
}

func findExportGroup(t *testing.T, pkg repository.ExportPackage, code string) repository.ExportProductGroup {
	t.Helper()
	for _, group := range pkg.Groups {
		if group.Code == code {
			return group
		}
	}
	t.Fatalf("group %s not found in export", code)
	return repository.ExportProductGroup{}
}

func indexExportPrices(prices []repository.ExportPrice) map[string]repository.ExportPrice {
	result := make(map[string]repository.ExportPrice, len(prices))
	for _, price := range prices {
		result[price.AssetCode] = price
	}
	return result
}

func assertFixedExamplePrice(t *testing.T, price repository.ExportPrice, assetCode string, amount uint64) {
	t.Helper()
	if price.AssetCode != assetCode {
		t.Fatalf("price for %s not found", assetCode)
	}
	if price.PricingMode != "fixed" || price.ListAmountMinor != amount || price.DiscountAmountMinor != 0 {
		t.Fatalf("unexpected fixed price for %s: %+v", assetCode, price)
	}
	if price.ReferenceAssetCode != nil || price.ReferenceListAmountMinor != nil ||
		price.ReferenceDiscountAmountMinor != nil || price.Coefficient != nil {
		t.Fatalf("fixed price %s must not have reference fields: %+v", assetCode, price)
	}
}

func assertDynamicExamplePrice(t *testing.T, price repository.ExportPrice, assetCode string) {
	t.Helper()
	if price.AssetCode != assetCode {
		t.Fatalf("price for %s not found", assetCode)
	}
	if price.PricingMode != "dynamic" {
		t.Fatalf("unexpected pricing mode for %s: %+v", assetCode, price)
	}
	if price.ReferenceAssetCode == nil || *price.ReferenceAssetCode != "USDT_TON" {
		t.Fatalf("unexpected reference asset for %s: %+v", assetCode, price.ReferenceAssetCode)
	}
	if price.ReferenceListAmountMinor == nil || *price.ReferenceListAmountMinor != 15000 {
		t.Fatalf("unexpected reference list amount for %s: %+v", assetCode, price.ReferenceListAmountMinor)
	}
	if price.ReferenceDiscountAmountMinor == nil || *price.ReferenceDiscountAmountMinor != 0 {
		t.Fatalf("unexpected reference discount amount for %s: %+v", assetCode, price.ReferenceDiscountAmountMinor)
	}
	if price.Coefficient == nil || *price.Coefficient != "1.000000000000" {
		t.Fatalf("unexpected coefficient for %s: %+v", assetCode, price.Coefficient)
	}
}

func TestPlategaAdapterFullCycle(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "platega_product",
		AssetCode:       platega.AssetCode,
		ListAmountMinor: 1299,
	})

	var createPayload plategaTestCreateTransactionPayload
	var createPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-MerchantId") != "merchant-1" || r.Header.Get("X-Secret") != "secret-1" {
			t.Fatalf("unexpected platega auth headers: merchant=%q secret=%q", r.Header.Get("X-MerchantId"), r.Header.Get("X-Secret"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/transaction/process":
			createPath = r.URL.Path
			if err := json.NewDecoder(r.Body).Decode(&createPayload); err != nil {
				t.Fatalf("decode platega create payload: %v", err)
			}
			writeJSON(t, w, map[string]any{
				"paymentMethod":  "SBPQR",
				"transactionId":  "platega-tx-1",
				"redirect":       "https://pay.platega.io?qrsbp",
				"return":         "https://example.com/success",
				"paymentDetails": "12.99 RUB",
				"status":         "PENDING",
				"expiresIn":      "00:15:00",
				"merchantId":     "merchant-1",
				"usdtRate":       90.5,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/h2h/platega-tx-1":
			writeJSON(t, w, map[string]any{
				"amount": 12.99,
				"qr":     "https://qr.nspk.ru/test",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/transaction/platega-tx-1":
			writeJSON(t, w, map[string]any{
				"id":     "platega-tx-1",
				"status": "CONFIRMED",
				"paymentDetails": map[string]any{
					"amount":   12.99,
					"currency": "RUB",
				},
				"paymentMethod": "SBPQR",
				"payload":       createPayload.Payload,
				"description":   createPayload.Description,
			})
		default:
			t.Fatalf("unexpected platega api request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	credentials := platega.Credentials{
		MerchantID: "merchant-1",
		Secret:     "secret-1",
		APIBaseURL: server.URL,
	}

	payment, err := env.api.Adapters.Platega.CreatePayment(env.ctx, platega.CreatePaymentParams{
		Credentials:    credentials,
		WorkspaceID:    testWorkspaceID,
		AppID:          8008,
		PlatformID:     3,
		PlatformUserID: "platega-user",
		ProductID:      productID,
		Locale:         "ru",
		Description:    "Platega product",
		ReturnURL:      "https://example.com/success",
		FailedURL:      "https://example.com/fail",
		PaymentMethod:  platega.PaymentMethodSBPQR,
		IdempotencyKey: "platega-full-cycle",
	})
	if err != nil {
		t.Fatalf("create platega payment: %v", err)
	}
	if createPath != "/transaction/process" {
		t.Fatalf("expected method-specific create path, got %s", createPath)
	}
	if createPayload.PaymentMethod == nil || *createPayload.PaymentMethod != int(platega.PaymentMethodSBPQR) {
		t.Fatalf("unexpected payment method payload: %#v", createPayload)
	}
	if createPayload.PaymentDetails.Amount.String() != "12.99" || createPayload.PaymentDetails.Currency != "RUB" {
		t.Fatalf("unexpected payment details: %#v", createPayload.PaymentDetails)
	}
	if createPayload.Payload != payment.OrderPublicID {
		t.Fatalf("expected order public id as payload, got %q", createPayload.Payload)
	}
	if payment.TransactionID != "platega-tx-1" || payment.PaymentURL == "" || payment.AmountMinor != 1299 {
		t.Fatalf("unexpected platega payment response: %#v", payment)
	}
	assertOrderStatus(t, env.ctx, env.db, payment.OrderID, "pending_payment")
	assertAttemptStatus(t, env.ctx, env.db, payment.AttemptID, "pending")

	h2h, err := env.api.Adapters.Platega.GetH2H(env.ctx, platega.GetH2HParams{
		Credentials:   credentials,
		TransactionID: payment.TransactionID,
	})
	if err != nil {
		t.Fatalf("get platega h2h: %v", err)
	}
	if !strings.Contains(h2h.QR, "qr.nspk.ru") {
		t.Fatalf("unexpected h2h response: %#v", h2h)
	}

	raw := []byte(`{"id":"platega-tx-1","amount":12.99,"currency":"RUB","status":"CONFIRMED","paymentMethod":2}`)
	headers := http.Header{}
	headers.Set("X-MerchantId", "merchant-1")
	headers.Set("X-Secret", "secret-1")
	result, err := env.api.Adapters.Platega.HandleWebhook(env.ctx, platega.WebhookRequest{
		Credentials: credentials,
		WorkspaceID: testWorkspaceID,
		Raw:         raw,
		Headers:     headers,
	})
	if err != nil {
		t.Fatalf("handle platega webhook: %v", err)
	}
	if result.OrderID != payment.OrderID || result.AttemptID != payment.AttemptID {
		t.Fatalf("unexpected platega webhook result: %#v", result)
	}
	assertOrderStatus(t, env.ctx, env.db, payment.OrderID, "fulfilled")
	assertAttemptStatus(t, env.ctx, env.db, payment.AttemptID, "succeeded")

	again, err := env.api.Adapters.Platega.SyncPayment(env.ctx, platega.SyncPaymentParams{
		Credentials:   credentials,
		WorkspaceID:   testWorkspaceID,
		TransactionID: payment.TransactionID,
	})
	if err != nil {
		t.Fatalf("sync platega payment: %v", err)
	}
	if !again.AlreadyDone {
		t.Fatalf("expected idempotent platega sync, got %#v", again)
	}
}

func TestPlategaAdapterRejectsInvalidWebhookCredentials(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	credentials := platega.Credentials{
		MerchantID: "merchant-1",
		Secret:     "secret-1",
		APIBaseURL: "https://example.com",
	}

	headers := http.Header{}
	headers.Set("X-MerchantId", "merchant-1")
	headers.Set("X-Secret", "wrong-secret")
	_, err := env.api.Adapters.Platega.HandleWebhook(env.ctx, platega.WebhookRequest{
		Credentials: credentials,
		Raw:         []byte(`{"id":"tx"}`),
		Headers:     headers,
	})
	if err == nil {
		t.Fatal("expected invalid platega webhook credentials error")
	}
}

func TestPlategaUsesExactMonetaryAmounts(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	const amountMinor = uint64(9007199254740993)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "platega_exact_amount",
		AssetCode:       platega.AssetCode,
		ListAmountMinor: amountMinor,
	})

	var receivedAmount json.Number
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload plategaTestCreateTransactionPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode exact platega amount: %v", err)
		}
		receivedAmount = payload.PaymentDetails.Amount
		writeJSON(t, w, map[string]any{
			"transactionId": "platega-exact-amount",
			"status":        "PENDING",
			"url":           "https://pay.example/exact",
		})
	}))
	defer server.Close()
	credentials := platega.Credentials{
		MerchantID: "merchant-exact",
		Secret:     "secret-exact",
		APIBaseURL: server.URL,
	}
	payment, err := env.api.Adapters.Platega.CreatePayment(env.ctx, platega.CreatePaymentParams{
		Credentials:    credentials,
		WorkspaceID:    testWorkspaceID,
		AppID:          8100,
		PlatformID:     3,
		PlatformUserID: "exact-amount-user",
		ProductID:      productID,
		IdempotencyKey: "platega-exact-amount-key",
	})
	if err != nil {
		t.Fatalf("create exact platega payment: %v", err)
	}
	if receivedAmount.String() != "90071992547409.93" {
		t.Fatalf("provider amount = %q, want exact decimal", receivedAmount.String())
	}

	headers := http.Header{}
	headers.Set("X-MerchantId", credentials.MerchantID)
	headers.Set("X-Secret", credentials.Secret)
	result, err := env.api.Adapters.Platega.HandleWebhook(env.ctx, platega.WebhookRequest{
		Credentials: credentials,
		WorkspaceID: testWorkspaceID,
		Raw: []byte(
			`{"id":"platega-exact-amount","amount":90071992547409.93,"currency":"RUB","status":"CONFIRMED","paymentMethod":2}`,
		),
		Headers: headers,
	})
	if err != nil {
		t.Fatalf("complete exact platega payment: %v", err)
	}
	if result.FulfilledID == nil || payment.AmountMinor != amountMinor {
		t.Fatalf("exact platega result: payment=%+v result=%+v", payment, result)
	}
}

func TestPlategaRejectsFractionalMinorUnits(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	credentials := platega.Credentials{
		MerchantID: "merchant-fraction",
		Secret:     "secret-fraction",
	}
	headers := http.Header{}
	headers.Set("X-MerchantId", credentials.MerchantID)
	headers.Set("X-Secret", credentials.Secret)

	_, err := env.api.Adapters.Platega.HandleWebhook(env.ctx, platega.WebhookRequest{
		Credentials: credentials,
		WorkspaceID: testWorkspaceID,
		Raw:         []byte(`{"id":"fractional","amount":1.004,"currency":"RUB","status":"CONFIRMED","paymentMethod":2}`),
		Headers:     headers,
	})
	if !errors.Is(err, platega.ErrAmountInvalid) {
		t.Fatalf("fractional minor units error = %v", err)
	}
}

func TestPlategaRejectsAmountAboveDatabaseRange(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	credentials := platega.Credentials{
		MerchantID: "merchant-overflow",
		Secret:     "secret-overflow",
	}
	headers := http.Header{}
	headers.Set("X-MerchantId", credentials.MerchantID)
	headers.Set("X-Secret", credentials.Secret)

	_, err := env.api.Adapters.Platega.HandleWebhook(env.ctx, platega.WebhookRequest{
		Credentials: credentials,
		WorkspaceID: testWorkspaceID,
		Raw:         []byte(`{"id":"overflow","amount":92233720368547758.08,"currency":"RUB","status":"CONFIRMED","paymentMethod":2}`),
		Headers:     headers,
	})
	if !errors.Is(err, platega.ErrAmountInvalid) {
		t.Fatalf("amount overflow error = %v", err)
	}
}

type plategaTestCreateTransactionPayload struct {
	PaymentMethod  *int `json:"paymentMethod"`
	PaymentDetails struct {
		Amount   json.Number `json:"amount"`
		Currency string      `json:"currency"`
	} `json:"paymentDetails"`
	Description string `json:"description"`
	ReturnURL   string `json:"return"`
	FailedURL   string `json:"failedUrl"`
	Payload     string `json:"payload"`
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("write json response: %v", err)
	}
}

func TestPaymentCacheVersionInvalidatesOtherNode(t *testing.T) {
	cache := newPaymentSharedTestCache()
	options := paymentTestOptions()
	options.Cache = cache
	options.CacheL1Delay = time.Minute
	options.CacheL2Delay = time.Minute

	env := setupPaymentIntegrationTestWithOptions(t, options)
	nodeB, err := NewWithDatabase(env.ctx, env.db, options)
	if err != nil {
		t.Fatalf("create second payment node: %v", err)
	}
	t.Cleanup(func() { _ = nodeB.Close() })

	now := time.Now().UTC()
	productID := "cache-version-product"
	groupCode := "cache-version-group"
	titleKey := productID + ".title"
	availableFrom := now.Add(-time.Hour)
	availableUntil := now.Add(time.Hour)

	if err := env.api.Admin.SaveProductGroup(env.ctx, product.UpsertGroupParams{
		WorkspaceID: testWorkspaceID,
		Code:        groupCode,
		IsActive:    true,
	}); err != nil {
		t.Fatalf("create cache test group: %v", err)
	}
	if err := env.api.Admin.SaveLocalization(env.ctx, product.UpsertLocalizationParams{
		WorkspaceID:     testWorkspaceID,
		Locale:          "ru",
		LocalizationKey: titleKey,
		Value:           "Old title",
	}); err != nil {
		t.Fatalf("create cache test localization: %v", err)
	}
	if err := env.api.Admin.SaveProduct(env.ctx, product.UpsertParams{
		WorkspaceID:    testWorkspaceID,
		ID:             productID,
		GroupCode:      &groupCode,
		TitleKey:       titleKey,
		QuantityMode:   "fixed",
		GlobalInterval: "UNLIMITED",
		UserLimit:      1,
		UserInterval:   "DAY",
		AvailableFrom:  &availableFrom,
		AvailableUntil: &availableUntil,
		IsVisible:      true,
	}); err != nil {
		t.Fatalf("create cache test product: %v", err)
	}
	if _, err := env.api.Admin.CreateCatalogPrice(env.ctx, product.CreatePriceParams{
		WorkspaceID:     testWorkspaceID,
		ProductID:       productID,
		AssetCode:       "RUB",
		ListAmountMinor: 100,
		StartsAt:        &availableFrom,
		EndsAt:          &availableUntil,
	}); err != nil {
		t.Fatalf("create cache test price: %v", err)
	}

	warm, err := nodeB.User.GetProduct(env.ctx, paymentGetProductParams(productID, "cache-user"))
	if err != nil {
		t.Fatalf("warm product cache on node B: %v", err)
	}
	if warm.Title != "Old title" || warm.Limit.User.Limit != 1 {
		t.Fatalf("unexpected warm product: %+v", warm)
	}

	if err := env.api.Admin.SaveLocalization(env.ctx, product.UpsertLocalizationParams{
		WorkspaceID:     testWorkspaceID,
		Locale:          "ru",
		LocalizationKey: titleKey,
		Value:           "New title",
	}); err != nil {
		t.Fatalf("update cache test localization: %v", err)
	}
	if err := env.api.Admin.SaveProduct(env.ctx, product.UpsertParams{
		WorkspaceID:    testWorkspaceID,
		ID:             productID,
		GroupCode:      &groupCode,
		TitleKey:       titleKey,
		QuantityMode:   "fixed",
		GlobalInterval: "UNLIMITED",
		UserLimit:      3,
		UserInterval:   "DAY",
		AvailableFrom:  &availableFrom,
		AvailableUntil: &availableUntil,
		IsVisible:      true,
	}); err != nil {
		t.Fatalf("update cache test product: %v", err)
	}

	updated, err := nodeB.User.GetProduct(env.ctx, paymentGetProductParams(productID, "cache-user"))
	if err != nil {
		t.Fatalf("read invalidated product on node B: %v", err)
	}
	if updated.Title != "New title" || updated.Limit.User.Limit != 3 {
		t.Fatalf("node B returned stale product: %+v", updated)
	}
}

func TestPaymentImportBatchesMoreThanPostgresParameterLimit(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	const productCount = 3001
	workspaceID := testsupport.WorkspaceID("large-import-workspace")

	products := make([]repository.ExportProduct, 0, productCount)
	for index := 0; index < productCount; index++ {
		productID := fmt.Sprintf("large-import-product-%04d", index)
		products = append(products, repository.ExportProduct{
			ID:             productID,
			TitleKey:       productID + ".title",
			QuantityMode:   "fixed",
			GlobalInterval: "UNLIMITED",
			UserInterval:   "UNLIMITED",
			IsVisible:      true,
		})
	}

	result, err := env.api.Admin.Import(env.ctx, workspaceID, admin.ImportRequest{
		Package: admin.ExportPackage{
			Format:   repository.ExportFormat,
			Service:  "payment",
			Products: products,
		},
		ConflictStrategy: repository.ImportConflictUpdate,
	})
	if err != nil {
		t.Fatalf("import package larger than PostgreSQL parameter limit: %v", err)
	}
	if result.Imported.Products != productCount {
		t.Fatalf("imported products = %d, want %d", result.Imported.Products, productCount)
	}

	var stored int
	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT COUNT(*) FROM payment_product WHERE workspace_id = $1",
		workspaceID,
	).Scan(&stored); err != nil {
		t.Fatalf("count imported products: %v", err)
	}
	if stored != productCount {
		t.Fatalf("stored products = %d, want %d", stored, productCount)
	}
}

func TestPaymentImportSerializesWithAdminWrite(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	workspaceID := testsupport.WorkspaceID("concurrent-import-workspace")

	transaction, err := env.db.BeginTx(env.ctx, nil)
	if err != nil {
		t.Fatalf("begin competing transaction: %v", err)
	}
	t.Cleanup(func() { _ = transaction.Rollback() })
	if _, err := transaction.ExecContext(
		env.ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		"payment:"+workspaceID,
	); err != nil {
		t.Fatalf("lock payment workspace: %v", err)
	}

	importResult := make(chan error, 1)
	go func() {
		_, err := env.api.Admin.Import(env.ctx, workspaceID, admin.ImportRequest{
			Package: admin.ExportPackage{
				Format:  repository.ExportFormat,
				Service: "payment",
				Products: []repository.ExportProduct{
					paymentImportTestProduct("concurrent-import-product"),
				},
			},
			ConflictStrategy: repository.ImportConflictUpdate,
		})
		importResult <- err
	}()

	waitForPaymentWorkspaceLock(t, env, 1)

	adminResult := make(chan error, 1)
	go func() {
		adminResult <- env.api.Admin.SaveProduct(env.ctx, product.UpsertParams{
			WorkspaceID:    workspaceID,
			ID:             "concurrent-admin-product",
			TitleKey:       "concurrent-admin-product.title",
			QuantityMode:   "fixed",
			GlobalInterval: "UNLIMITED",
			UserInterval:   "UNLIMITED",
			AvailableFrom:  timePointer(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)),
			AvailableUntil: timePointer(time.Date(2124, 1, 1, 0, 0, 0, 0, time.UTC)),
			IsVisible:      true,
		})
	}()

	waitForPaymentWorkspaceLock(t, env, 2)

	if err := transaction.Commit(); err != nil {
		t.Fatalf("release payment workspace lock: %v", err)
	}
	if err := <-importResult; err != nil {
		t.Fatalf("concurrent import: %v", err)
	}
	if err := <-adminResult; err != nil {
		t.Fatalf("concurrent admin write: %v", err)
	}

	var stored int
	if err := env.db.QueryRowContext(
		env.ctx,
		`SELECT COUNT(*)
FROM payment_product
WHERE workspace_id = $1
  AND id IN ('concurrent-import-product', 'concurrent-admin-product')`,
		workspaceID,
	).Scan(&stored); err != nil {
		t.Fatalf("count concurrent payment products: %v", err)
	}
	if stored != 2 {
		t.Fatalf("concurrent operations stored %d products, want 2", stored)
	}
}

func waitForPaymentWorkspaceLock(t *testing.T, env paymentTestEnv, minimum int) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting int
		err := env.db.QueryRowContext(env.ctx, `
SELECT COUNT(*)
FROM pg_stat_activity
WHERE datname = current_database()
  AND wait_event_type = 'Lock'
  AND query LIKE '%pg_advisory_xact_lock%'`).Scan(&waiting)
		if err != nil {
			t.Fatalf("inspect payment workspace lock waiters: %v", err)
		}
		if waiting >= minimum {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("payment workspace lock waiters = %d, want at least %d", waiting, minimum)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func paymentImportTestProduct(id string) repository.ExportProduct {
	return repository.ExportProduct{
		ID:             id,
		TitleKey:       id + ".title",
		QuantityMode:   "fixed",
		GlobalInterval: "UNLIMITED",
		UserInterval:   "UNLIMITED",
		IsVisible:      true,
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func paymentGetProductParams(productID string, platformUserID string) product.GetParams {
	return product.GetParams{
		Identity: services.Identity{
			WorkspaceID:    testWorkspaceID,
			AppID:          1,
			PlatformID:     1,
			PlatformUserID: platformUserID,
		},
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	}
}

type paymentSharedTestCacheEntry struct {
	value     []byte
	expiresAt time.Time
}

type paymentSharedTestCache struct {
	mu      sync.Mutex
	entries map[string]paymentSharedTestCacheEntry
}

func newPaymentSharedTestCache() *paymentSharedTestCache {
	return &paymentSharedTestCache{
		entries: make(map[string]paymentSharedTestCacheEntry),
	}
}

func (c *paymentSharedTestCache) GetWithTTL(key string) ([]byte, time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.entries[key]
	if !exists {
		return nil, 0, nil
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return nil, 0, nil
	}

	return append([]byte(nil), entry.value...), time.Until(entry.expiresAt), nil
}

func (c *paymentSharedTestCache) Set(key string, value []byte, expiration time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := paymentSharedTestCacheEntry{
		value: append([]byte(nil), value...),
	}
	if expiration > 0 {
		entry.expiresAt = time.Now().Add(expiration)
	}
	c.entries[key] = entry

	return nil
}

func (c *paymentSharedTestCache) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)

	return nil
}

func (c *paymentSharedTestCache) Reset() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	clear(c.entries)

	return nil
}

func (c *paymentSharedTestCache) Close() error {
	return nil
}

var _ Storage = (*paymentSharedTestCache)(nil)

type testProductOptions struct {
	ProductID           string
	WorkspaceID         string
	AssetCode           string
	ListAmountMinor     uint64
	DiscountAmountMinor uint64
	GlobalLimit         int32
	GlobalInterval      string
	GlobalIntervalCount int32
	UserLimit           int32
	UserInterval        string
	UserIntervalCount   int32
	AvailableFrom       time.Time
	AvailableUntil      time.Time
	IsVisible           bool
	IsHidden            bool
	IsClosed            bool
}

func TestPaymentWorkspaceIsolation(t *testing.T) {
	// Test database setup.
	// Create the MySQL database connection and bootstrap the payment schema.
	// Verify workspace separates catalogs without binding products to AppID.
	env := setupPaymentIntegrationTest(t)
	workspaceA := fmt.Sprintf("00000000-0000-0000-0000-%012d", time.Now().UnixNano()%1_000_000_000_000)
	workspaceB := fmt.Sprintf("11111111-1111-1111-1111-%012d", time.Now().UnixNano()%1_000_000_000_000)

	// Workspace catalog isolation.
	// Create a product only inside workspace A and fetch it from two workspaces.
	// Verify the same AppID cannot see workspace A catalog entries through workspace B.
	productID := createPaymentProduct(t, env, testProductOptions{
		WorkspaceID:     workspaceA,
		AssetCode:       "RUB",
		ListAmountMinor: 1000,
	})
	if _, err := env.api.User.GetProduct(env.ctx, product.GetParams{
		Identity:  paymentTestIdentity(workspaceA, 4500, 1, "workspace-user"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	}); err != nil {
		t.Fatalf("expected workspace A product to be visible, got %v", err)
	}
	if _, err := env.api.User.GetProduct(env.ctx, product.GetParams{
		Identity:  paymentTestIdentity(workspaceB, 4500, 1, "workspace-user"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected workspace B lookup to be isolated, got %v", err)
	}

	sameProductID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       productID,
		WorkspaceID:     workspaceB,
		AssetCode:       "RUB",
		ListAmountMinor: 2500,
	})
	if sameProductID != productID {
		t.Fatalf("expected duplicate product id across workspaces, got %s", sameProductID)
	}
	workspaceBProduct, err := env.api.User.GetProduct(env.ctx, product.GetParams{
		Identity:  paymentTestIdentity(workspaceB, 4500, 1, "workspace-user"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("expected workspace B product with duplicate id to be visible, got %v", err)
	}
	if workspaceBProduct.Price.ListAmountMinor != 2500 {
		t.Fatalf("expected workspace B duplicate product price 2500, got %d", workspaceBProduct.Price.ListAmountMinor)
	}

	// Workspace checkout isolation.
	// Create orders for the same product id after both workspaces define it.
	// Verify checkout resolves catalog data by workspace instead of global product ids.
	if _, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(workspaceA, 4500, 1, "workspace-user"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	}); err != nil {
		t.Fatalf("expected workspace A order to be created, got %v", err)
	}
	if _, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(workspaceB, 4500, 1, "workspace-user"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	}); err != nil {
		t.Fatalf("expected workspace B order for its duplicate product to be created, got %v", err)
	}
}

func TestPaymentLimitsAcrossAllIntervals(t *testing.T) {
	// Test database setup.
	// Create the MySQL database connection and bootstrap the payment schema.
	// Verify limit checks run against real persisted paid orders.
	env := setupPaymentIntegrationTest(t)

	intervals := []string{"SECOND", "MINUTE", "HOUR", "DAY", "WEEK", "MONTH", "ONCE"}
	for _, interval := range intervals {
		t.Run("user_"+interval, func(t *testing.T) {
			intervalCount := stablePaymentTestIntervalCount(interval)

			// User purchase limit.
			// Complete one purchase for a user-limited product in the selected interval.
			// Verify the same user is blocked while another user can still buy it.
			productID := createPaymentProduct(t, env, testProductOptions{
				AssetCode:           "RUB",
				ListAmountMinor:     1000,
				DiscountAmountMinor: 100,
				UserLimit:           1,
				UserInterval:        interval,
				UserIntervalCount:   intervalCount,
			})

			completeTestPayment(t, env, productID, "limit-user-"+interval, "buyer-a", "yookassa", "RUB")

			if _, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
				Identity:  paymentTestIdentity(testWorkspaceID, 4100, 1, "buyer-a"),
				ProductID: productID,
				AssetCode: "RUB",
				Locale:    "ru",
			}); !errors.Is(err, repository.ErrProductLocked) {
				t.Fatalf("expected user limit lock, got %v", err)
			}

			if _, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
				Identity:  paymentTestIdentity(testWorkspaceID, 4101, 1, "buyer-a"),
				ProductID: productID,
				AssetCode: "RUB",
				Locale:    "ru",
			}); !errors.Is(err, repository.ErrProductLocked) {
				t.Fatalf("expected user limit lock across AppID, got %v", err)
			}

			if _, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
				Identity:  paymentTestIdentity(testWorkspaceID, 4100, 1, "buyer-b"),
				ProductID: productID,
				AssetCode: "RUB",
				Locale:    "ru",
			}); err != nil {
				t.Fatalf("expected another user to bypass user limit, got %v", err)
			}
		})

		t.Run("global_"+interval, func(t *testing.T) {
			intervalCount := stablePaymentTestIntervalCount(interval)

			// Global purchase limit.
			// Complete one purchase for a globally limited product in the selected interval.
			// Verify every user is blocked after the global quota is consumed.
			productID := createPaymentProduct(t, env, testProductOptions{
				AssetCode:           "RUB",
				ListAmountMinor:     1000,
				GlobalLimit:         1,
				GlobalInterval:      interval,
				GlobalIntervalCount: intervalCount,
				UserInterval:        "UNLIMITED",
				DiscountAmountMinor: 0,
			})

			completeTestPayment(t, env, productID, "limit-global-"+interval, "buyer-a", "yookassa", "RUB")

			if _, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
				Identity:  paymentTestIdentity(testWorkspaceID, 4101, 1, "buyer-b"),
				ProductID: productID,
				AssetCode: "RUB",
				Locale:    "ru",
			}); !errors.Is(err, repository.ErrProductLocked) {
				t.Fatalf("expected global limit lock, got %v", err)
			}
		})
	}

	t.Run("unlimited", func(t *testing.T) {
		// Unlimited purchase interval.
		// Complete one purchase for a product without global or user limits.
		// Verify the same user can create another order after a completed purchase.
		productID := createPaymentProduct(t, env, testProductOptions{
			AssetCode:       "RUB",
			ListAmountMinor: 1000,
			UserInterval:    "UNLIMITED",
			GlobalInterval:  "UNLIMITED",
		})

		completeTestPayment(t, env, productID, "limit-unlimited", "buyer-a", "yookassa", "RUB")

		if _, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
			Identity:  paymentTestIdentity(testWorkspaceID, 4100, 1, "buyer-a"),
			ProductID: productID,
			AssetCode: "RUB",
			Locale:    "ru",
		}); err != nil {
			t.Fatalf("expected unlimited product to remain available, got %v", err)
		}
	})
}

func stablePaymentTestIntervalCount(interval string) int32 {

	switch interval {
	case "SECOND":
		return 1_000_000_000
	case "MINUTE":
		return 10_000_000
	case "HOUR":
		return 100_000
	case "DAY":
		return 10_000
	case "WEEK":
		return 1_000
	case "MONTH":
		return 1_200
	default:
		return 1
	}

}

func TestPaymentOrderLimitReservationLifecycle(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		AssetCode:           "RUB",
		ListAmountMinor:     1000,
		GlobalLimit:         1,
		GlobalInterval:      "ONCE",
		GlobalIntervalCount: 1,
	})

	firstOrder, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 4110, 1, "reservation-owner"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create reserved order: %v", err)
	}

	if _, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 4110, 2, "reservation-contender"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	}); !errors.Is(err, repository.ErrProductLocked) {
		t.Fatalf("expected pending order to reserve global limit, got %v", err)
	}

	if _, err := env.api.Admin.UpdateOrderStatus(
		env.ctx,
		testWorkspaceID,
		firstOrder.ID,
		"canceled",
	); err != nil {
		t.Fatalf("cancel reserved order: %v", err)
	}

	if _, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 4110, 2, "reservation-contender"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	}); err != nil {
		t.Fatalf("expected canceled order to release reservation, got %v", err)
	}

	snapshotProductID := createPaymentProduct(t, env, testProductOptions{
		AssetCode:           "RUB",
		ListAmountMinor:     1000,
		GlobalLimit:         1,
		GlobalInterval:      "ONCE",
		GlobalIntervalCount: 1,
	})
	snapshotOrder, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 4111, 1, "snapshot-owner"),
		ProductID: snapshotProductID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create snapshot order: %v", err)
	}
	providerPaymentID := uniquePaymentID("limit-snapshot")
	attempt, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          paymentAttemptIdentity(snapshotOrder),
		OrderID:           snapshotOrder.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &providerPaymentID,
	})
	if err != nil {
		t.Fatalf("create snapshot attempt: %v", err)
	}
	if _, err := env.db.ExecContext(
		env.ctx,
		`UPDATE payment_product
SET global_limit = 0,
    global_interval = 'UNLIMITED',
    global_interval_count = 0
WHERE workspace_id = $1
  AND id = $2`,
		testWorkspaceID,
		snapshotProductID,
	); err != nil {
		t.Fatalf("change product after order creation: %v", err)
	}
	if _, err := env.api.Operational.CompleteAttempt(env.ctx, checkout.CompleteAttemptParams{
		WorkspaceID:       testWorkspaceID,
		AttemptID:         attempt.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &providerPaymentID,
		AmountMinor:       attempt.AmountMinor,
		AssetCode:         "RUB",
	}); err != nil {
		t.Fatalf("complete order from limit snapshot: %v", err)
	}
	var paidCount int64
	var reservedCount int64
	if err := env.db.QueryRowContext(
		env.ctx,
		`SELECT paid_count, reserved_count
FROM payment_product_limit_counter
WHERE workspace_id = $1
  AND product_id = $2
  AND counter_scope = 'global'`,
		testWorkspaceID,
		snapshotProductID,
	).Scan(&paidCount, &reservedCount); err != nil {
		t.Fatalf("read consumed snapshot reservation: %v", err)
	}
	if paidCount != 1 || reservedCount != 0 {
		t.Fatalf("unexpected snapshot counter: paid=%d reserved=%d", paidCount, reservedCount)
	}
}

func TestPaymentStaleOrderExpirationReleasesLimitAndPurchaseKey(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		AssetCode:           "RUB",
		ListAmountMinor:     1000,
		GlobalLimit:         1,
		GlobalInterval:      "ONCE",
		GlobalIntervalCount: 1,
	})

	key, err := env.api.Admin.CreateProductKey(env.ctx, product.CreateKeyParams{
		WorkspaceID:    testWorkspaceID,
		AppID:          4120,
		PlatformID:     1,
		PlatformUserID: "stale-recipient",
		ProductID:      productID,
		MaxUses:        1,
	})
	if err != nil {
		t.Fatalf("create purchase key: %v", err)
	}

	order, err := env.api.User.CreateOrderByKey(env.ctx, checkout.CreateOrderByKeyParams{
		Key: key,
		Payer: &services.Actor{
			PlatformID:     1,
			PlatformUserID: "stale-payer",
		},
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create stale order: %v", err)
	}

	if _, err := env.db.ExecContext(
		env.ctx,
		"UPDATE payment_order SET created_at = now() - INTERVAL '2 hours' WHERE id = $1",
		order.ID,
	); err != nil {
		t.Fatalf("age order: %v", err)
	}
	if err := env.api.expireStaleOrders(env.ctx); err != nil {
		t.Fatalf("expire stale orders: %v", err)
	}

	assertOrderStatus(t, env.ctx, env.db, order.ID, "expired")
	if _, err := env.api.User.CreateOrderByKey(env.ctx, checkout.CreateOrderByKeyParams{
		Key: key,
		Payer: &services.Actor{
			PlatformID:     2,
			PlatformUserID: "replacement-payer",
		},
		AssetCode: "RUB",
		Locale:    "ru",
	}); err != nil {
		t.Fatalf("create replacement order after expiration: %v", err)
	}
}

func TestPaymentStaleOrderAndActiveAttemptExpireConsistently(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:           "active_provider_attempt",
		AssetCode:           "RUB",
		ListAmountMinor:     1000,
		GlobalLimit:         1,
		GlobalInterval:      "ONCE",
		GlobalIntervalCount: 1,
	})
	order, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 4121, 1, "delayed-payment"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create delayed order: %v", err)
	}
	providerPaymentID := uniquePaymentID("delayed-payment")
	attempt, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          paymentAttemptIdentity(order),
		OrderID:           order.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &providerPaymentID,
	})
	if err != nil {
		t.Fatalf("create delayed attempt: %v", err)
	}
	if _, err := env.db.ExecContext(env.ctx, `
		UPDATE payment_order SET created_at = now() - INTERVAL '2 hours' WHERE id = $1
	`, order.ID); err != nil {
		t.Fatalf("age delayed order: %v", err)
	}
	if _, err := env.db.ExecContext(env.ctx, `
		UPDATE payment_attempt SET updated_at = now() - INTERVAL '2 hours' WHERE id = $1
	`, attempt.ID); err != nil {
		t.Fatalf("age delayed attempt: %v", err)
	}

	if err := env.api.expireStaleOrders(env.ctx); err != nil {
		t.Fatalf("expire stale orders: %v", err)
	}
	assertOrderStatus(t, env.ctx, env.db, order.ID, "expired")
	assertAttemptStatus(t, env.ctx, env.db, attempt.ID, "expired")

	replacement, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 4121, 1, "replacement"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create replacement after expiration: %v", err)
	}
	if replacement.ID == order.ID {
		t.Fatalf("replacement reused expired order: %+v", replacement)
	}
}

func TestPaymentSubscriptionRejectsMismatchedOrderAndAttempt(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		AssetCode:       "RUB",
		ListAmountMinor: 1000,
	})

	createOrderAttempt := func(userID string) (uint64, uint64) {
		order, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
			Identity:  paymentTestIdentity(testWorkspaceID, 4130, 1, userID),
			ProductID: productID,
			AssetCode: "RUB",
			Locale:    "ru",
		})
		if err != nil {
			t.Fatalf("create order for %s: %v", userID, err)
		}

		providerPaymentID := uniquePaymentID("subscription-" + userID)
		attempt, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
			Identity:          paymentAttemptIdentity(order),
			OrderID:           order.ID,
			ProviderCode:      "yookassa",
			ProviderPaymentID: &providerPaymentID,
		})
		if err != nil {
			t.Fatalf("create attempt for %s: %v", userID, err)
		}

		return order.ID, attempt.ID
	}

	firstOrderID, firstAttemptID := createOrderAttempt("subscription-first")
	secondOrderID, secondAttemptID := createOrderAttempt("subscription-second")
	if _, err := env.api.Admin.UpsertSubscription(env.ctx, admin.SubscriptionUpsertParams{
		WorkspaceID:            testWorkspaceID,
		ProviderCode:           "yookassa",
		ProviderSubscriptionID: uniquePaymentID("subscription-valid"),
		AppID:                  4130,
		PlatformID:             1,
		PlatformUserID:         "subscription-first",
		ProductID:              productID,
		OrderID:                utils.Ref(int64(firstOrderID)),
		AttemptID:              utils.Ref(int64(firstAttemptID)),
		Status:                 "active",
	}); err != nil {
		t.Fatalf("upsert valid subscription: %v", err)
	}

	if _, err := env.api.Admin.UpsertSubscription(env.ctx, admin.SubscriptionUpsertParams{
		WorkspaceID:            testWorkspaceID,
		ProviderCode:           "yookassa",
		ProviderSubscriptionID: uniquePaymentID("subscription-mismatch"),
		AppID:                  4130,
		PlatformID:             1,
		PlatformUserID:         "subscription-first",
		ProductID:              productID,
		OrderID:                utils.Ref(int64(firstOrderID)),
		AttemptID:              utils.Ref(int64(secondAttemptID)),
		Status:                 "active",
	}); !errors.Is(err, repository.ErrPaymentMismatch) {
		t.Fatalf(
			"mismatched subscription must fail: first_order=%d second_order=%d err=%v",
			firstOrderID,
			secondOrderID,
			err,
		)
	}
}

func TestPaymentCatalogAvailabilityAndPriceSafety(t *testing.T) {
	// Test database setup.
	// Create the MySQL database connection and bootstrap the payment schema.
	// Verify catalog visibility, time windows, localization fallback, and prices.
	env := setupPaymentIntegrationTest(t)

	// Product availability guards.
	// Create unavailable products for every visibility and availability state.
	// Verify hidden, closed, future, and expired products cannot be fetched.
	now := time.Now()
	unavailable := []struct {
		name string
		opt  testProductOptions
	}{
		{name: "hidden", opt: testProductOptions{IsHidden: true}},
		{name: "closed", opt: testProductOptions{IsVisible: true, IsClosed: true}},
		{name: "future", opt: testProductOptions{IsVisible: true, AvailableFrom: now.Add(time.Hour), AvailableUntil: now.Add(2 * time.Hour)}},
		{name: "expired", opt: testProductOptions{IsVisible: true, AvailableFrom: now.Add(-2 * time.Hour), AvailableUntil: now.Add(-time.Hour)}},
	}
	for _, tc := range unavailable {
		productID := createPaymentProduct(t, env, mergeProductOptions(testProductOptions{
			AssetCode:       "RUB",
			ListAmountMinor: 1000,
		}, tc.opt))
		if _, err := env.api.User.GetProduct(env.ctx, product.GetParams{
			Identity:  paymentTestIdentity(testWorkspaceID, 4200, 1, "buyer"),
			ProductID: productID,
			AssetCode: "RUB",
			Locale:    "ru",
		}); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("%s product should be unavailable, got %v", tc.name, err)
		}
	}

	// Price selection and localization fallback.
	// Create active regular, active promo, and expired prices for one product.
	// Verify the current promo price wins and missing locale falls back to keys.
	productID := createPaymentProduct(t, env, testProductOptions{
		AssetCode:       "RUB",
		ListAmountMinor: 1000,
	})
	priceStartsAt := now.Add(-time.Hour)
	priceEndsAt := now.Add(time.Hour)
	expiredStart := now.Add(-3 * time.Hour)
	expiredEnd := now.Add(-2 * time.Hour)
	if _, err := env.api.Admin.CreateCatalogPrice(env.ctx, product.CreatePriceParams{
		WorkspaceID:         testWorkspaceID,
		ProductID:           productID,
		AssetCode:           "RUB",
		ListAmountMinor:     2000,
		DiscountAmountMinor: 0,
		StartsAt:            &expiredStart,
		EndsAt:              &expiredEnd,
	}); err != nil {
		t.Fatalf("create expired price: %v", err)
	}
	if _, err := env.api.Admin.CreateCatalogPrice(env.ctx, product.CreatePriceParams{
		WorkspaceID:         testWorkspaceID,
		ProductID:           productID,
		AssetCode:           "RUB",
		ListAmountMinor:     900,
		DiscountAmountMinor: 200,
		IsPromotion:         true,
		StartsAt:            &priceStartsAt,
		EndsAt:              &priceEndsAt,
	}); err != nil {
		t.Fatalf("create promotion price: %v", err)
	}

	item, err := env.api.User.GetProduct(env.ctx, product.GetParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 4200, 1, "buyer"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "en",
	})
	if err != nil {
		t.Fatalf("get product with promo price: %v", err)
	}
	if item.Price.PayableAmountMinor != 700 {
		t.Fatalf("expected promo payable amount 700, got %d", item.Price.PayableAmountMinor)
	}
	if item.Description == "" {
		t.Fatal("expected localization fallback to keep a non-empty description")
	}

	// Invalid price guard.
	// Try to create and update prices with discounts greater than the list amount.
	// Verify catalog writes reject underflow-prone price definitions.
	if _, err := env.api.Admin.CreateCatalogPrice(env.ctx, product.CreatePriceParams{
		WorkspaceID:         testWorkspaceID,
		ProductID:           productID,
		AssetCode:           "RUB",
		ListAmountMinor:     100,
		DiscountAmountMinor: 101,
		StartsAt:            &priceStartsAt,
		EndsAt:              &priceEndsAt,
	}); !errors.Is(err, repository.ErrInvalidPrice) {
		t.Fatalf("expected invalid price create rejection, got %v", err)
	}
	if _, err := env.api.Admin.UpdateCatalogPrice(env.ctx, product.UpdatePriceParams{
		ID:                  item.Price.ID,
		WorkspaceID:         testWorkspaceID,
		AssetCode:           "RUB",
		ListAmountMinor:     100,
		DiscountAmountMinor: 101,
		StartsAt:            &priceStartsAt,
		EndsAt:              &priceEndsAt,
	}); !errors.Is(err, repository.ErrInvalidPrice) {
		t.Fatalf("expected invalid price update rejection, got %v", err)
	}
}

func TestPaymentProductCacheConsistency(t *testing.T) {
	// Product cache synchronization.
	// Create a product, mutate localization and price, then rebuild the full workspace cache.
	// Verify Product.Get reflects source data changes through the cache read model.
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		AssetCode:       "RUB",
		ListAmountMinor: 1000,
	})

	item, err := env.api.User.GetProduct(env.ctx, product.GetParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 4600, 1, "cache-user"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("get cached product: %v", err)
	}
	if item.Title != "Security product" {
		t.Fatalf("expected initial cached title, got %q", item.Title)
	}

	if err := env.api.Admin.SaveLocalization(env.ctx, product.UpsertLocalizationParams{
		WorkspaceID:     testWorkspaceID,
		Locale:          "ru",
		LocalizationKey: productID + ".title",
		Value:           "Updated cached product",
	}); err != nil {
		t.Fatalf("update cached product title: %v", err)
	}
	item, err = env.api.User.GetProduct(env.ctx, product.GetParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 4600, 1, "cache-user"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("get product after localization update: %v", err)
	}
	if item.Title != "Updated cached product" {
		t.Fatalf("expected updated cached title, got %q", item.Title)
	}

	priceStart := time.Now().Add(-time.Hour)
	priceEnd := time.Now().Add(time.Hour)
	if _, err := env.api.Admin.UpdateCatalogPrice(env.ctx, product.UpdatePriceParams{
		ID:                  item.Price.ID,
		WorkspaceID:         testWorkspaceID,
		AssetCode:           "RUB",
		ListAmountMinor:     1500,
		DiscountAmountMinor: 250,
		StartsAt:            &priceStart,
		EndsAt:              &priceEnd,
	}); err != nil {
		t.Fatalf("update cached product price: %v", err)
	}
	item, err = env.api.User.GetProduct(env.ctx, product.GetParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 4600, 1, "cache-user"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("get product after price update: %v", err)
	}
	if item.Price.PayableAmountMinor != 1250 {
		t.Fatalf("expected updated cached price 1250, got %d", item.Price.PayableAmountMinor)
	}

	if _, err := env.db.ExecContext(env.ctx, "DELETE FROM payment_product_cache WHERE workspace_id = $1", testWorkspaceID); err != nil {
		t.Fatalf("clear product cache: %v", err)
	}
	if err := env.client.ResetCache(); err != nil {
		t.Fatalf("clear go product cache: %v", err)
	}
	freshAPI, err := NewWithDatabase(env.ctx, env.db, paymentTestOptions())
	if err != nil {
		t.Fatalf("create fresh payment service: %v", err)
	}
	defer freshAPI.Close()
	if _, err := freshAPI.User.GetProduct(env.ctx, product.GetParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 4600, 1, "cache-user"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected empty cache lookup to miss, got %v", err)
	}
	if err := env.api.Admin.RebuildProductCache(env.ctx, testWorkspaceID); err != nil {
		t.Fatalf("rebuild workspace product cache: %v", err)
	}
	item, err = env.api.User.GetProduct(env.ctx, product.GetParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 4600, 1, "cache-user"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("get product after workspace cache rebuild: %v", err)
	}
	if item.Title != "Updated cached product" || item.Price.PayableAmountMinor != 1250 {
		t.Fatalf("unexpected rebuilt cache product: title=%q price=%d", item.Title, item.Price.PayableAmountMinor)
	}
}

func TestPaymentCheckoutNegativeSecurityCases(t *testing.T) {
	// Test database setup.
	// Create the MySQL database connection and bootstrap the payment schema.
	// Verify checkout rejects mismatch, duplicate, and incompatible provider cases.
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		AssetCode:       "RUB",
		ListAmountMinor: 1000,
	})

	// Provider and asset compatibility.
	// Try to create a VKMA attempt for a RUB order.
	// Verify providers cannot charge assets they are not configured to support.
	order, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 4300, 1, "buyer-provider"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create provider guard order: %v", err)
	}
	badProviderPaymentID := uniquePaymentID("bad-provider")
	if _, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          paymentAttemptIdentity(order),
		OrderID:           order.ID,
		ProviderCode:      "vkma",
		ProviderPaymentID: &badProviderPaymentID,
	}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected provider asset rejection, got %v", err)
	}

	// Completion mismatch guard.
	// Complete the same attempt with wrong provider, amount, asset, and payment id values.
	// Verify every mismatch is rejected before fulfillment can be created.
	goodProviderPaymentID := uniquePaymentID("good")
	attempt, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          paymentAttemptIdentity(order),
		OrderID:           order.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &goodProviderPaymentID,
	})
	if err != nil {
		t.Fatalf("create mismatch attempt: %v", err)
	}
	wrongPaymentID := goodProviderPaymentID + "_wrong"
	mismatchCases := []struct {
		name   string
		params checkout.CompleteAttemptParams
	}{
		{
			name: "provider",
			params: checkout.CompleteAttemptParams{
				WorkspaceID:       testWorkspaceID,
				AttemptID:         attempt.ID,
				ProviderCode:      "platega",
				ProviderPaymentID: &goodProviderPaymentID,
				AmountMinor:       attempt.AmountMinor,
				AssetCode:         "RUB",
			},
		},
		{
			name: "payment_id",
			params: checkout.CompleteAttemptParams{
				WorkspaceID:       testWorkspaceID,
				AttemptID:         attempt.ID,
				ProviderCode:      "yookassa",
				ProviderPaymentID: &wrongPaymentID,
				AmountMinor:       attempt.AmountMinor,
				AssetCode:         "RUB",
			},
		},
		{
			name: "missing_payment_id",
			params: checkout.CompleteAttemptParams{
				WorkspaceID:  testWorkspaceID,
				AttemptID:    attempt.ID,
				ProviderCode: "yookassa",
				AmountMinor:  attempt.AmountMinor,
				AssetCode:    "RUB",
			},
		},
		{
			name: "amount",
			params: checkout.CompleteAttemptParams{
				WorkspaceID:       testWorkspaceID,
				AttemptID:         attempt.ID,
				ProviderCode:      "yookassa",
				ProviderPaymentID: &goodProviderPaymentID,
				AmountMinor:       attempt.AmountMinor + 1,
				AssetCode:         "RUB",
			},
		},
		{
			name: "asset",
			params: checkout.CompleteAttemptParams{
				WorkspaceID:       testWorkspaceID,
				AttemptID:         attempt.ID,
				ProviderCode:      "yookassa",
				ProviderPaymentID: &goodProviderPaymentID,
				AmountMinor:       attempt.AmountMinor,
				AssetCode:         "VOTE",
			},
		},
	}
	for _, tc := range mismatchCases {
		if _, err := env.api.Operational.CompleteAttempt(env.ctx, tc.params); !errors.Is(err, repository.ErrPaymentMismatch) {
			t.Fatalf("%s mismatch should be rejected, got %v", tc.name, err)
		}
	}

	otherOrder, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 4300, 1, "event-mismatch"),
		ProductID: productID,
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create event mismatch order: %v", err)
	}
	mismatchEventID := uniquePaymentID("event-mismatch")
	if _, err := env.api.Operational.CreateEvent(env.ctx, checkout.CreateEventParams{
		WorkspaceID:     testWorkspaceID,
		ProviderCode:    "yookassa",
		AttemptID:       utils.Ref(int64(attempt.ID)),
		OrderID:         utils.Ref(int64(otherOrder.ID)),
		ProviderEventID: &mismatchEventID,
		EventType:       "succeeded",
		PayloadHash:     sha256Hex(mismatchEventID),
	}); !errors.Is(err, repository.ErrPaymentMismatch) {
		t.Fatalf("expected mismatched attempt and order event to be rejected, got %v", err)
	}

	completed, err := env.api.Operational.CompleteAttempt(env.ctx, checkout.CompleteAttemptParams{
		WorkspaceID:       testWorkspaceID,
		AttemptID:         attempt.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &goodProviderPaymentID,
		AmountMinor:       attempt.AmountMinor,
		AssetCode:         "RUB",
	})
	if err != nil {
		t.Fatalf("complete valid attempt after mismatch checks: %v", err)
	}
	if completed.FulfillmentID == nil {
		t.Fatal("expected fulfillment after valid completion")
	}

	// Duplicate and invalid state guards.
	// Reuse provider ids, event ids, payload hashes, and a fulfilled order.
	// Verify uniqueness and order-state checks stop replay attempts.
	if _, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          paymentAttemptIdentity(order),
		OrderID:           order.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &goodProviderPaymentID,
	}); !errors.Is(err, repository.ErrOrderStateInvalid) {
		t.Fatalf("expected fulfilled order to reject new attempt, got %v", err)
	}

	eventID := uniquePaymentID("event")
	if _, err := env.api.Operational.CreateEvent(env.ctx, checkout.CreateEventParams{
		WorkspaceID:       testWorkspaceID,
		ProviderCode:      "yookassa",
		AttemptID:         utils.Ref(int64(attempt.ID)),
		OrderID:           utils.Ref(int64(order.ID)),
		ProviderEventID:   &eventID,
		ProviderPaymentID: &goodProviderPaymentID,
		EventType:         "succeeded",
		EventStatus:       utils.Ref("succeeded"),
		PayloadHash:       sha256Hex(eventID),
	}); err != nil {
		t.Fatalf("create event: %v", err)
	}
	if _, err := env.api.Operational.CreateEvent(env.ctx, checkout.CreateEventParams{
		WorkspaceID:       testWorkspaceID,
		ProviderCode:      "yookassa",
		AttemptID:         utils.Ref(int64(attempt.ID)),
		OrderID:           utils.Ref(int64(order.ID)),
		ProviderEventID:   &eventID,
		ProviderPaymentID: &goodProviderPaymentID,
		EventType:         "succeeded",
		EventStatus:       utils.Ref("succeeded"),
		PayloadHash:       sha256Hex(eventID + "_different"),
	}); err == nil {
		t.Fatal("expected duplicate provider event id to be rejected")
	}
}

func TestPaymentGiftKeyNegativeCases(t *testing.T) {
	// Test database setup.
	// Create the MySQL database connection and bootstrap the payment schema.
	// Verify gift purchase keys cannot be reused, expired, or guessed.
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		AssetCode:       "RUB",
		ListAmountMinor: 1000,
	})

	// Expired and unknown gift keys.
	// Create an already expired key and also try a random unknown key.
	// Verify neither key can reveal or create a payable gift offer.
	expiredAt := time.Now().Add(-time.Minute)
	expiredKey, err := env.api.Admin.CreateProductKey(env.ctx, product.CreateKeyParams{
		WorkspaceID:    testWorkspaceID,
		AppID:          4400,
		PlatformID:     1,
		PlatformUserID: "recipient-expired",
		ProductID:      productID,
		MaxUses:        1,
		ExpiresAt:      &expiredAt,
	})
	if err != nil {
		t.Fatalf("create expired key: %v", err)
	}
	for _, key := range []string{expiredKey, "not-a-real-key"} {
		if _, err := env.api.User.GetProductByKey(env.ctx, product.GetByKeyParams{Key: key, AssetCode: "RUB", Locale: "ru"}); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("expected key %q to be hidden, got %v", key, err)
		}
		if _, err := env.api.User.CreateOrderByKey(env.ctx, checkout.CreateOrderByKeyParams{
			Key: key,
			Payer: &services.Actor{
				PlatformID:     1,
				PlatformUserID: "payer",
			},
			AssetCode: "RUB",
			Locale:    "ru",
		}); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("expected key %q to reject order creation, got %v", key, err)
		}
	}

	// Gift key max uses.
	// Complete one gift purchase from a max-use-one key and try using it again.
	// Verify used keys cannot create a second payable order.
	key, err := env.api.Admin.CreateProductKey(env.ctx, product.CreateKeyParams{
		WorkspaceID:    testWorkspaceID,
		AppID:          4400,
		PlatformID:     1,
		PlatformUserID: "recipient-once",
		ProductID:      productID,
		MaxUses:        1,
	})
	if err != nil {
		t.Fatalf("create one-use key: %v", err)
	}
	payerPlatformID := int64(1)
	payerUserID := "payer-once"
	order, err := env.api.User.CreateOrderByKey(env.ctx, checkout.CreateOrderByKeyParams{
		Key: key,
		Payer: &services.Actor{
			PlatformID:     payerPlatformID,
			PlatformUserID: payerUserID,
		},
		AssetCode: "RUB",
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create gift order: %v", err)
	}
	if _, err := env.api.User.CreateOrderByKey(env.ctx, checkout.CreateOrderByKeyParams{
		Key: key,
		Payer: &services.Actor{
			PlatformID:     payerPlatformID,
			PlatformUserID: "second-payer-before-completion",
		},
		AssetCode: "RUB",
		Locale:    "ru",
	}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("pending gift order must reserve max_uses: %v", err)
	}

	providerPaymentID := uniquePaymentID("gift-once")
	attempt, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          paymentAttemptIdentity(order),
		OrderID:           order.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &providerPaymentID,
	})
	if err != nil {
		t.Fatalf("create gift attempt: %v", err)
	}
	if _, err := env.api.Operational.CompleteAttempt(env.ctx, checkout.CompleteAttemptParams{
		WorkspaceID:       testWorkspaceID,
		AttemptID:         attempt.ID,
		ProviderCode:      "yookassa",
		ProviderPaymentID: &providerPaymentID,
		AmountMinor:       attempt.AmountMinor,
		AssetCode:         "RUB",
	}); err != nil {
		t.Fatalf("complete gift attempt: %v", err)
	}
	if _, err := env.api.User.CreateOrderByKey(env.ctx, checkout.CreateOrderByKeyParams{
		Key: key,
		Payer: &services.Actor{
			PlatformID:     payerPlatformID,
			PlatformUserID: payerUserID,
		},
		AssetCode: "RUB",
		Locale:    "ru",
	}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected used gift key to reject another order, got %v", err)
	}
}

func createPaymentProduct(t *testing.T, env paymentTestEnv, opt testProductOptions) string {
	t.Helper()

	now := time.Now()
	productID := opt.ProductID
	if productID == "" {
		productID = fmt.Sprintf("secure_product_%d", time.Now().UnixNano())
	}
	groupCode := fmt.Sprintf("secure_group_%d", time.Now().UnixNano())
	itemID := fmt.Sprintf("secure_item_%d", time.Now().UnixNano())
	productTitleKey := productID + ".title"
	productDescriptionKey := productID + ".description"
	itemTitleKey := itemID + ".title"
	itemDescriptionKey := itemID + ".description"
	availableFrom := opt.AvailableFrom
	if availableFrom.IsZero() {
		availableFrom = now.Add(-time.Hour)
	}
	availableUntil := opt.AvailableUntil
	if availableUntil.IsZero() {
		availableUntil = now.Add(time.Hour)
	}
	priceStartsAt := now.Add(-time.Hour)
	priceEndsAt := now.Add(time.Hour)
	assetCode := opt.AssetCode
	if assetCode == "" {
		assetCode = "RUB"
	}
	listAmount := opt.ListAmountMinor
	if listAmount == 0 {
		listAmount = 1000
	}
	globalInterval := opt.GlobalInterval
	if globalInterval == "" {
		globalInterval = "UNLIMITED"
	}
	userInterval := opt.UserInterval
	if userInterval == "" {
		userInterval = "UNLIMITED"
	}
	workspaceID := opt.WorkspaceID
	if workspaceID == "" {
		workspaceID = testWorkspaceID
	}

	if err := env.api.Admin.SaveProductGroup(env.ctx, product.UpsertGroupParams{
		Code:           groupCode,
		WorkspaceID:    workspaceID,
		TitleKey:       utils.Ref(groupCode + ".title"),
		DescriptionKey: utils.Ref(groupCode + ".description"),
		Position:       1,
		IsActive:       true,
	}); err != nil {
		t.Fatalf("upsert test product group: %v", err)
	}
	if err := env.api.Admin.SaveProduct(env.ctx, product.UpsertParams{
		ID:                  productID,
		WorkspaceID:         workspaceID,
		GroupCode:           utils.Ref(groupCode),
		TitleKey:            productTitleKey,
		DescriptionKey:      utils.Ref(productDescriptionKey),
		Position:            1,
		GlobalLimit:         opt.GlobalLimit,
		GlobalInterval:      globalInterval,
		GlobalIntervalCount: opt.GlobalIntervalCount,
		UserLimit:           opt.UserLimit,
		UserInterval:        userInterval,
		UserIntervalCount:   opt.UserIntervalCount,
		AvailableFrom:       &availableFrom,
		AvailableUntil:      &availableUntil,
		IsVisible:           !opt.IsHidden,
		IsClosed:            opt.IsClosed,
	}); err != nil {
		t.Fatalf("create test product: %v", err)
	}
	for _, localization := range []product.UpsertLocalizationParams{
		{Locale: "ru", LocalizationKey: productTitleKey, Value: "Security product"},
		{Locale: "ru", LocalizationKey: productDescriptionKey, Value: "Security product description"},
		{Locale: "ru", LocalizationKey: itemTitleKey, Value: "Security item"},
		{Locale: "ru", LocalizationKey: itemDescriptionKey, Value: "Security item description"},
	} {
		localization.WorkspaceID = workspaceID
		if err := env.api.Admin.SaveLocalization(env.ctx, localization); err != nil {
			t.Fatalf("upsert test localization: %v", err)
		}
	}
	if err := env.api.Admin.AttachProductItem(env.ctx, product.AddItemParams{
		ProductID:   productID,
		WorkspaceID: workspaceID,
		ItemID:      itemID,
		Quantity:    1,
	}); err != nil {
		t.Fatalf("add test item: %v", err)
	}
	if _, err := env.api.Admin.CreateCatalogPrice(env.ctx, product.CreatePriceParams{
		ProductID:           productID,
		WorkspaceID:         workspaceID,
		AssetCode:           assetCode,
		ListAmountMinor:     listAmount,
		DiscountAmountMinor: opt.DiscountAmountMinor,
		StartsAt:            &priceStartsAt,
		EndsAt:              &priceEndsAt,
	}); err != nil {
		t.Fatalf("create test price: %v", err)
	}

	return productID
}

func mergeProductOptions(base testProductOptions, override testProductOptions) testProductOptions {
	if override.AssetCode != "" {
		base.AssetCode = override.AssetCode
	}
	if override.ListAmountMinor != 0 {
		base.ListAmountMinor = override.ListAmountMinor
	}
	if override.DiscountAmountMinor != 0 {
		base.DiscountAmountMinor = override.DiscountAmountMinor
	}
	if override.GlobalLimit != 0 {
		base.GlobalLimit = override.GlobalLimit
	}
	if override.GlobalInterval != "" {
		base.GlobalInterval = override.GlobalInterval
	}
	if override.GlobalIntervalCount != 0 {
		base.GlobalIntervalCount = override.GlobalIntervalCount
	}
	if override.UserLimit != 0 {
		base.UserLimit = override.UserLimit
	}
	if override.UserInterval != "" {
		base.UserInterval = override.UserInterval
	}
	if override.UserIntervalCount != 0 {
		base.UserIntervalCount = override.UserIntervalCount
	}
	if !override.AvailableFrom.IsZero() {
		base.AvailableFrom = override.AvailableFrom
	}
	if !override.AvailableUntil.IsZero() {
		base.AvailableUntil = override.AvailableUntil
	}
	base.IsVisible = override.IsVisible
	base.IsHidden = override.IsHidden
	base.IsClosed = override.IsClosed
	return base
}

func completeTestPayment(t *testing.T, env paymentTestEnv, productID string, prefix string, platformUserID string, providerCode string, assetCode string) {
	t.Helper()

	order, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(testWorkspaceID, 4100, 1, platformUserID),
		ProductID: productID,
		AssetCode: assetCode,
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create order for %s: %v", prefix, err)
	}
	providerPaymentID := uniquePaymentID(prefix)
	attempt, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          paymentAttemptIdentity(order),
		OrderID:           order.ID,
		ProviderCode:      providerCode,
		ProviderPaymentID: &providerPaymentID,
	})
	if err != nil {
		t.Fatalf("create attempt for %s: %v", prefix, err)
	}
	if _, err := env.api.Operational.CompleteAttempt(env.ctx, checkout.CompleteAttemptParams{
		WorkspaceID:       testWorkspaceID,
		AttemptID:         attempt.ID,
		ProviderCode:      providerCode,
		ProviderPaymentID: &providerPaymentID,
		AmountMinor:       attempt.AmountMinor,
		AssetCode:         assetCode,
	}); err != nil {
		t.Fatalf("complete attempt for %s: %v", prefix, err)
	}
}

func TestTelegramStarsAdapterFullCycle(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "telegram_stars_product",
		AssetCode:       telegramstars.AssetCode,
		ListAmountMinor: 25,
	})

	var createInvoicePayload telegramStarsTestCreateInvoicePayload
	var answeredPreCheckout bool
	var refundCalled bool
	var editSubscriptionCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/createInvoiceLink"):
			if err := json.NewDecoder(r.Body).Decode(&createInvoicePayload); err != nil {
				t.Fatalf("decode createInvoiceLink payload: %v", err)
			}
			writeTelegramStarsResult(t, w, "https://t.me/invoice/test-link")
		case strings.HasSuffix(r.URL.Path, "/answerPreCheckoutQuery"):
			answeredPreCheckout = true
			writeTelegramStarsResult(t, w, true)
		case strings.HasSuffix(r.URL.Path, "/refundStarPayment"):
			refundCalled = true
			writeTelegramStarsResult(t, w, true)
		case strings.HasSuffix(r.URL.Path, "/editUserStarSubscription"):
			editSubscriptionCalled = true
			writeTelegramStarsResult(t, w, true)
		default:
			t.Fatalf("unexpected telegram bot api path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	credentials := telegramstars.Credentials{
		BotToken:   "test-token",
		APIBaseURL: server.URL,
	}

	payment, err := env.api.Adapters.TelegramStars.CreatePayment(env.ctx, telegramstars.CreatePaymentParams{
		Credentials:    credentials,
		WorkspaceID:    testWorkspaceID,
		AppID:          7007,
		PlatformID:     2,
		PlatformUserID: "12345",
		ProductID:      productID,
		Locale:         "ru",
		Title:          "Stars product",
		Description:    "Stars product description",
		IdempotencyKey: "telegram-stars-full-cycle",
	})
	if err != nil {
		t.Fatalf("create telegram stars payment: %v", err)
	}
	if payment.InvoiceLink != "https://t.me/invoice/test-link" || payment.AmountMinor != 25 {
		t.Fatalf("unexpected telegram stars payment: %#v", payment)
	}
	if createInvoicePayload.Currency != "XTR" || createInvoicePayload.ProviderToken != "" || len(createInvoicePayload.Prices) != 1 {
		t.Fatalf("unexpected createInvoiceLink payload: %#v", createInvoicePayload)
	}
	if createInvoicePayload.Payload != payment.OrderPublicID || createInvoicePayload.Prices[0].Amount != 25 {
		t.Fatalf("unexpected invoice payload or amount: %#v", createInvoicePayload)
	}
	var storedInvoiceLink sql.NullString
	if err := env.db.QueryRowContext(env.ctx, `
SELECT confirmation_url
FROM payment_attempt
WHERE id = $1`, payment.AttemptID).Scan(&storedInvoiceLink); err != nil {
		t.Fatalf("select telegram stars confirmation url: %v", err)
	}
	if !storedInvoiceLink.Valid || storedInvoiceLink.String != payment.InvoiceLink {
		t.Fatalf("stored telegram stars invoice link = %q, want %q", storedInvoiceLink.String, payment.InvoiceLink)
	}

	preCheckout, err := env.api.Adapters.TelegramStars.HandlePreCheckoutQuery(env.ctx, telegramstars.PreCheckoutQuery{
		Credentials:    credentials,
		WorkspaceID:    testWorkspaceID,
		ID:             "pre-checkout-1",
		FromID:         12345,
		Currency:       telegramstars.AssetCode,
		TotalAmount:    payment.AmountMinor,
		InvoicePayload: payment.OrderPublicID,
	})
	if err != nil {
		t.Fatalf("handle pre-checkout query: %v", err)
	}
	if !answeredPreCheckout || !preCheckout.Accepted {
		t.Fatalf("expected accepted pre-checkout, got %#v", preCheckout)
	}

	success, err := env.api.Adapters.TelegramStars.HandleSuccessfulPayment(env.ctx, telegramstars.SuccessfulPayment{
		WorkspaceID:             testWorkspaceID,
		Currency:                telegramstars.AssetCode,
		TotalAmount:             payment.AmountMinor,
		InvoicePayload:          payment.OrderPublicID,
		TelegramPaymentChargeID: "tg-charge-1",
	})
	if err != nil {
		t.Fatalf("handle successful payment: %v", err)
	}
	if success.OrderID != payment.OrderID || success.AttemptID != payment.AttemptID {
		t.Fatalf("unexpected successful payment result: %#v", success)
	}
	assertOrderStatus(t, env.ctx, env.db, payment.OrderID, "fulfilled")
	assertAttemptStatus(t, env.ctx, env.db, payment.AttemptID, "succeeded")

	var chargeID string
	if err := env.db.QueryRowContext(env.ctx, `
SELECT provider_charge_id
FROM payment_attempt
WHERE id = $1`, payment.AttemptID).Scan(&chargeID); err != nil {
		t.Fatalf("select telegram charge id: %v", err)
	}
	if chargeID != "tg-charge-1" {
		t.Fatalf("unexpected telegram charge id: %s", chargeID)
	}

	refunded, err := env.api.Admin.ExecuteRefund(env.ctx, paymentrefund.Params{
		WorkspaceID:    testWorkspaceID,
		OrderID:        payment.OrderID,
		AttemptID:      payment.AttemptID,
		IdempotencyKey: "telegram-stars-refund-full-cycle",
		Reason:         "test refund",
		ProviderParams: credentials,
	})
	if err != nil {
		t.Fatalf("orchestrate telegram stars refund: %v", err)
	}
	if !refundCalled {
		t.Fatal("expected refundStarPayment call")
	}
	if refunded.OrderID != payment.OrderID || refunded.AttemptID != payment.AttemptID || refunded.Status != "succeeded" {
		t.Fatalf("unexpected orchestrated refund result: %#v", refunded)
	}
	assertOrderStatus(t, env.ctx, env.db, payment.OrderID, "refunded")
	assertAttemptStatus(t, env.ctx, env.db, payment.AttemptID, "refunded")
	assertRefundStatus(t, env.ctx, env.db, refunded.RefundID, "succeeded")

	if err := env.api.Adapters.TelegramStars.EditSubscription(env.ctx, telegramstars.EditSubscriptionParams{
		Credentials:             credentials,
		UserID:                  12345,
		TelegramPaymentChargeID: "tg-charge-1",
		IsCanceled:              true,
	}); err != nil {
		t.Fatalf("edit telegram stars subscription: %v", err)
	}
	if !editSubscriptionCalled {
		t.Fatal("expected editUserStarSubscription call")
	}
}

func TestPaymentRefundRetryReusesExternalIdempotency(t *testing.T) {

	env := setupPaymentIntegrationTest(t)
	fixture := createCompletedPaymentFixture(t, env, 7008, "refund-idempotency")

	providerCalls := 0
	appliedRefunds := make(map[uint64]struct{})
	responseLost := errors.New("provider response lost")
	refundService := paymentrefund.NewWithOptions(
		env.ctx,
		env.client,
		map[string]paymentrefund.ProviderRefundFunc{
			fixture.ProviderCode: func(
				_ context.Context,
				params paymentrefund.ProviderRefundParams,
			) (paymentrefund.ProviderRefundResult, error) {
				providerCalls++
				appliedRefunds[params.RefundID] = struct{}{}
				if providerCalls == 1 {
					return paymentrefund.ProviderRefundResult{}, responseLost
				}

				return paymentrefund.ProviderRefundResult{
					ProviderRefundID: fmt.Sprintf("provider-refund-%d", params.RefundID),
					Status:           "succeeded",
				}, nil
			},
		},
		repository.Options{},
	)
	defer func() { _ = refundService.Close() }()

	baseParams := paymentrefund.Params{
		WorkspaceID:    testWorkspaceID,
		OrderID:        fixture.OrderID,
		AttemptID:      fixture.AttemptID,
		IdempotencyKey: "refund-provider-timeout",
		Reason:         "provider timeout regression",
	}
	missingKey := baseParams
	missingKey.IdempotencyKey = ""
	if _, err := refundService.Execute(env.ctx, missingKey); !errors.Is(err, paymentrefund.ErrIdempotencyKeyRequired) {
		t.Fatalf("missing idempotency error = %v, want %v", err, paymentrefund.ErrIdempotencyKeyRequired)
	}

	if _, err := refundService.Execute(env.ctx, baseParams); !errors.Is(err, responseLost) {
		t.Fatalf("first refund error = %v, want response loss", err)
	}

	var refundID uint64
	var refundStatus string
	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT id, status::text FROM payment_refund WHERE workspace_id = $1 AND idempotency_key = $2",
		testWorkspaceID,
		baseParams.IdempotencyKey,
	).Scan(&refundID, &refundStatus); err != nil {
		t.Fatalf("read pending refund: %v", err)
	}
	if refundStatus != "pending" {
		t.Fatalf("refund after ambiguous provider error = %q, want pending", refundStatus)
	}

	retried, err := refundService.Execute(env.ctx, baseParams)
	if err != nil {
		t.Fatalf("retry refund: %v", err)
	}
	if retried.RefundID != refundID || retried.Status != "succeeded" {
		t.Fatalf("unexpected retried refund: %+v, original id=%d", retried, refundID)
	}
	if providerCalls != 2 || len(appliedRefunds) != 1 {
		t.Fatalf(
			"provider calls=%d unique external operations=%d, want 2 calls and 1 operation",
			providerCalls,
			len(appliedRefunds),
		)
	}

	replayed, err := refundService.Execute(env.ctx, baseParams)
	if err != nil {
		t.Fatalf("replay completed refund: %v", err)
	}
	if replayed.RefundID != refundID || providerCalls != 2 {
		t.Fatalf("completed replay = %+v provider calls=%d", replayed, providerCalls)
	}

}

func TestTelegramStarsGiftRefundUsesPayer(t *testing.T) {

	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "telegram_stars_gift_refund",
		AssetCode:       telegramstars.AssetCode,
		ListAmountMinor: 25,
	})
	recipient := paymentTestIdentity(testWorkspaceID, 7009, 2, "222")
	payer := paymentTestIdentity(testWorkspaceID, 7009, 2, "111")

	order, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity: recipient,
		Payer: &services.Actor{
			PlatformID:     payer.PlatformID,
			PlatformUserID: payer.PlatformUserID,
		},
		ProductID: productID,
		AssetCode: telegramstars.AssetCode,
		Locale:    "ru",
	})
	if err != nil {
		t.Fatalf("create telegram stars gift order: %v", err)
	}

	providerPaymentID := order.PublicID
	attempt, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
		Identity:          payer,
		OrderID:           order.ID,
		ProviderCode:      telegramstars.ProviderCode,
		ProviderPaymentID: &providerPaymentID,
	})
	if err != nil {
		t.Fatalf("create telegram stars gift attempt: %v", err)
	}

	if _, err := env.api.Adapters.TelegramStars.HandleSuccessfulPayment(
		env.ctx,
		telegramstars.SuccessfulPayment{
			WorkspaceID:             testWorkspaceID,
			Currency:                telegramstars.AssetCode,
			TotalAmount:             attempt.AmountMinor,
			InvoicePayload:          order.PublicID,
			TelegramPaymentChargeID: "telegram-gift-charge",
		},
	); err != nil {
		t.Fatalf("complete telegram stars gift: %v", err)
	}

	var refundedUserID int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var body struct {
			UserID int64 `json:"user_id"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode telegram refund: %v", err)
		}
		refundedUserID = body.UserID
		writeTelegramStarsResult(t, w, true)
	}))
	defer server.Close()

	if _, err := env.api.Admin.ExecuteRefund(env.ctx, paymentrefund.Params{
		WorkspaceID:    testWorkspaceID,
		OrderID:        order.ID,
		AttemptID:      attempt.ID,
		IdempotencyKey: "telegram-gift-refund",
		Reason:         "gift refund",
		ProviderParams: telegramstars.Credentials{
			BotToken:   "test-token",
			APIBaseURL: server.URL,
		},
	}); err != nil {
		t.Fatalf("refund telegram stars gift: %v", err)
	}
	if refundedUserID != 111 {
		t.Fatalf("telegram refund user_id = %d, want payer 111", refundedUserID)
	}

}

func assertRefundStatus(t *testing.T, ctx context.Context, db *sql.DB, refundID uint64, want string) {
	t.Helper()
	var got string
	if err := db.QueryRowContext(ctx, "SELECT status FROM payment_refund WHERE id = $1", refundID).Scan(&got); err != nil {
		t.Fatalf("select refund status: %v", err)
	}
	if got != want {
		t.Fatalf("unexpected refund status: got %s want %s", got, want)
	}
}

func TestTelegramStarsAdapterSubscriptionCycle(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "telegram_stars_subscription",
		AssetCode:       telegramstars.AssetCode,
		ListAmountMinor: 50,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTelegramStarsResult(t, w, "https://t.me/invoice/subscription-link")
	}))
	defer server.Close()

	credentials := telegramstars.Credentials{
		BotToken:   "test-token",
		APIBaseURL: server.URL,
	}

	payment, err := env.api.Adapters.TelegramStars.CreatePayment(env.ctx, telegramstars.CreatePaymentParams{
		Credentials:        credentials,
		WorkspaceID:        testWorkspaceID,
		AppID:              7007,
		PlatformID:         2,
		PlatformUserID:     "telegram-sub-user",
		ProductID:          productID,
		Locale:             "ru",
		Title:              "Stars subscription",
		Description:        "Stars subscription description",
		IdempotencyKey:     "telegram-stars-subscription",
		SubscriptionPeriod: 30 * 24 * 60 * 60,
	})
	if err != nil {
		t.Fatalf("create telegram stars subscription payment: %v", err)
	}

	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	initial, err := env.api.Adapters.TelegramStars.HandleSuccessfulPayment(env.ctx, telegramstars.SuccessfulPayment{
		WorkspaceID:                testWorkspaceID,
		Currency:                   telegramstars.AssetCode,
		TotalAmount:                payment.AmountMinor,
		InvoicePayload:             payment.OrderPublicID,
		TelegramPaymentChargeID:    "tg-sub-charge-1",
		SubscriptionExpirationDate: expiresAt.Unix(),
		IsRecurring:                true,
		IsFirstRecurring:           true,
	})
	if err != nil {
		t.Fatalf("handle successful subscription payment: %v", err)
	}
	if initial.FulfillmentID == nil || initial.RenewalID != nil || initial.AlreadyDone {
		t.Fatalf("unexpected initial subscription result: %+v", initial)
	}

	active, err := env.api.User.IsSubscriptionActive(env.ctx, subscription.IsActiveParams{
		Identity:     paymentTestIdentity(testWorkspaceID, 7007, 2, "telegram-sub-user"),
		ProductID:    productID,
		ProviderCode: telegramstars.ProviderCode,
	})
	if err != nil {
		t.Fatalf("check telegram stars subscription active: %v", err)
	}
	if !active {
		t.Fatal("expected telegram stars subscription to be active")
	}

	renewedUntil := expiresAt.Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	renewed, err := env.api.Adapters.TelegramStars.HandleSuccessfulPayment(
		env.ctx,
		telegramstars.SuccessfulPayment{
			WorkspaceID:                testWorkspaceID,
			Currency:                   telegramstars.AssetCode,
			TotalAmount:                payment.AmountMinor,
			InvoicePayload:             payment.OrderPublicID,
			TelegramPaymentChargeID:    "tg-sub-charge-2",
			SubscriptionExpirationDate: renewedUntil.Unix(),
			IsRecurring:                true,
			IsFirstRecurring:           false,
		},
	)
	if err != nil {
		t.Fatalf("handle recurring subscription payment: %v", err)
	}
	if renewed.RenewalID == nil || renewed.FulfillmentID != nil || renewed.AlreadyDone {
		t.Fatalf("unexpected recurring result: %+v", renewed)
	}

	var renewalCount int
	var subscriptionCount int
	var renewedCallbackCount int
	var storedAttemptChargeID string
	var storedPeriodEnd time.Time
	if err := env.db.QueryRowContext(env.ctx, `
SELECT
    (SELECT COUNT(*) FROM payment_subscription_renewal WHERE workspace_id = $1),
    (SELECT COUNT(*) FROM payment_subscription WHERE workspace_id = $1 AND provider_code = $2),
    (SELECT COUNT(*) FROM payment_clb_event WHERE workspace_id = $1 AND event_type = $3),
    (SELECT provider_charge_id FROM payment_attempt WHERE id = $4),
    (SELECT ended_at FROM payment_subscription WHERE workspace_id = $1 AND provider_code = $2)
`,
		testWorkspaceID,
		telegramstars.ProviderCode,
		CallbackEventPaymentSubscriptionRenewed,
		payment.AttemptID,
	).Scan(
		&renewalCount,
		&subscriptionCount,
		&renewedCallbackCount,
		&storedAttemptChargeID,
		&storedPeriodEnd,
	); err != nil {
		t.Fatalf("read recurring payment state: %v", err)
	}
	if renewalCount != 1 || subscriptionCount != 1 || renewedCallbackCount != 1 {
		t.Fatalf(
			"recurring counts renewal=%d subscription=%d callback=%d, want 1/1/1",
			renewalCount,
			subscriptionCount,
			renewedCallbackCount,
		)
	}
	if storedAttemptChargeID != "tg-sub-charge-1" {
		t.Fatalf("attempt subscription charge = %q, want initial charge", storedAttemptChargeID)
	}
	if !storedPeriodEnd.Equal(renewedUntil) {
		t.Fatalf("subscription period end = %s, want %s", storedPeriodEnd, renewedUntil)
	}

	replayed, err := env.api.Adapters.TelegramStars.HandleSuccessfulPayment(
		env.ctx,
		telegramstars.SuccessfulPayment{
			WorkspaceID:                testWorkspaceID,
			Currency:                   telegramstars.AssetCode,
			TotalAmount:                payment.AmountMinor,
			InvoicePayload:             payment.OrderPublicID,
			TelegramPaymentChargeID:    "tg-sub-charge-2",
			SubscriptionExpirationDate: renewedUntil.Unix(),
			IsRecurring:                true,
			IsFirstRecurring:           false,
		},
	)
	if err != nil {
		t.Fatalf("replay recurring subscription payment: %v", err)
	}
	if !replayed.AlreadyDone || replayed.RenewalID == nil ||
		*replayed.RenewalID != *renewed.RenewalID {
		t.Fatalf("unexpected recurring replay: %+v", replayed)
	}

	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT COUNT(*) FROM payment_clb_event WHERE workspace_id = $1 AND event_type = $2",
		testWorkspaceID,
		CallbackEventPaymentSubscriptionRenewed,
	).Scan(&renewedCallbackCount); err != nil {
		t.Fatalf("count replayed renewal callbacks: %v", err)
	}
	if renewedCallbackCount != 1 {
		t.Fatalf("renewal callbacks after replay = %d, want 1", renewedCallbackCount)
	}

	callbackCtx, cancel := context.WithCancel(env.ctx)
	defer cancel()
	seenRenewal := false
	err = env.api.OnCallback(
		callbackCtx,
		func(callback Context) error {
			if callback.EventType == CallbackEventPaymentSubscriptionRenewed {
				seenRenewal = true
				if callback.PaymentSubscriptionRenewed == nil || callback.Payload == nil {
					return errors.New("missing typed subscription renewal payload")
				}
				if callback.PaymentSubscriptionRenewed.RenewalID != *renewed.RenewalID ||
					callback.PaymentSubscriptionRenewed.ProviderSubscriptionID != "tg-sub-charge-1" ||
					callback.PaymentSubscriptionRenewed.ProviderChargeID != "tg-sub-charge-2" {
					return fmt.Errorf(
						"unexpected subscription renewal callback: %+v",
						callback.PaymentSubscriptionRenewed,
					)
				}
			}

			if err := callback.Successful(); err != nil {
				return err
			}
			if seenRenewal {
				cancel()
			}

			return nil
		},
		WithCallbackBatchSize(1),
		WithCallbackIdleDelay(time.Millisecond),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("consume subscription renewal callback: %v", err)
	}
	if !seenRenewal {
		t.Fatal("subscription renewal callback was not delivered")
	}
}

type telegramStarsSubscriptionFixture struct {
	payment   telegramstars.CreatePaymentResponse
	expiresAt time.Time
}

func createTelegramStarsSubscriptionFixture(
	t *testing.T,
	env paymentTestEnv,
	suffix string,
) telegramStarsSubscriptionFixture {

	t.Helper()

	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "telegram_subscription_" + suffix,
		AssetCode:       telegramstars.AssetCode,
		ListAmountMinor: 50,
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTelegramStarsResult(t, w, "https://t.me/invoice/subscription-link")
	}))
	t.Cleanup(server.Close)

	payment, err := env.api.Adapters.TelegramStars.CreatePayment(
		env.ctx,
		telegramstars.CreatePaymentParams{
			Credentials: telegramstars.Credentials{
				BotToken:   "test-token",
				APIBaseURL: server.URL,
			},
			WorkspaceID:        testWorkspaceID,
			AppID:              7008,
			PlatformID:         2,
			PlatformUserID:     "telegram-sub-" + suffix,
			ProductID:          productID,
			Locale:             "ru",
			Title:              "Stars subscription",
			Description:        "Stars subscription description",
			IdempotencyKey:     "telegram-subscription-" + suffix,
			SubscriptionPeriod: 30 * 24 * 60 * 60,
		},
	)
	if err != nil {
		t.Fatalf("create telegram stars subscription: %v", err)
	}

	expiresAt := time.Now().UTC().Truncate(time.Second).Add(30 * 24 * time.Hour)
	if _, err := env.api.Adapters.TelegramStars.HandleSuccessfulPayment(
		env.ctx,
		telegramstars.SuccessfulPayment{
			WorkspaceID:                testWorkspaceID,
			Currency:                   telegramstars.AssetCode,
			TotalAmount:                payment.AmountMinor,
			InvoicePayload:             payment.OrderPublicID,
			TelegramPaymentChargeID:    "initial-charge-" + suffix,
			SubscriptionExpirationDate: expiresAt.Unix(),
			IsRecurring:                true,
			IsFirstRecurring:           true,
		},
	); err != nil {
		t.Fatalf("complete initial telegram subscription: %v", err)
	}

	return telegramStarsSubscriptionFixture{
		payment:   *payment,
		expiresAt: expiresAt,
	}

}

func TestTelegramStarsDelayedRenewalDoesNotShortenSubscription(t *testing.T) {

	env := setupPaymentIntegrationTest(t)
	fixture := createTelegramStarsSubscriptionFixture(t, env, "out-of-order")
	newerPeriodEnd := fixture.expiresAt.Add(60 * 24 * time.Hour)
	olderPeriodEnd := fixture.expiresAt.Add(30 * 24 * time.Hour)

	for index, renewal := range []struct {
		chargeID  string
		periodEnd time.Time
	}{
		{chargeID: "renewal-newer", periodEnd: newerPeriodEnd},
		{chargeID: "renewal-delayed", periodEnd: olderPeriodEnd},
	} {
		if _, err := env.api.Adapters.TelegramStars.HandleSuccessfulPayment(
			env.ctx,
			telegramstars.SuccessfulPayment{
				WorkspaceID:                testWorkspaceID,
				Currency:                   telegramstars.AssetCode,
				TotalAmount:                fixture.payment.AmountMinor,
				InvoicePayload:             fixture.payment.OrderPublicID,
				TelegramPaymentChargeID:    renewal.chargeID,
				SubscriptionExpirationDate: renewal.periodEnd.Unix(),
				IsRecurring:                true,
				IsFirstRecurring:           false,
			},
		); err != nil {
			t.Fatalf("process renewal %d: %v", index, err)
		}
	}

	var storedEnd time.Time
	if err := env.db.QueryRowContext(
		env.ctx,
		`SELECT ended_at FROM payment_subscription WHERE workspace_id = $1 AND attempt_id = $2`,
		testWorkspaceID,
		fixture.payment.AttemptID,
	).Scan(&storedEnd); err != nil {
		t.Fatalf("read subscription end: %v", err)
	}
	if !storedEnd.Equal(newerPeriodEnd) {
		t.Fatalf("subscription end = %s, want monotonic %s", storedEnd, newerPeriodEnd)
	}

}

func TestTelegramStarsRenewalChargeCannotChangePeriod(t *testing.T) {

	env := setupPaymentIntegrationTest(t)
	fixture := createTelegramStarsSubscriptionFixture(t, env, "charge-period")
	chargeID := "renewal-single-charge"
	firstPeriodEnd := fixture.expiresAt.Add(30 * 24 * time.Hour)
	secondPeriodEnd := firstPeriodEnd.Add(30 * 24 * time.Hour)

	request := telegramstars.SuccessfulPayment{
		WorkspaceID:                testWorkspaceID,
		Currency:                   telegramstars.AssetCode,
		TotalAmount:                fixture.payment.AmountMinor,
		InvoicePayload:             fixture.payment.OrderPublicID,
		TelegramPaymentChargeID:    chargeID,
		SubscriptionExpirationDate: firstPeriodEnd.Unix(),
		IsRecurring:                true,
		IsFirstRecurring:           false,
	}
	if _, err := env.api.Adapters.TelegramStars.HandleSuccessfulPayment(env.ctx, request); err != nil {
		t.Fatalf("process first renewal: %v", err)
	}

	request.SubscriptionExpirationDate = secondPeriodEnd.Unix()
	if _, err := env.api.Adapters.TelegramStars.HandleSuccessfulPayment(
		env.ctx,
		request,
	); !errors.Is(err, repository.ErrPaymentMismatch) {
		t.Fatalf("changed renewal period error = %v, want ErrPaymentMismatch", err)
	}

	var renewalCount int
	var callbackCount int
	var storedEnd time.Time
	if err := env.db.QueryRowContext(env.ctx, `
SELECT
    (SELECT COUNT(*) FROM payment_subscription_renewal WHERE workspace_id = $1 AND provider_charge_id = $2),
    (SELECT COUNT(*) FROM payment_clb_event WHERE workspace_id = $1 AND event_type = $3),
    (SELECT ended_at FROM payment_subscription WHERE workspace_id = $1 AND attempt_id = $4)
`,
		testWorkspaceID,
		chargeID,
		CallbackEventPaymentSubscriptionRenewed,
		fixture.payment.AttemptID,
	).Scan(&renewalCount, &callbackCount, &storedEnd); err != nil {
		t.Fatalf("read renewal idempotency state: %v", err)
	}
	if renewalCount != 1 || callbackCount != 1 {
		t.Fatalf("renewals/callbacks = %d/%d, want 1/1", renewalCount, callbackCount)
	}
	if !storedEnd.Equal(firstPeriodEnd) {
		t.Fatalf("subscription end = %s, want %s", storedEnd, firstPeriodEnd)
	}

}

func TestTelegramStarsRenewalReplayDoesNotReactivateRefundedSubscription(t *testing.T) {

	env := setupPaymentIntegrationTest(t)
	fixture := createTelegramStarsSubscriptionFixture(t, env, "renewal-replay-refunded")
	periodEnd := fixture.expiresAt.Add(30 * 24 * time.Hour)
	request := telegramstars.SuccessfulPayment{
		WorkspaceID:                testWorkspaceID,
		Currency:                   telegramstars.AssetCode,
		TotalAmount:                fixture.payment.AmountMinor,
		InvoicePayload:             fixture.payment.OrderPublicID,
		TelegramPaymentChargeID:    "renewal-before-refund",
		SubscriptionExpirationDate: periodEnd.Unix(),
		IsRecurring:                true,
		IsFirstRecurring:           false,
	}

	if _, err := env.api.Adapters.TelegramStars.HandleSuccessfulPayment(env.ctx, request); err != nil {
		t.Fatalf("process renewal before refund: %v", err)
	}
	if rows, err := env.api.Admin.UpdateSubscriptionStatus(
		env.ctx,
		admin.SubscriptionStatusUpdateParams{
			WorkspaceID:            testWorkspaceID,
			ProviderCode:           telegramstars.ProviderCode,
			ProviderSubscriptionID: "initial-charge-renewal-replay-refunded",
			Status:                 "refunded",
			EndedAt:                &periodEnd,
		},
	); err != nil || rows != 1 {
		t.Fatalf("mark subscription refunded: rows=%d err=%v", rows, err)
	}

	replayed, err := env.api.Adapters.TelegramStars.HandleSuccessfulPayment(env.ctx, request)
	if err != nil {
		t.Fatalf("replay renewal after refund: %v", err)
	}
	if !replayed.AlreadyDone {
		t.Fatalf("renewal replay result: %+v", replayed)
	}

	var status string
	if err := env.db.QueryRowContext(env.ctx, `
SELECT status::text
FROM payment_subscription
WHERE workspace_id = $1
  AND provider_code = $2
  AND provider_subscription_id = $3
`,
		testWorkspaceID,
		telegramstars.ProviderCode,
		"initial-charge-renewal-replay-refunded",
	).Scan(&status); err != nil {
		t.Fatalf("read subscription after renewal replay: %v", err)
	}
	if status != "refunded" {
		t.Fatalf("subscription status after renewal replay = %q, want refunded", status)
	}

}

func TestTelegramStarsCreatePaymentIsIdempotent(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "telegram_stars_idempotent",
		AssetCode:       telegramstars.AssetCode,
		ListAmountMinor: 25,
	})

	var createCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/createInvoiceLink") {
			t.Fatalf("unexpected telegram bot api path: %s", r.URL.Path)
		}
		createCalls.Add(1)
		writeTelegramStarsResult(t, w, "https://t.me/invoice/idempotent")
	}))
	defer server.Close()

	params := telegramstars.CreatePaymentParams{
		Credentials: telegramstars.Credentials{
			BotToken:   "test-token",
			APIBaseURL: server.URL,
		},
		WorkspaceID:    testWorkspaceID,
		AppID:          7100,
		PlatformID:     2,
		PlatformUserID: "idempotent-user",
		ProductID:      productID,
		Locale:         "ru",
		Title:          "Idempotent stars",
		Description:    "Idempotent stars payment",
		IdempotencyKey: "telegram-stars-idempotent-key",
	}
	first, err := env.api.Adapters.TelegramStars.CreatePayment(env.ctx, params)
	if err != nil {
		t.Fatalf("create first telegram stars payment: %v", err)
	}
	second, err := env.api.Adapters.TelegramStars.CreatePayment(env.ctx, params)
	if err != nil {
		t.Fatalf("replay telegram stars payment: %v", err)
	}
	if first.OrderID != second.OrderID || first.AttemptID != second.AttemptID ||
		first.InvoiceLink != second.InvoiceLink {
		t.Fatalf("idempotent responses differ: first=%+v second=%+v", first, second)
	}
	if createCalls.Load() != 1 {
		t.Fatalf("createInvoiceLink calls = %d, want 1", createCalls.Load())
	}

	var orders, attempts int
	if err := env.db.QueryRowContext(env.ctx, `
		SELECT
			(SELECT COUNT(*) FROM payment_order WHERE workspace_id = $1 AND product_id = $2),
			(SELECT COUNT(*) FROM payment_attempt WHERE workspace_id = $1 AND provider_code = $3)
	`, testWorkspaceID, productID, telegramstars.ProviderCode).Scan(&orders, &attempts); err != nil {
		t.Fatalf("count telegram stars rows: %v", err)
	}
	if orders != 1 || attempts != 1 {
		t.Fatalf("telegram stars rows orders=%d attempts=%d, want 1 and 1", orders, attempts)
	}
}

func TestTelegramStarsDefinitiveCreateFailureReleasesReservation(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:           "telegram_stars_failure_release",
		AssetCode:           telegramstars.AssetCode,
		ListAmountMinor:     25,
		GlobalLimit:         1,
		GlobalInterval:      "ONCE",
		GlobalIntervalCount: 1,
	})

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"invalid invoice"}`))
			return
		}
		writeTelegramStarsResult(t, w, "https://t.me/invoice/retry")
	}))
	defer server.Close()
	credentials := telegramstars.Credentials{BotToken: "test-token", APIBaseURL: server.URL}

	_, err := env.api.Adapters.TelegramStars.CreatePayment(env.ctx, telegramstars.CreatePaymentParams{
		Credentials:    credentials,
		WorkspaceID:    testWorkspaceID,
		AppID:          7200,
		PlatformID:     2,
		PlatformUserID: "failed-user",
		ProductID:      productID,
		IdempotencyKey: "telegram-stars-failed-key",
	})
	if err == nil {
		t.Fatal("definitive telegram stars API error was not returned")
	}

	retry, err := env.api.Adapters.TelegramStars.CreatePayment(env.ctx, telegramstars.CreatePaymentParams{
		Credentials:    credentials,
		WorkspaceID:    testWorkspaceID,
		AppID:          7200,
		PlatformID:     2,
		PlatformUserID: "retry-user",
		ProductID:      productID,
		IdempotencyKey: "telegram-stars-retry-key",
	})
	if err != nil {
		t.Fatalf("create after released reservation: %v", err)
	}
	if retry.OrderID == 0 || retry.AttemptID == 0 {
		t.Fatalf("retry payment: %+v", retry)
	}
}

func TestTelegramStarsPreCheckoutRejectsTerminalAndExpiredOrders(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(testing.TB, *paymentTestEnv, *telegramstars.CreatePaymentResponse)
	}{
		{
			name: "terminal",
			mutate: func(t testing.TB, env *paymentTestEnv, payment *telegramstars.CreatePaymentResponse) {
				t.Helper()
				if _, err := env.db.ExecContext(
					env.ctx,
					"UPDATE payment_attempt SET status = 'canceled' WHERE id = $1",
					payment.AttemptID,
				); err != nil {
					t.Fatalf("cancel telegram stars attempt: %v", err)
				}
				if _, err := env.db.ExecContext(
					env.ctx,
					"UPDATE payment_order SET status = 'canceled' WHERE id = $1",
					payment.OrderID,
				); err != nil {
					t.Fatalf("cancel telegram stars payment: %v", err)
				}
			},
		},
		{
			name: "expired",
			mutate: func(t testing.TB, env *paymentTestEnv, payment *telegramstars.CreatePaymentResponse) {
				t.Helper()
				if _, err := env.db.ExecContext(env.ctx, `
					UPDATE payment_order SET expires_at = now() - INTERVAL '1 minute' WHERE id = $1
				`, payment.OrderID); err != nil {
					t.Fatalf("expire telegram stars payment: %v", err)
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			env := setupPaymentIntegrationTest(t)
			productID := createPaymentProduct(t, env, testProductOptions{
				ProductID:       "telegram_stars_precheckout_" + testCase.name,
				AssetCode:       telegramstars.AssetCode,
				ListAmountMinor: 25,
			})
			var answeredOK bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/createInvoiceLink"):
					writeTelegramStarsResult(t, w, "https://t.me/invoice/"+testCase.name)
				case strings.HasSuffix(r.URL.Path, "/answerPreCheckoutQuery"):
					var request struct {
						OK bool `json:"ok"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Fatalf("decode precheckout answer: %v", err)
					}
					answeredOK = request.OK
					writeTelegramStarsResult(t, w, true)
				default:
					t.Fatalf("unexpected telegram bot api path: %s", r.URL.Path)
				}
			}))
			defer server.Close()
			credentials := telegramstars.Credentials{BotToken: "test-token", APIBaseURL: server.URL}

			payment, err := env.api.Adapters.TelegramStars.CreatePayment(env.ctx, telegramstars.CreatePaymentParams{
				Credentials:    credentials,
				WorkspaceID:    testWorkspaceID,
				AppID:          7300,
				PlatformID:     2,
				PlatformUserID: "precheckout-" + testCase.name,
				ProductID:      productID,
				IdempotencyKey: "telegram-stars-precheckout-" + testCase.name,
			})
			if err != nil {
				t.Fatalf("create telegram stars payment: %v", err)
			}
			testCase.mutate(t, &env, payment)

			result, err := env.api.Adapters.TelegramStars.HandlePreCheckoutQuery(
				env.ctx,
				telegramstars.PreCheckoutQuery{
					Credentials:    credentials,
					WorkspaceID:    testWorkspaceID,
					ID:             "precheckout-" + testCase.name,
					Currency:       telegramstars.AssetCode,
					TotalAmount:    payment.AmountMinor,
					InvoicePayload: payment.OrderPublicID,
				},
			)
			if err != nil {
				t.Fatalf("handle rejected precheckout: %v", err)
			}
			if result.Accepted || answeredOK {
				t.Fatalf("precheckout accepted terminal/expired payment: result=%+v answer_ok=%t", result, answeredOK)
			}
		})
	}
}

func TestTelegramStarsAcceptedPreCheckoutProtectsStaleOrder(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "telegram_stars_precheckout_protection",
		AssetCode:       telegramstars.AssetCode,
		ListAmountMinor: 25,
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/createInvoiceLink"):
			writeTelegramStarsResult(t, w, "https://t.me/invoice/protected")
		case strings.HasSuffix(r.URL.Path, "/answerPreCheckoutQuery"):
			writeTelegramStarsResult(t, w, true)
		default:
			t.Fatalf("unexpected telegram bot api path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	credentials := telegramstars.Credentials{BotToken: "test-token", APIBaseURL: server.URL}
	payment, err := env.api.Adapters.TelegramStars.CreatePayment(env.ctx, telegramstars.CreatePaymentParams{
		Credentials:    credentials,
		WorkspaceID:    testWorkspaceID,
		AppID:          7400,
		PlatformID:     2,
		PlatformUserID: "protected-precheckout",
		ProductID:      productID,
		IdempotencyKey: "telegram-stars-protected-precheckout",
	})
	if err != nil {
		t.Fatalf("create telegram stars payment: %v", err)
	}
	if _, err := env.db.ExecContext(env.ctx, `
		UPDATE payment_order SET created_at = now() - INTERVAL '2 hours' WHERE id = $1
	`, payment.OrderID); err != nil {
		t.Fatalf("age telegram stars order: %v", err)
	}
	if _, err := env.db.ExecContext(env.ctx, `
		UPDATE payment_attempt SET updated_at = now() - INTERVAL '20 minutes' WHERE id = $1
	`, payment.AttemptID); err != nil {
		t.Fatalf("age telegram stars attempt: %v", err)
	}

	preCheckout, err := env.api.Adapters.TelegramStars.HandlePreCheckoutQuery(
		env.ctx,
		telegramstars.PreCheckoutQuery{
			Credentials:    credentials,
			WorkspaceID:    testWorkspaceID,
			ID:             "protected-precheckout-query",
			Currency:       telegramstars.AssetCode,
			TotalAmount:    payment.AmountMinor,
			InvoicePayload: payment.OrderPublicID,
		},
	)
	if err != nil || !preCheckout.Accepted {
		t.Fatalf("accept precheckout: result=%+v err=%v", preCheckout, err)
	}
	if err := env.api.expireStaleOrders(env.ctx); err != nil {
		t.Fatalf("expire stale orders after precheckout: %v", err)
	}
	assertOrderStatus(t, env.ctx, env.db, payment.OrderID, "pending_payment")
	assertAttemptStatus(t, env.ctx, env.db, payment.AttemptID, "pending")

	if _, err := env.api.Adapters.TelegramStars.HandleSuccessfulPayment(
		env.ctx,
		telegramstars.SuccessfulPayment{
			WorkspaceID:             testWorkspaceID,
			Currency:                telegramstars.AssetCode,
			TotalAmount:             payment.AmountMinor,
			InvoicePayload:          payment.OrderPublicID,
			TelegramPaymentChargeID: "protected-precheckout-charge",
		},
	); err != nil {
		t.Fatalf("complete protected telegram stars payment: %v", err)
	}
	assertOrderStatus(t, env.ctx, env.db, payment.OrderID, "fulfilled")
}

func TestTelegramStarsStalePendingAttemptExpiresWithOrder(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "telegram_stars_stale_attempt",
		AssetCode:       telegramstars.AssetCode,
		ListAmountMinor: 25,
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTelegramStarsResult(t, w, "https://t.me/invoice/stale")
	}))
	defer server.Close()
	payment, err := env.api.Adapters.TelegramStars.CreatePayment(env.ctx, telegramstars.CreatePaymentParams{
		Credentials: telegramstars.Credentials{
			BotToken:   "test-token",
			APIBaseURL: server.URL,
		},
		WorkspaceID:    testWorkspaceID,
		AppID:          7500,
		PlatformID:     2,
		PlatformUserID: "stale-telegram-stars",
		ProductID:      productID,
		IdempotencyKey: "telegram-stars-stale-attempt",
	})
	if err != nil {
		t.Fatalf("create stale telegram stars payment: %v", err)
	}
	if _, err := env.db.ExecContext(env.ctx, `
		UPDATE payment_order SET created_at = now() - INTERVAL '2 hours' WHERE id = $1
	`, payment.OrderID); err != nil {
		t.Fatalf("age telegram stars order: %v", err)
	}
	if _, err := env.db.ExecContext(env.ctx, `
		UPDATE payment_attempt SET updated_at = now() - INTERVAL '20 minutes' WHERE id = $1
	`, payment.AttemptID); err != nil {
		t.Fatalf("age telegram stars attempt: %v", err)
	}

	if err := env.api.expireStaleOrders(env.ctx); err != nil {
		t.Fatalf("expire stale telegram stars payment: %v", err)
	}
	assertOrderStatus(t, env.ctx, env.db, payment.OrderID, "expired")
	assertAttemptStatus(t, env.ctx, env.db, payment.AttemptID, "expired")
}

func TestTelegramStarsNilReceiverReturnsNotInitialized(t *testing.T) {
	var adapter *telegramstars.TelegramStars
	ctx := context.Background()

	if _, err := adapter.CreatePayment(ctx, telegramstars.CreatePaymentParams{}); !errors.Is(err, telegramstars.ErrNotInitialized) {
		t.Fatalf("create payment error = %v", err)
	}
	if _, err := adapter.HandlePreCheckoutQuery(ctx, telegramstars.PreCheckoutQuery{}); !errors.Is(err, telegramstars.ErrNotInitialized) {
		t.Fatalf("precheckout error = %v", err)
	}
	if _, err := adapter.HandleSuccessfulPayment(ctx, telegramstars.SuccessfulPayment{}); !errors.Is(err, telegramstars.ErrNotInitialized) {
		t.Fatalf("successful payment error = %v", err)
	}
	if _, err := adapter.Execute(ctx, telegramstars.RefundParams{}); !errors.Is(err, telegramstars.ErrNotInitialized) {
		t.Fatalf("refund error = %v", err)
	}
	if err := adapter.EditSubscription(ctx, telegramstars.EditSubscriptionParams{}); !errors.Is(err, telegramstars.ErrNotInitialized) {
		t.Fatalf("edit subscription error = %v", err)
	}
}

type telegramStarsTestCreateInvoicePayload struct {
	Title              string                       `json:"title"`
	Description        string                       `json:"description"`
	Payload            string                       `json:"payload"`
	ProviderToken      string                       `json:"provider_token"`
	Currency           string                       `json:"currency"`
	Prices             []telegramstars.LabeledPrice `json:"prices"`
	SubscriptionPeriod int                          `json:"subscription_period"`
}

func writeTelegramStarsResult(t *testing.T, w http.ResponseWriter, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"ok":     true,
		"result": result,
	}); err != nil {
		t.Fatalf("write telegram stars response: %v", err)
	}
}

func TestTONAdapterFullCycleAndCursor(t *testing.T) {

	// Test database setup.
	// Create a TON-priced product and a blockchain payment request.
	// Verify the adapter stores a pending attempt with the order public id as comment.
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "ton_product",
		AssetCode:       paymentton.AssetTON,
		ListAmountMinor: 1_000_000_000,
	})
	configureTONWallet(t, env, testWorkspaceID, paymentton.NetworkMainnet, "EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c")

	payment, err := env.api.Adapters.TON.CreatePayment(env.ctx, paymentton.CreatePaymentParams{
		WorkspaceID:    testWorkspaceID,
		AppID:          6006,
		PlatformID:     1,
		PlatformUserID: "buyer-ton",
		ProductID:      productID,
		AssetCode:      paymentton.AssetTON,
		Locale:         "ru",
	})
	if err != nil {
		t.Fatalf("create ton payment: %v", err)
	}
	if payment.Comment == "" || payment.AmountMinor != 1_000_000_000 {
		t.Fatalf("unexpected ton payment response: %#v", payment)
	}
	tx, err := env.api.Adapters.TON.CreateTransaction(env.ctx, paymentton.CreateTransactionParams{
		AssetCode:   payment.AssetCode,
		Network:     payment.Network,
		Destination: payment.WalletAddress,
		AmountMinor: payment.AmountMinor,
		Comment:     payment.Comment,
	})
	if err != nil {
		t.Fatalf("create ton transaction: %v", err)
	}
	if tonkeeper := paymentton.TonkeeperLink(tx); tonkeeper == "" {
		t.Fatalf("expected tonkeeper link for ton transaction: %#v", tx)
	}
	assertOrderStatus(t, env.ctx, env.db, payment.OrderID, "pending_payment")
	assertAttemptStatus(t, env.ctx, env.db, payment.AttemptID, "pending")

	// TON transfer processing.
	// Emulate an incoming TON transfer with the expected comment and amount.
	// Verify the shared checkout completion path fulfills the order and stores LT.
	result, err := env.api.Adapters.TON.ProcessTransfer(env.ctx, paymentton.IncomingTransfer{
		WorkspaceID:        testWorkspaceID,
		Network:            paymentton.NetworkMainnet,
		WalletAddress:      payment.WalletAddress,
		AssetCode:          paymentton.AssetTON,
		TxHash:             "ton_tx_hash_1",
		LogicalTime:        uint64(time.Now().UnixNano()),
		SourceAddress:      "EQ_SOURCE",
		DestinationAddress: payment.WalletAddress,
		AmountMinor:        payment.AmountMinor,
		Comment:            payment.Comment,
	})
	if err != nil {
		t.Fatalf("process ton transfer: %v", err)
	}
	if result.Transaction == 0 || result.OrderID != payment.OrderID || result.AttemptID != payment.AttemptID {
		t.Fatalf("unexpected ton process result: %#v", result)
	}
	assertOrderStatus(t, env.ctx, env.db, payment.OrderID, "fulfilled")
	assertAttemptStatus(t, env.ctx, env.db, payment.AttemptID, "succeeded")

	var lastLT uint64
	if err := env.db.QueryRowContext(env.ctx, `
SELECT cursor_sequence
FROM payment_provider_cursor
WHERE workspace_id = $1
  AND provider_code = 'ton'
  AND network = 'mainnet'
  AND source_key = $2`, testWorkspaceID, payment.WalletAddress).Scan(&lastLT); err != nil {
		t.Fatalf("select ton cursor: %v", err)
	}
	if lastLT == 0 {
		t.Fatal("expected ton cursor last_lt to be stored")
	}

	// TON transfer idempotency.
	// Process the same transaction hash again.
	// Verify duplicate blockchain events do not create another fulfillment.
	again, err := env.api.Adapters.TON.ProcessTransfer(env.ctx, paymentton.IncomingTransfer{
		WorkspaceID:        testWorkspaceID,
		Network:            paymentton.NetworkMainnet,
		WalletAddress:      payment.WalletAddress,
		AssetCode:          paymentton.AssetTON,
		TxHash:             "ton_tx_hash_1",
		LogicalTime:        lastLT,
		SourceAddress:      "EQ_SOURCE",
		DestinationAddress: payment.WalletAddress,
		AmountMinor:        payment.AmountMinor,
		Comment:            payment.Comment,
	})
	if err != nil {
		t.Fatalf("process duplicate ton transfer: %v", err)
	}
	if !again.AlreadyDone || again.Transaction != result.Transaction {
		t.Fatalf("expected duplicate ton transaction to be idempotent: %#v", again)
	}

	if _, err := env.api.Adapters.TON.ProcessTransfer(env.ctx, paymentton.IncomingTransfer{
		WorkspaceID:        testWorkspaceID,
		Network:            paymentton.NetworkMainnet,
		WalletAddress:      payment.WalletAddress,
		AssetCode:          paymentton.AssetTON,
		TxHash:             "ton_tx_hash_1",
		LogicalTime:        lastLT,
		SourceAddress:      "EQ_SOURCE",
		DestinationAddress: payment.WalletAddress,
		AmountMinor:        payment.AmountMinor + 1,
		Comment:            payment.Comment,
	}); !errors.Is(err, repository.ErrPaymentMismatch) {
		t.Fatalf("changed duplicate transfer error = %v, want ErrPaymentMismatch", err)
	}
}

func TestTONAdapterRetriesTransferAfterLocalCompletionFailure(t *testing.T) {

	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "ton_retry_after_completion_failure",
		AssetCode:       paymentton.AssetTON,
		ListAmountMinor: 1_000_000_000,
	})
	configureTONWallet(
		t,
		env,
		testWorkspaceID,
		paymentton.NetworkMainnet,
		"EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c",
	)

	payment, err := env.api.Adapters.TON.CreatePayment(env.ctx, paymentton.CreatePaymentParams{
		WorkspaceID:    testWorkspaceID,
		AppID:          6007,
		PlatformID:     1,
		PlatformUserID: "buyer-ton-retry",
		ProductID:      productID,
		AssetCode:      paymentton.AssetTON,
		Locale:         "ru",
	})
	if err != nil {
		t.Fatalf("create ton payment: %v", err)
	}

	if _, err := env.db.ExecContext(
		env.ctx,
		"UPDATE payment_order SET status = 'canceled' WHERE id = $1",
		payment.OrderID,
	); err != nil {
		t.Fatalf("temporarily make order unavailable: %v", err)
	}

	transfer := paymentton.IncomingTransfer{
		WorkspaceID:        testWorkspaceID,
		Network:            paymentton.NetworkMainnet,
		WalletAddress:      payment.WalletAddress,
		AssetCode:          paymentton.AssetTON,
		TxHash:             "ton_tx_retry_after_completion_failure",
		LogicalTime:        uint64(time.Now().UnixNano()),
		SourceAddress:      "EQ_SOURCE_RETRY",
		DestinationAddress: payment.WalletAddress,
		AmountMinor:        payment.AmountMinor,
		Comment:            payment.Comment,
	}

	if _, err := env.api.Adapters.TON.ProcessTransfer(env.ctx, transfer); !errors.Is(err, repository.ErrOrderStateInvalid) {
		t.Fatalf("first process error = %v, want %v", err, repository.ErrOrderStateInvalid)
	}

	var storedTransfers int
	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT COUNT(*) FROM payment_provider_transaction WHERE external_transaction_id = $1",
		transfer.TxHash,
	).Scan(&storedTransfers); err != nil {
		t.Fatalf("count prematurely stored transfers: %v", err)
	}
	if storedTransfers != 0 {
		t.Fatalf("stored transfers after failed completion = %d, want 0", storedTransfers)
	}

	if _, err := env.db.ExecContext(
		env.ctx,
		"UPDATE payment_order SET status = 'pending_payment' WHERE id = $1",
		payment.OrderID,
	); err != nil {
		t.Fatalf("restore pending order: %v", err)
	}

	result, err := env.api.Adapters.TON.ProcessTransfer(env.ctx, transfer)
	if err != nil {
		t.Fatalf("retry ton transfer: %v", err)
	}
	if result.AlreadyDone || result.OrderID != payment.OrderID {
		t.Fatalf("unexpected retry result: %#v", result)
	}

	assertOrderStatus(t, env.ctx, env.db, payment.OrderID, "fulfilled")
	assertAttemptStatus(t, env.ctx, env.db, payment.AttemptID, "succeeded")

}

func TestTONAdapterRecoversLegacyFailedMatchingTransfer(t *testing.T) {

	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "ton_legacy_failed_transfer",
		AssetCode:       paymentton.AssetTON,
		ListAmountMinor: 1_000_000_000,
	})
	configureTONWallet(
		t,
		env,
		testWorkspaceID,
		paymentton.NetworkMainnet,
		"EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c",
	)

	payment, err := env.api.Adapters.TON.CreatePayment(env.ctx, paymentton.CreatePaymentParams{
		WorkspaceID:    testWorkspaceID,
		AppID:          6008,
		PlatformID:     1,
		PlatformUserID: "buyer-ton-legacy-failed",
		ProductID:      productID,
		AssetCode:      paymentton.AssetTON,
		Locale:         "ru",
	})
	if err != nil {
		t.Fatalf("create ton payment: %v", err)
	}

	transfer := paymentton.IncomingTransfer{
		WorkspaceID:        testWorkspaceID,
		Network:            paymentton.NetworkMainnet,
		WalletAddress:      payment.WalletAddress,
		AssetCode:          paymentton.AssetTON,
		TxHash:             "ton_legacy_failed_hash",
		LogicalTime:        uint64(time.Now().UnixNano()),
		SourceAddress:      "EQ_SOURCE_LEGACY",
		DestinationAddress: payment.WalletAddress,
		AmountMinor:        payment.AmountMinor,
		Comment:            payment.Comment,
	}

	var transactionID uint64
	if err := env.db.QueryRowContext(env.ctx, `
INSERT INTO payment_provider_transaction (
    workspace_id,
    provider_code,
    network,
    source_key,
    asset_code,
    external_transaction_id,
    sequence_number,
    source_address,
    destination_address,
    amount_minor,
    payment_reference,
    order_id,
    attempt_id,
    status,
    error
)
VALUES ($1, 'ton', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'failed', 'legacy failure')
RETURNING id
`,
		transfer.WorkspaceID,
		transfer.Network,
		transfer.WalletAddress,
		transfer.AssetCode,
		transfer.TxHash,
		transfer.LogicalTime,
		transfer.SourceAddress,
		transfer.DestinationAddress,
		transfer.AmountMinor,
		transfer.Comment,
		payment.OrderID,
		payment.AttemptID,
	).Scan(&transactionID); err != nil {
		t.Fatalf("insert legacy failed transaction: %v", err)
	}

	result, err := env.api.Adapters.TON.ProcessTransfer(env.ctx, transfer)
	if err != nil {
		t.Fatalf("recover failed transfer: %v", err)
	}
	if result.Transaction != transactionID || result.AlreadyDone || result.Ignored {
		t.Fatalf("unexpected recovered transfer: %+v", result)
	}

	assertOrderStatus(t, env.ctx, env.db, payment.OrderID, "fulfilled")
	assertAttemptStatus(t, env.ctx, env.db, payment.AttemptID, "succeeded")

	var status string
	var storedTransactionCount int
	if err := env.db.QueryRowContext(env.ctx, `
SELECT
    (SELECT status::text FROM payment_provider_transaction WHERE id = $1),
    (SELECT COUNT(*) FROM payment_provider_transaction WHERE external_transaction_id = $2)
`, transactionID, transfer.TxHash).Scan(&status, &storedTransactionCount); err != nil {
		t.Fatalf("read recovered transfer: %v", err)
	}
	if status != "matched" || storedTransactionCount != 1 {
		t.Fatalf("recovered status/count = %s/%d, want matched/1", status, storedTransactionCount)
	}

}

func TestTONAdapterJettonTransfer(t *testing.T) {

	// Test database setup.
	// Create a USDT_TON-priced product and payment request.
	// Verify a Jetton transfer is matched by comment and asset code.
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "ton_jetton_product",
		AssetCode:       "USDT_TON",
		ListAmountMinor: 1_000_000,
	})
	configureTONWallet(t, env, testWorkspaceID, paymentton.NetworkMainnet, "EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c")

	payment, err := env.api.Adapters.TON.CreatePayment(env.ctx, paymentton.CreatePaymentParams{
		WorkspaceID:    testWorkspaceID,
		AppID:          6006,
		PlatformID:     1,
		PlatformUserID: "buyer-ton-jetton",
		ProductID:      productID,
		AssetCode:      "USDT_TON",
		Locale:         "ru",
	})
	if err != nil {
		t.Fatalf("create ton jetton payment: %v", err)
	}
	if payment.Decimals != 6 {
		t.Fatalf("expected USDT_TON decimals=6, got %d", payment.Decimals)
	}

	result, err := env.api.Adapters.TON.ProcessTransfer(env.ctx, paymentton.IncomingTransfer{
		WorkspaceID:        testWorkspaceID,
		Network:            paymentton.NetworkMainnet,
		WalletAddress:      payment.WalletAddress,
		AssetCode:          "USDT_TON",
		TxHash:             "ton_jetton_tx_hash_1",
		LogicalTime:        uint64(time.Now().UnixNano()),
		SourceAddress:      "JETTON_WALLET",
		DestinationAddress: payment.WalletAddress,
		AmountMinor:        payment.AmountMinor,
		Comment:            payment.Comment,
		JettonSender:       "EQ_JETTON_SENDER",
	})
	if err != nil {
		t.Fatalf("process ton jetton transfer: %v", err)
	}
	if result.Transaction == 0 || result.OrderID != payment.OrderID {
		t.Fatalf("unexpected ton jetton process result: %#v", result)
	}
	assertOrderStatus(t, env.ctx, env.db, payment.OrderID, "fulfilled")
	assertAttemptStatus(t, env.ctx, env.db, payment.AttemptID, "succeeded")
}

func TestTONAdapterResolvesMultipleJettonAssets(t *testing.T) {
	env := setupPaymentIntegrationTest(t)

	tests := []struct {
		master   string
		code     string
		decimals uint16
	}{
		{
			master:   "EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs",
			code:     "USDT_TON",
			decimals: 6,
		},
		{
			master:   "EQC98_qAmNEptUtPc7W6xdHh_ZHrBUFpw5Ft_IzNU20QAJav",
			code:     "TSTON_TON",
			decimals: 9,
		},
	}

	for _, tt := range tests {
		master, err := address.ParseAddr(tt.master)
		if err != nil {
			t.Fatalf("parse %s master address: %v", tt.code, err)
		}
		resolved, err := env.api.Adapters.TON.ResolveJettonAsset(env.ctx, paymentton.NetworkMainnet, master.StringRaw())
		if err != nil {
			t.Fatalf("resolve %s by raw master address: %v", tt.code, err)
		}
		if resolved.Code != tt.code || resolved.Decimals != tt.decimals {
			t.Fatalf("unexpected resolved asset for %s: %#v", tt.code, resolved)
		}
	}
}

func TestTONAdapterUsesWorkspaceWalletForPayment(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "ton_configured_wallet_product",
		AssetCode:       paymentton.AssetTON,
		ListAmountMinor: 100_000_000,
	})
	rawWallet := "0:0000000000000000000000000000000000000000000000000000000000000000"
	expectedWallet, err := paymentton.NormalizeWalletAddress(rawWallet, paymentton.NetworkMainnet)
	if err != nil {
		t.Fatalf("normalize test wallet: %v", err)
	}
	configureTONWallet(t, env, testWorkspaceID, paymentton.NetworkMainnet, rawWallet)

	payment, err := env.api.Adapters.TON.CreatePayment(env.ctx, paymentton.CreatePaymentParams{
		WorkspaceID:    testWorkspaceID,
		AppID:          6006,
		PlatformID:     1,
		PlatformUserID: "buyer-ton-configured",
		ProductID:      productID,
		AssetCode:      paymentton.AssetTON,
		Locale:         "ru",
	})
	if err != nil {
		t.Fatalf("create ton payment with workspace wallet: %v", err)
	}
	if payment.WalletAddress != expectedWallet {
		t.Fatalf("expected workspace TON wallet in payment response: %#v", payment)
	}
}

func TestTONAdapterRequiresWorkspaceWallet(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "ton_missing_wallet_product",
		AssetCode:       paymentton.AssetTON,
		ListAmountMinor: 100_000_000,
	})

	_, err := env.api.Adapters.TON.CreatePayment(env.ctx, paymentton.CreatePaymentParams{
		WorkspaceID:    testWorkspaceID,
		AppID:          6006,
		PlatformID:     1,
		PlatformUserID: "buyer-ton-missing-wallet",
		ProductID:      productID,
		AssetCode:      paymentton.AssetTON,
		Locale:         "ru",
	})
	if err == nil {
		t.Fatal("expected missing workspace TON wallet to be rejected")
	}
}

func configureTONWallet(t *testing.T, env paymentTestEnv, workspaceID string, network string, wallet string) {
	t.Helper()
	if err := env.api.Admin.SaveTONWallet(env.ctx, admin.TONWalletUpsertParams{
		WorkspaceID:   workspaceID,
		Network:       network,
		WalletAddress: wallet,
		IsEnabled:     true,
	}); err != nil {
		t.Fatalf("configure ton wallet: %v", err)
	}
}

func TestPaymentTONWalletAdminConfig(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	wallet := "UQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAJKZ"
	customConfigURL := "https://example.com/ton.config.json"
	expectedWallet, err := paymentton.NormalizeWalletAddress(wallet, paymentton.NetworkMainnet)
	if err != nil {
		t.Fatalf("normalize wallet: %v", err)
	}

	if err := env.api.Admin.SaveTONWallet(env.ctx, admin.TONWalletUpsertParams{
		WorkspaceID:      testWorkspaceID,
		Network:          paymentton.NetworkMainnet,
		WalletAddress:    wallet,
		NetworkConfigURL: &customConfigURL,
		IsEnabled:        true,
	}); err != nil {
		t.Fatalf("save enabled ton wallet: %v", err)
	}
	got, err := env.api.Admin.GetTONWallet(env.ctx, testWorkspaceID)
	if err != nil {
		t.Fatalf("get ton wallet: %v", err)
	}
	if got.WalletAddress != expectedWallet || !got.IsEnabled || !got.NetworkConfigUrl.Valid || got.NetworkConfigUrl.String != customConfigURL {
		t.Fatalf("unexpected ton wallet: %+v", got)
	}

	if err := env.api.Admin.SaveTONWallet(env.ctx, admin.TONWalletUpsertParams{
		WorkspaceID:   testWorkspaceID,
		Network:       paymentton.NetworkMainnet,
		WalletAddress: wallet,
		IsEnabled:     false,
	}); err != nil {
		t.Fatalf("disable ton wallet: %v", err)
	}
	if err := env.api.Adapters.TON.SyncManagedSubscribers(env.ctx); err != nil {
		t.Fatalf("sync managed subscribers with disabled wallet: %v", err)
	}

	got, err = env.api.Admin.GetTONWallet(env.ctx, testWorkspaceID)
	if err != nil {
		t.Fatalf("get disabled ton wallet: %v", err)
	}
	if got.IsEnabled || got.WalletAddress != expectedWallet {
		t.Fatalf("unexpected disabled ton wallet: %+v", got)
	}

	rows, err := env.api.Admin.DeleteTONWallet(env.ctx, testWorkspaceID)
	if err != nil {
		t.Fatalf("delete ton wallet: %v", err)
	}
	if rows != 1 {
		t.Fatalf("delete ton wallet rows = %d, want 1", rows)
	}

	replacementWallet := "0:1111111111111111111111111111111111111111111111111111111111111111"
	expectedReplacementWallet, err := paymentton.NormalizeWalletAddress(replacementWallet, paymentton.NetworkTestnet)
	if err != nil {
		t.Fatalf("normalize replacement wallet: %v", err)
	}
	if err := env.api.Admin.SaveTONWallet(env.ctx, admin.TONWalletUpsertParams{
		WorkspaceID:   testWorkspaceID,
		Network:       paymentton.NetworkMainnet,
		WalletAddress: wallet,
		IsEnabled:     true,
	}); err != nil {
		t.Fatalf("save first workspace ton wallet: %v", err)
	}
	if err := env.api.Admin.SaveTONWallet(env.ctx, admin.TONWalletUpsertParams{
		WorkspaceID:   testWorkspaceID,
		Network:       paymentton.NetworkTestnet,
		WalletAddress: replacementWallet,
		IsEnabled:     true,
	}); err != nil {
		t.Fatalf("replace workspace ton wallet: %v", err)
	}
	got, err = env.api.Admin.GetTONWallet(env.ctx, testWorkspaceID)
	if err != nil {
		t.Fatalf("get replaced ton wallet: %v", err)
	}
	if got.Network != paymentton.NetworkTestnet || got.WalletAddress != expectedReplacementWallet {
		t.Fatalf("expected replaced workspace ton wallet: %+v", got)
	}
}

func TestVKMAAdapterFullCycleWithSubscription(t *testing.T) {

	// Test database setup.
	// Create the MySQL database connection and bootstrap the payment schema.
	// Verify the VKMA adapter runs against the same initialized payment API.
	env := setupPaymentIntegrationTest(t)
	productID, itemID := createVKMAProduct(t, env)

	// VKMA item lookup.
	// Request product information through the VKMA get_item adapter method.
	// Verify VK receives the localized title, item id, and VOTE price.
	item, err := env.api.Adapters.VKMA.GetItemForWorkspace(env.ctx, testWorkspaceID, vkmashop.Params{
		NotificationType: vkmashop.GetItem,
		AppID:            3003,
		UserID:           8001,
		Item:             productID,
		Lang:             "ru",
	})
	if err != nil {
		t.Fatalf("vkma get item: %v", err)
	}
	if item.ItemID != productID || item.Price != 35 || item.Title != "VKMA подписка" {
		t.Fatalf("unexpected vkma item response: %#v", item)
	}

	// VKMA regular
	// Process a chargeable order_status_change notification as a one-time purchase.
	// Verify the adapter creates and fulfills the payment order.
	orderPaymentID := int(time.Now().UnixNano() % 1_000_000_000)
	regular, err := env.api.Adapters.VKMA.ChargeableForWorkspace(env.ctx, testWorkspaceID, vkmashop.Params{
		NotificationType: vkmashop.OrderStatusChange,
		Status:           vkmashop.Chargeable,
		AppID:            3003,
		UserID:           8001,
		Item:             productID,
		OrderID:          orderPaymentID,
		Lang:             "ru",
	})
	if err != nil {
		t.Fatalf("vkma regular chargeable: %v", err)
	}
	if regular.AppOrderID == 0 || regular.OrderID != orderPaymentID {
		t.Fatalf("unexpected regular vkma response: %#v", regular)
	}
	assertOrderStatus(t, env.ctx, env.db, regular.AppOrderID, "fulfilled")
	if _, err := env.db.ExecContext(env.ctx,
		"UPDATE payment_clb_event SET status = 'ok', delivered_at = now() WHERE source_service = 'payment' AND event_key = $1",
		fmt.Sprintf("%s:%d", CallbackEventPaymentOrderFulfilled, regular.AppOrderID),
	); err != nil {
		t.Fatalf("complete vkma fulfilled callback fixture: %v", err)
	}

	// VK initiates refunds through order_status_change; the application only
	// revokes the fulfilled purchase and acknowledges the notification.
	refundRequest := paymentvkma.Request{
		WorkspaceID: testWorkspaceID,
		Params: vkmashop.Params{
			NotificationType: vkmashop.OrderStatusChange,
			Status:           vkmashop.Refunded,
			AppID:            3003,
			UserID:           8001,
			Item:             productID,
			OrderID:          orderPaymentID,
			Lang:             "ru",
		},
	}
	refundResponse, err := env.api.Adapters.VKMA.HandleRequest(env.ctx, refundRequest)
	if err != nil {
		t.Fatalf("vkma regular refund: %v", err)
	}
	refunded, ok := refundResponse.(*paymentvkma.ChargeableResponse)
	if !ok || refunded.AppOrderID != regular.AppOrderID || refunded.OrderID != orderPaymentID {
		t.Fatalf("unexpected regular vkma refund response: %#v", refundResponse)
	}
	assertOrderStatus(t, env.ctx, env.db, regular.AppOrderID, "refunded")

	var attemptID uint64
	var attemptStatus string
	var fulfillmentID uint64
	var fulfillmentStatus string
	var refundID uint64
	var refundStatus string
	var refundCount int
	if err := env.db.QueryRowContext(env.ctx,
		"SELECT id, status FROM payment_attempt WHERE order_id = $1 AND provider_code = $2",
		regular.AppOrderID, paymentvkma.ProviderCode,
	).Scan(&attemptID, &attemptStatus); err != nil {
		t.Fatalf("select refunded vkma attempt: %v", err)
	}
	if attemptStatus != "refunded" {
		t.Fatalf("unexpected refunded vkma attempt status: %s", attemptStatus)
	}
	if err := env.db.QueryRowContext(env.ctx,
		"SELECT id, status FROM payment_fulfillment WHERE order_id = $1",
		regular.AppOrderID,
	).Scan(&fulfillmentID, &fulfillmentStatus); err != nil {
		t.Fatalf("select revoked vkma fulfillment: %v", err)
	}
	if fulfillmentStatus != "revoked" {
		t.Fatalf("unexpected vkma fulfillment status: %s", fulfillmentStatus)
	}
	if err := env.db.QueryRowContext(env.ctx,
		"SELECT COUNT(*), MAX(id), MAX(status) FROM payment_refund WHERE order_id = $1",
		regular.AppOrderID,
	).Scan(&refundCount, &refundID, &refundStatus); err != nil {
		t.Fatalf("select vkma refund: %v", err)
	}
	if refundCount != 1 || refundStatus != "succeeded" {
		t.Fatalf("unexpected vkma refund state: count=%d status=%s", refundCount, refundStatus)
	}
	productStats, err := env.api.Admin.GetProductStats(env.ctx, testWorkspaceID, productID)
	if err != nil {
		t.Fatalf("get refunded product stats: %v", err)
	}
	if len(productStats.Assets) != 1 || productStats.Assets[0].RefundCount != 1 ||
		productStats.Assets[0].RefundAmountMinor != 35 {
		t.Fatalf("unexpected refunded product stats: %#v", productStats)
	}
	assertVKMARefundedCallback(t, env, PaymentRefundedCallbackPayload{
		OrderID:           regular.AppOrderID,
		AttemptID:         attemptID,
		FulfillmentID:     fulfillmentID,
		RefundID:          refundID,
		WorkspaceID:       testWorkspaceID,
		AppID:             3003,
		PlatformID:        paymentvkma.PlatformID,
		PlatformUserID:    "8001",
		ProductID:         productID,
		Quantity:          1,
		ProviderCode:      paymentvkma.ProviderCode,
		ProviderPaymentID: fmt.Sprint(orderPaymentID),
		AssetCode:         paymentvkma.AssetCode,
		AmountMinor:       35,
		Rewards: []Reward{
			{Key: itemID, Type: "quantity", Quantity: 1},
		},
	})

	if _, err := env.api.Adapters.VKMA.HandleRequest(env.ctx, refundRequest); err != nil {
		t.Fatalf("vkma duplicate regular refund: %v", err)
	}
	if err := env.db.QueryRowContext(env.ctx,
		"SELECT COUNT(*) FROM payment_refund WHERE order_id = $1",
		regular.AppOrderID,
	).Scan(&refundCount); err != nil {
		t.Fatalf("count duplicate vkma refund: %v", err)
	}
	if refundCount != 1 {
		t.Fatalf("expected one idempotent vkma refund, got %d", refundCount)
	}
	productStats, err = env.api.Admin.GetProductStats(env.ctx, testWorkspaceID, productID)
	if err != nil {
		t.Fatalf("get duplicate refunded product stats: %v", err)
	}
	if len(productStats.Assets) != 1 || productStats.Assets[0].RefundCount != 1 {
		t.Fatalf("duplicate vkma refund created extra stats: %#v", productStats.Assets)
	}
	now := time.Now()
	overview, err := env.api.Admin.ListDailyOverview(
		env.ctx, testWorkspaceID, now.Add(-24*time.Hour), now.Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf("list refunded payment daily overview: %v", err)
	}
	if len(overview) != 1 || overview[0].RefundedOrders != 1 || overview[0].RefundCount != 1 {
		t.Fatalf("unexpected refunded payment daily overview: %#v", overview)
	}

	// VKMA subscription lookup.
	// Request subscription product information through get_subscription.
	// Verify VK receives the subscription duration as expiration.
	subscriptionItem, err := env.api.Adapters.VKMA.GetSubscriptionForWorkspace(env.ctx, testWorkspaceID, vkmashop.Params{
		NotificationType: vkmashop.GetSubscription,
		AppID:            3003,
		UserID:           8002,
		Item:             productID,
		Lang:             "ru",
	})
	if err != nil {
		t.Fatalf("vkma get subscription: %v", err)
	}
	if subscriptionItem.Expiration != 2592000 {
		t.Fatalf("unexpected subscription expiration: %d", subscriptionItem.Expiration)
	}

	// VKMA subscription
	// Process a chargeable subscription_status_change notification and repeat it.
	// Verify the subscription is activated and duplicate callbacks stay idempotent.
	subscriptionOrderID := orderPaymentID + 1
	subscriptionID := orderPaymentID + 100
	created, err := env.api.Adapters.VKMA.ChargeableForWorkspace(env.ctx, testWorkspaceID, vkmashop.Params{
		NotificationType: vkmashop.SubscriptionStatusChange,
		Status:           vkmashop.Chargeable,
		AppID:            3003,
		UserID:           8002,
		Item:             productID,
		OrderID:          subscriptionOrderID,
		SubscriptionID:   subscriptionID,
		Lang:             "ru",
	})
	if err != nil {
		t.Fatalf("vkma subscription chargeable: %v", err)
	}
	if created.AppOrderID == 0 || created.OrderID != subscriptionOrderID {
		t.Fatalf("unexpected subscription vkma response: %#v", created)
	}
	assertOrderStatus(t, env.ctx, env.db, created.AppOrderID, "fulfilled")

	again, err := env.api.Adapters.VKMA.ChargeableForWorkspace(env.ctx, testWorkspaceID, vkmashop.Params{
		NotificationType: vkmashop.SubscriptionStatusChange,
		Status:           vkmashop.Chargeable,
		AppID:            3003,
		UserID:           8002,
		Item:             productID,
		OrderID:          subscriptionOrderID,
		SubscriptionID:   subscriptionID,
		Lang:             "ru",
	})
	if err != nil {
		t.Fatalf("vkma subscription chargeable again: %v", err)
	}
	if again.AppOrderID != created.AppOrderID {
		t.Fatalf("expected idempotent app order id: got %d want %d", again.AppOrderID, created.AppOrderID)
	}

	active, err := env.api.User.IsSubscriptionActive(env.ctx, subscription.IsActiveParams{
		Identity:     paymentTestIdentity(testWorkspaceID, 3003, paymentvkma.PlatformID, "8002"),
		ProductID:    productID,
		ProviderCode: paymentvkma.ProviderCode,
	})
	if err != nil {
		t.Fatalf("subscription is active: %v", err)
	}
	if !active {
		t.Fatal("expected active subscription after chargeable")
	}

	wrongWorkspaceID := "00000000-0000-0000-0000-000000000999"
	rows, err := env.api.Admin.UpdateSubscriptionStatus(env.ctx, admin.SubscriptionStatusUpdateParams{
		WorkspaceID:            wrongWorkspaceID,
		ProviderCode:           paymentvkma.ProviderCode,
		ProviderSubscriptionID: strconv.Itoa(subscriptionID),
		Status:                 "canceled",
	})
	if err != nil {
		t.Fatalf("update subscription through wrong workspace: %v", err)
	}
	if rows != 0 {
		t.Fatalf("wrong workspace updated %d subscriptions", rows)
	}
	active, err = env.api.User.IsSubscriptionActive(env.ctx, subscription.IsActiveParams{
		Identity:     paymentTestIdentity(testWorkspaceID, 3003, paymentvkma.PlatformID, "8002"),
		ProductID:    productID,
		ProviderCode: paymentvkma.ProviderCode,
	})
	if err != nil || !active {
		t.Fatalf("wrong workspace changed subscription: active=%t err=%v", active, err)
	}

	// VKMA subscription statuses.
	// Apply active, canceled, and refunded subscription notifications.
	// Verify the shared Subscription API reports active and inactive states correctly.
	if _, err := env.api.Adapters.VKMA.Active(env.ctx, testWorkspaceID, vkmashop.Params{
		NotificationType: vkmashop.SubscriptionStatusChange,
		Status:           vkmashop.Active,
		AppID:            3003,
		UserID:           8002,
		SubscriptionID:   subscriptionID,
		CancelReason:     vkmashop.CancelUserDecision,
	}); err != nil {
		t.Fatalf("vkma subscription active status: %v", err)
	}

	active, err = env.api.User.IsSubscriptionActive(env.ctx, subscription.IsActiveParams{
		Identity:     paymentTestIdentity(testWorkspaceID, 3003, paymentvkma.PlatformID, "8002"),
		ProductID:    productID,
		ProviderCode: paymentvkma.ProviderCode,
	})
	if err != nil {
		t.Fatalf("subscription is active after active status: %v", err)
	}
	if !active {
		t.Fatal("expected active subscription after active status")
	}

	if _, err := env.api.Adapters.VKMA.Canceled(env.ctx, testWorkspaceID, vkmashop.Params{
		NotificationType: vkmashop.SubscriptionStatusChange,
		Status:           vkmashop.Canceled,
		AppID:            3003,
		UserID:           8002,
		SubscriptionID:   subscriptionID,
		CancelReason:     vkmashop.CancelUserDecision,
	}); err != nil {
		t.Fatalf("vkma subscription canceled status: %v", err)
	}

	active, err = env.api.User.IsSubscriptionActive(env.ctx, subscription.IsActiveParams{
		Identity:     paymentTestIdentity(testWorkspaceID, 3003, paymentvkma.PlatformID, "8002"),
		ProductID:    productID,
		ProviderCode: paymentvkma.ProviderCode,
	})
	if err != nil {
		t.Fatalf("subscription is active after cancel: %v", err)
	}
	if active {
		t.Fatal("expected inactive subscription after cancel")
	}

	if _, err := env.api.Adapters.VKMA.Refunded(env.ctx, testWorkspaceID, vkmashop.Params{
		NotificationType: vkmashop.SubscriptionStatusChange,
		Status:           vkmashop.Refunded,
		AppID:            3003,
		UserID:           8002,
		SubscriptionID:   subscriptionID,
	}); err != nil {
		t.Fatalf("vkma subscription refunded status: %v", err)
	}
}

func assertVKMARefundedCallback(t *testing.T, env paymentTestEnv, want PaymentRefundedCallbackPayload) {
	t.Helper()

	ctx, cancel := context.WithCancel(env.ctx)
	handled := 0
	err := env.api.OnCallback(ctx, func(callback Context) error {
		handled++
		if callback.EventType != CallbackEventPaymentOrderRefunded {
			t.Fatalf("unexpected callback event type: %s", callback.EventType)
		}
		if callback.EventKey != fmt.Sprintf("%s:%d", CallbackEventPaymentOrderRefunded, want.OrderID) {
			t.Fatalf("unexpected refunded callback event key: %s", callback.EventKey)
		}
		if callback.PaymentRefunded == nil {
			t.Fatal("expected payment refunded callback payload")
		}
		if got := *callback.PaymentRefunded; !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected payment refunded callback payload: got %#v want %#v", got, want)
		}
		if err := callback.Successful(); err != nil {
			return err
		}
		cancel()
		return nil
	}, WithCallbackBatchSize(1), WithCallbackIdleDelay(time.Millisecond))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("on refunded callback: %v", err)
	}
	if handled != 1 {
		t.Fatalf("unexpected refunded callback handled count: got %d want 1", handled)
	}
}

func TestVKMAAdapterUsesRequestWorkspace(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID, _ := createVKMAProduct(t, env)

	response, err := env.api.Adapters.VKMA.HandleRequest(env.ctx, paymentvkma.Request{
		WorkspaceID: testWorkspaceID,
		Params: vkmashop.Params{
			NotificationType: vkmashop.GetItem,
			AppID:            3003,
			UserID:           8001,
			Item:             productID,
			Lang:             "ru",
		},
	})
	if err != nil {
		t.Fatalf("vkma get item with request workspace: %v", err)
	}
	item, ok := response.(*paymentvkma.ItemResponse)
	if !ok {
		t.Fatalf("unexpected vkma response type: %T", response)
	}
	if item.ItemID != productID || item.Price != 35 {
		t.Fatalf("unexpected vkma item response: %#v", item)
	}
}

func createVKMAProduct(t *testing.T, env paymentTestEnv) (string, string) {
	t.Helper()

	productID := fmt.Sprintf("vkma_product_%d", time.Now().UnixNano())
	groupCode := fmt.Sprintf("vkma_group_%d", time.Now().UnixNano())
	itemID := fmt.Sprintf("vkma_item_%d", time.Now().UnixNano())
	productTitleKey := productID + ".title"
	productDescriptionKey := productID + ".description"
	itemTitleKey := itemID + ".title"
	itemDescriptionKey := itemID + ".description"
	now := time.Now()
	availableFrom := now.Add(-time.Hour)
	availableUntil := now.Add(time.Hour)
	priceStartsAt := now.Add(-time.Hour)
	priceEndsAt := now.Add(time.Hour)
	periodSeconds := int64(2592000)

	if err := env.api.Admin.SaveProductGroup(env.ctx, product.UpsertGroupParams{
		WorkspaceID:    testWorkspaceID,
		Code:           groupCode,
		TitleKey:       utils.Ref(groupCode + ".title"),
		DescriptionKey: utils.Ref(groupCode + ".description"),
		Position:       1,
		IsActive:       true,
	}); err != nil {
		t.Fatalf("upsert vkma product group: %v", err)
	}

	if err := env.api.Admin.SaveProduct(env.ctx, product.UpsertParams{
		WorkspaceID:    testWorkspaceID,
		ID:             productID,
		GroupCode:      utils.Ref(groupCode),
		TitleKey:       productTitleKey,
		DescriptionKey: utils.Ref(productDescriptionKey),
		PeriodSeconds:  &periodSeconds,
		Position:       1,
		GlobalInterval: "UNLIMITED",
		UserInterval:   "UNLIMITED",
		AvailableFrom:  &availableFrom,
		AvailableUntil: &availableUntil,
		IsVisible:      true,
	}); err != nil {
		t.Fatalf("create vkma product: %v", err)
	}

	for _, localization := range []product.UpsertLocalizationParams{
		{WorkspaceID: testWorkspaceID, Locale: "ru", LocalizationKey: productTitleKey, Value: "VKMA подписка"},
		{WorkspaceID: testWorkspaceID, Locale: "ru", LocalizationKey: productDescriptionKey, Value: "VKMA описание"},
		{WorkspaceID: testWorkspaceID, Locale: "ru", LocalizationKey: itemTitleKey, Value: "VKMA premium"},
		{WorkspaceID: testWorkspaceID, Locale: "ru", LocalizationKey: itemDescriptionKey, Value: "VKMA premium description"},
	} {
		if err := env.api.Admin.SaveLocalization(env.ctx, localization); err != nil {
			t.Fatalf("upsert vkma localization: %v", err)
		}
	}

	if err := env.api.Admin.AttachProductItem(env.ctx, product.AddItemParams{
		WorkspaceID: testWorkspaceID,
		ProductID:   productID,
		ItemID:      itemID,
		Quantity:    1,
	}); err != nil {
		t.Fatalf("add vkma product item: %v", err)
	}

	if _, err := env.api.Admin.CreateCatalogPrice(env.ctx, product.CreatePriceParams{
		WorkspaceID:     testWorkspaceID,
		ProductID:       productID,
		AssetCode:       paymentvkma.AssetCode,
		ListAmountMinor: 35,
		StartsAt:        &priceStartsAt,
		EndsAt:          &priceEndsAt,
	}); err != nil {
		t.Fatalf("create vkma product price: %v", err)
	}

	return productID, itemID
}

func TestYooKassaAdapterFullCycle(t *testing.T) {

	// Test database setup.
	// Create a RUB product and configure a fake YooKassa HTTP API.
	// Verify the adapter uses the shared payment database and provider catalog.
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "yookassa_product",
		AssetCode:       yookassa.AssetCode,
		ListAmountMinor: 1299,
	})

	var requestSeen bool
	var refundSeen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/refunds" {
			refundSeen = true
			if r.Header.Get("Idempotence-Key") == "" {
				t.Fatal("expected yookassa refund idempotence key")
			}
			var body struct {
				PaymentID string `json:"payment_id"`
				Amount    struct {
					Value    string `json:"value"`
					Currency string `json:"currency"`
				} `json:"amount"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode yookassa refund: %v", err)
			}
			if body.PaymentID != "yk_pay_1" || body.Amount.Value != "12.99" || body.Amount.Currency != "RUB" {
				t.Fatalf("unexpected yookassa refund body: %#v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id": "yk_refund_1",
				"status": "succeeded",
				"payment_id": "yk_pay_1",
				"amount": {"value": "12.99", "currency": "RUB"}
			}`))
			return
		}
		if r.URL.Path != "/v3/payments" {
			t.Fatalf("unexpected yookassa path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected yookassa method: %s", r.Method)
		}
		shopID, secret, ok := r.BasicAuth()
		if !ok || shopID != "shop_1" || secret != "secret_1" {
			t.Fatalf("unexpected yookassa auth: ok=%v shop=%s secret=%s", ok, shopID, secret)
		}
		if r.Header.Get("Idempotence-Key") != "idem-yookassa-1" {
			t.Fatalf("unexpected idempotence key: %s", r.Header.Get("Idempotence-Key"))
		}

		var body struct {
			Amount struct {
				Value    string `json:"value"`
				Currency string `json:"currency"`
			} `json:"amount"`
			Capture      bool `json:"capture"`
			Confirmation struct {
				Type      string `json:"type"`
				ReturnURL string `json:"return_url"`
			} `json:"confirmation"`
			PaymentMethodData struct {
				Type string `json:"type"`
			} `json:"payment_method_data"`
			Receipt struct {
				Customer struct {
					Email string `json:"email"`
				} `json:"customer"`
				Items []struct {
					Description string `json:"description"`
					Quantity    string `json:"quantity"`
					Amount      struct {
						Value    string `json:"value"`
						Currency string `json:"currency"`
					} `json:"amount"`
					VATCode        int    `json:"vat_code"`
					PaymentMode    string `json:"payment_mode"`
					PaymentSubject string `json:"payment_subject"`
				} `json:"items"`
			} `json:"receipt"`
			Metadata map[string]string `json:"metadata"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode yookassa request: %v", err)
		}
		if body.Amount.Value != "12.99" || body.Amount.Currency != "RUB" {
			t.Fatalf("unexpected yookassa amount: %#v", body.Amount)
		}
		if !body.Capture {
			t.Fatal("expected yookassa capture=true")
		}
		if body.Confirmation.Type != "redirect" || body.Confirmation.ReturnURL != "https://example.com/return" {
			t.Fatalf("unexpected yookassa confirmation: %#v", body.Confirmation)
		}
		if body.PaymentMethodData.Type != string(yookassa.PaymentMethodSBP) {
			t.Fatalf("unexpected yookassa payment method: %#v", body.PaymentMethodData)
		}
		if body.Receipt.Customer.Email != "buyer@example.com" {
			t.Fatalf("unexpected yookassa receipt customer: %#v", body.Receipt.Customer)
		}
		if len(body.Receipt.Items) != 1 || body.Receipt.Items[0].Description != "Elum Love Premium" || body.Receipt.Items[0].Amount.Value != "12.99" {
			t.Fatalf("unexpected yookassa receipt items: %#v", body.Receipt.Items)
		}
		if body.Metadata["product_id"] != productID || body.Metadata["workspace_id"] != testWorkspaceID {
			t.Fatalf("unexpected yookassa metadata: %#v", body.Metadata)
		}

		requestSeen = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "yk_pay_1",
			"status": "pending",
			"paid": false,
			"amount": {"value": "12.99", "currency": "RUB"},
			"confirmation": {
				"type": "redirect",
				"confirmation_url": "https://yookassa.test/confirm/yk_pay_1"
			}
		}`))
	}))
	defer server.Close()

	credentials := yookassa.Credentials{
		ShopID:     "shop_1",
		SecretKey:  "secret_1",
		APIBaseURL: server.URL,
		HTTPClient: server.Client(),
	}

	// YooKassa payment creation.
	// Create a local order and remote YooKassa payment with redirect confirmation.
	// Verify provider identifiers and confirmation URL are stored on the attempt.
	response, err := env.api.Adapters.YooKassa.CreatePayment(env.ctx, yookassa.CreatePaymentParams{
		Credentials:       credentials,
		WorkspaceID:       testWorkspaceID,
		AppID:             5005,
		PlatformID:        1,
		PlatformUserID:    "buyer-yookassa",
		ProductID:         productID,
		Locale:            "ru",
		ReturnURL:         "https://example.com/return",
		IdempotencyKey:    "idem-yookassa-1",
		PaymentMethodType: yookassa.PaymentMethodSBP,
		Description:       "YooKassa adapter test",
		Receipt: &yookassa.Receipt{
			Customer: yookassa.ReceiptCustomer{Email: "buyer@example.com"},
			Items: []yookassa.ReceiptItem{
				{
					Description:    "Elum Love Premium",
					Quantity:       "1.00",
					Amount:         yookassa.Amount{Value: "12.99", Currency: yookassa.AssetCode},
					VATCode:        1,
					PaymentMode:    "full_payment",
					PaymentSubject: "service",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("create yookassa payment: %v", err)
	}
	if !requestSeen {
		t.Fatal("expected fake yookassa API to receive request")
	}
	if response.PaymentID != "yk_pay_1" || response.ConfirmationURL == "" {
		t.Fatalf("unexpected yookassa response: %#v", response)
	}
	if response.AmountMinor != 1299 || response.AssetCode != "RUB" {
		t.Fatalf("unexpected yookassa attempt amount: %#v", response)
	}
	if response.PaymentMethodType != yookassa.PaymentMethodSBP {
		t.Fatalf("unexpected yookassa response payment method: %#v", response)
	}
	assertOrderStatus(t, env.ctx, env.db, response.OrderID, "pending_payment")
	assertAttemptStatus(t, env.ctx, env.db, response.AttemptID, "pending")

	// YooKassa payment webhook.
	// Process payment.succeeded and repeat the same notification.
	// Verify fulfillment is created once and duplicate webhook remains idempotent.
	webhook := []byte(`{
		"type": "notification",
		"event": "payment.succeeded",
		"object": {
			"id": "yk_pay_1",
			"status": "succeeded",
			"paid": true,
			"amount": {"value": "12.99", "currency": "RUB"}
		}
	}`)
	completed, err := env.api.Adapters.YooKassa.HandleWebhook(env.ctx, yookassa.WebhookRequest{
		WorkspaceID:    testWorkspaceID,
		Raw:            webhook,
		SignatureValid: true,
	})
	if err != nil {
		t.Fatalf("handle yookassa webhook: %v", err)
	}
	if completed.FulfilledID == nil {
		t.Fatal("expected yookassa fulfillment id")
	}
	assertOrderStatus(t, env.ctx, env.db, response.OrderID, "fulfilled")
	assertAttemptStatus(t, env.ctx, env.db, response.AttemptID, "succeeded")
	assertFulfillmentItemCount(t, env.ctx, env.db, *completed.FulfilledID, 1)

	again, err := env.api.Adapters.YooKassa.HandleWebhook(env.ctx, yookassa.WebhookRequest{
		WorkspaceID:    testWorkspaceID,
		Raw:            webhook,
		SignatureValid: true,
	})
	if err != nil {
		t.Fatalf("handle yookassa webhook again: %v", err)
	}
	if !again.AlreadyDone {
		t.Fatal("expected duplicate yookassa webhook to be idempotent")
	}

	refunded, err := env.api.Admin.ExecuteRefund(env.ctx, paymentrefund.Params{
		WorkspaceID:    testWorkspaceID,
		OrderID:        response.OrderID,
		AttemptID:      response.AttemptID,
		IdempotencyKey: "yookassa-refund-full-cycle",
		Reason:         "test yookassa refund",
		ProviderParams: credentials,
	})
	if err != nil {
		t.Fatalf("execute yookassa refund: %v", err)
	}
	if !refundSeen {
		t.Fatal("expected yookassa refund request")
	}
	if refunded.ProviderRefundID == nil || *refunded.ProviderRefundID != "yk_refund_1" || refunded.Status != "succeeded" {
		t.Fatalf("unexpected yookassa refund result: %#v", refunded)
	}
	assertOrderStatus(t, env.ctx, env.db, response.OrderID, "refunded")
	assertAttemptStatus(t, env.ctx, env.db, response.AttemptID, "refunded")
	assertRefundStatus(t, env.ctx, env.db, refunded.RefundID, "succeeded")
}

func TestYooKassaAdapterRejectsWrongWebhookAmount(t *testing.T) {

	// Test database setup.
	// Create a YooKassa payment through a fake API.
	// Verify webhook amount mismatch cannot fulfill the order.
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "yookassa_wrong_amount_product",
		AssetCode:       yookassa.AssetCode,
		ListAmountMinor: 500,
		GlobalLimit:     1,
		GlobalInterval:  "ONCE",
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "yk_pay_wrong_amount",
			"status": "pending",
			"paid": false,
			"amount": {"value": "5.00", "currency": "RUB"},
			"confirmation": {
				"type": "redirect",
				"confirmation_url": "https://yookassa.test/confirm/yk_pay_wrong_amount"
			}
		}`))
	}))
	defer server.Close()

	credentials := yookassa.Credentials{
		ShopID:     "shop_1",
		SecretKey:  "secret_1",
		APIBaseURL: server.URL,
		HTTPClient: server.Client(),
	}

	response, err := env.api.Adapters.YooKassa.CreatePayment(env.ctx, yookassa.CreatePaymentParams{
		Credentials:    credentials,
		WorkspaceID:    testWorkspaceID,
		AppID:          5005,
		PlatformID:     1,
		PlatformUserID: "buyer-yookassa-wrong",
		ProductID:      productID,
		Locale:         "ru",
		ReturnURL:      "https://example.com/return",
		IdempotencyKey: "idem-yookassa-wrong-amount-" + time.Now().Format("150405.000000000"),
	})
	if err != nil {
		t.Fatalf("create yookassa payment: %v", err)
	}

	_, err = env.api.Adapters.YooKassa.HandleWebhook(env.ctx, yookassa.WebhookRequest{
		WorkspaceID: testWorkspaceID,
		Raw: []byte(`{
		"type": "notification",
		"event": "payment.succeeded",
		"object": {
			"id": "yk_pay_wrong_amount",
			"status": "succeeded",
			"paid": true,
			"amount": {"value": "5.01", "currency": "RUB"}
		}
	}`),
		SignatureValid: true,
	})
	if err == nil {
		t.Fatal("expected yookassa wrong amount webhook to fail")
	}
	assertOrderStatus(t, env.ctx, env.db, response.OrderID, "pending_payment")

	canceled, err := env.api.Adapters.YooKassa.HandleWebhook(env.ctx, yookassa.WebhookRequest{
		WorkspaceID: testWorkspaceID,
		Raw: []byte(`{
		"type": "notification",
		"event": "payment.canceled",
		"object": {
			"id": "yk_pay_wrong_amount",
			"status": "canceled",
			"paid": false,
			"amount": {"value": "5.00", "currency": "RUB"}
		}
	}`),
		SignatureValid: true,
	})
	if err != nil {
		t.Fatalf("cancel yookassa payment: %v", err)
	}
	if canceled.Status != "canceled" {
		t.Fatalf("canceled status = %q, want canceled", canceled.Status)
	}
	assertOrderStatus(t, env.ctx, env.db, response.OrderID, "canceled")
	assertAttemptStatus(t, env.ctx, env.db, response.AttemptID, "canceled")

	if _, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity: paymentTestIdentity(
			testWorkspaceID,
			5006,
			1,
			"buyer-yookassa-after-cancel",
		),
		ProductID: productID,
		AssetCode: yookassa.AssetCode,
		Locale:    "ru",
	}); err != nil {
		t.Fatalf("create order after yookassa cancellation released global limit: %v", err)
	}
}

func TestYooKassaCreatePaymentIsIdempotentAndRejectsProductMixing(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	firstProductID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "yookassa_idempotent_first",
		AssetCode:       yookassa.AssetCode,
		ListAmountMinor: 100,
	})
	secondProductID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "yookassa_idempotent_second",
		AssetCode:       yookassa.AssetCode,
		ListAmountMinor: 200,
	})

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"yk-idempotent-payment",
			"status":"pending",
			"paid":false,
			"amount":{"value":"1.00","currency":"RUB"},
			"confirmation":{"confirmation_url":"https://example.com/pay"}
		}`))
	}))
	defer server.Close()

	params := yookassa.CreatePaymentParams{
		Credentials: yookassa.Credentials{
			ShopID:     "idempotent-shop",
			SecretKey:  "idempotent-secret",
			APIBaseURL: server.URL,
			HTTPClient: server.Client(),
		},
		WorkspaceID:    testWorkspaceID,
		AppID:          9101,
		PlatformID:     1,
		PlatformUserID: "idempotent-user",
		ProductID:      firstProductID,
		Locale:         "ru",
		ReturnURL:      "https://example.com/return",
		IdempotencyKey: "yookassa-one-order-one-attempt",
	}

	first, err := env.api.Adapters.YooKassa.CreatePayment(env.ctx, params)
	if err != nil {
		t.Fatalf("first create payment: %v", err)
	}
	second, err := env.api.Adapters.YooKassa.CreatePayment(env.ctx, params)
	if err != nil {
		t.Fatalf("repeat create payment: %v", err)
	}
	if first.OrderID != second.OrderID || first.AttemptID != second.AttemptID {
		t.Fatalf("idempotent result changed: first=%#v second=%#v", first, second)
	}
	if calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", calls.Load())
	}

	params.ProductID = secondProductID
	if _, err := env.api.Adapters.YooKassa.CreatePayment(env.ctx, params); !errors.Is(err, repository.ErrPaymentMismatch) {
		t.Fatalf("same key with another product error = %v, want ErrPaymentMismatch", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("provider called for mismatched product: %d", calls.Load())
	}

	var orderCount int
	var attemptCount int
	if err := env.db.QueryRowContext(env.ctx, `
SELECT COUNT(DISTINCT po.id), COUNT(pa.id)
FROM payment_attempt pa
JOIN payment_order po ON po.id = pa.order_id
WHERE pa.workspace_id = $1
  AND pa.provider_code = $2
  AND pa.idempotency_key = $3`, testWorkspaceID, yookassa.ProviderCode, params.IdempotencyKey).Scan(
		&orderCount,
		&attemptCount,
	); err != nil {
		t.Fatalf("count idempotent records: %v", err)
	}
	if orderCount != 1 || attemptCount != 1 {
		t.Fatalf("orders=%d attempts=%d, want 1/1", orderCount, attemptCount)
	}
}

func TestYooKassaProviderIDsAreScopedByWorkspace(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	workspaceA := testsupport.WorkspaceID("yookassa-provider-scope-a")
	workspaceB := testsupport.WorkspaceID("yookassa-provider-scope-b")
	productA := createPaymentProduct(t, env, testProductOptions{
		WorkspaceID:     workspaceA,
		ProductID:       "scoped_product",
		AssetCode:       yookassa.AssetCode,
		ListAmountMinor: 100,
	})
	productB := createPaymentProduct(t, env, testProductOptions{
		WorkspaceID:     workspaceB,
		ProductID:       "scoped_product",
		AssetCode:       yookassa.AssetCode,
		ListAmountMinor: 100,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"same-provider-payment-id",
			"status":"pending",
			"paid":false,
			"amount":{"value":"1.00","currency":"RUB"}
		}`))
	}))
	defer server.Close()
	credentials := yookassa.Credentials{
		ShopID:     "scope-shop",
		SecretKey:  "scope-secret",
		APIBaseURL: server.URL,
		HTTPClient: server.Client(),
	}

	create := func(workspaceID, productID, userID, key string) *yookassa.CreatePaymentResponse {
		t.Helper()
		result, err := env.api.Adapters.YooKassa.CreatePayment(env.ctx, yookassa.CreatePaymentParams{
			Credentials:    credentials,
			WorkspaceID:    workspaceID,
			AppID:          9201,
			PlatformID:     1,
			PlatformUserID: userID,
			ProductID:      productID,
			IdempotencyKey: key,
		})
		if err != nil {
			t.Fatalf("create scoped payment: %v", err)
		}
		return result
	}
	first := create(workspaceA, productA, "scope-user-a", "scope-key-a")
	second := create(workspaceB, productB, "scope-user-b", "scope-key-b")
	if first.AttemptID == second.AttemptID || first.OrderID == second.OrderID {
		t.Fatalf("workspace payments reused local records: first=%#v second=%#v", first, second)
	}

	webhook := []byte(`{
		"type":"notification",
		"event":"payment.succeeded",
		"object":{
			"id":"same-provider-payment-id",
			"status":"succeeded",
			"paid":true,
			"amount":{"value":"1.00","currency":"RUB"}
		}
	}`)
	if _, err := env.api.Adapters.YooKassa.HandleWebhook(env.ctx, yookassa.WebhookRequest{
		WorkspaceID:    workspaceA,
		Raw:            webhook,
		SignatureValid: true,
	}); err != nil {
		t.Fatalf("handle workspace A webhook: %v", err)
	}
	assertOrderStatus(t, env.ctx, env.db, first.OrderID, "fulfilled")
	assertOrderStatus(t, env.ctx, env.db, second.OrderID, "pending_payment")

	if _, err := env.api.Adapters.YooKassa.HandleWebhook(env.ctx, yookassa.WebhookRequest{
		WorkspaceID:    workspaceB,
		Raw:            webhook,
		SignatureValid: true,
	}); err != nil {
		t.Fatalf("handle workspace B webhook: %v", err)
	}
	assertOrderStatus(t, env.ctx, env.db, second.OrderID, "fulfilled")
}

func TestYooKassaConcurrentIdempotencyCreatesOneOrderAndAttempt(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "yookassa_concurrent_idempotency",
		AssetCode:       yookassa.AssetCode,
		ListAmountMinor: 100,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"yk-concurrent-payment",
			"status":"pending",
			"paid":false,
			"amount":{"value":"1.00","currency":"RUB"}
		}`))
	}))
	defer server.Close()

	params := yookassa.CreatePaymentParams{
		Credentials: yookassa.Credentials{
			ShopID:     "concurrent-shop",
			SecretKey:  "concurrent-secret",
			APIBaseURL: server.URL,
			HTTPClient: server.Client(),
		},
		WorkspaceID:    testWorkspaceID,
		AppID:          9501,
		PlatformID:     1,
		PlatformUserID: "concurrent-user",
		ProductID:      productID,
		IdempotencyKey: "yookassa-concurrent-key",
	}

	const workers = 8
	start := make(chan struct{})
	results := make(chan *yookassa.CreatePaymentResponse, workers)
	errorsCh := make(chan error, workers)
	var wait sync.WaitGroup
	wait.Add(workers)
	for range workers {
		go func() {
			defer wait.Done()
			<-start
			result, err := env.api.Adapters.YooKassa.CreatePayment(env.ctx, params)
			if err != nil {
				errorsCh <- err
				return
			}
			results <- result
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		t.Fatalf("concurrent create payment: %v", err)
	}

	var orderID uint64
	var attemptID uint64
	for result := range results {
		if orderID == 0 {
			orderID = result.OrderID
			attemptID = result.AttemptID
			continue
		}
		if result.OrderID != orderID || result.AttemptID != attemptID {
			t.Fatalf("concurrent result changed: %#v", result)
		}
	}

	var orderCount int
	var attemptCount int
	if err := env.db.QueryRowContext(env.ctx, `
SELECT COUNT(DISTINCT po.id), COUNT(pa.id)
FROM payment_attempt pa
JOIN payment_order po ON po.id = pa.order_id
WHERE pa.workspace_id = $1
  AND pa.provider_code = $2
  AND pa.idempotency_key = $3`, testWorkspaceID, yookassa.ProviderCode, params.IdempotencyKey).Scan(
		&orderCount,
		&attemptCount,
	); err != nil {
		t.Fatalf("count concurrent records: %v", err)
	}
	if orderCount != 1 || attemptCount != 1 {
		t.Fatalf("orders=%d attempts=%d, want 1/1", orderCount, attemptCount)
	}
}

func TestYooKassaDefinitiveProviderErrorFailsAttemptAndReleasesOrder(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:           "yookassa_provider_error",
		AssetCode:           yookassa.AssetCode,
		ListAmountMinor:     100,
		GlobalLimit:         1,
		GlobalInterval:      "ONCE",
		GlobalIntervalCount: 1,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"type":"invalid_request"}`, http.StatusBadRequest)
	}))
	defer server.Close()
	key := "yookassa-definitive-error"
	_, err := env.api.Adapters.YooKassa.CreatePayment(env.ctx, yookassa.CreatePaymentParams{
		Credentials: yookassa.Credentials{
			ShopID:     "error-shop",
			SecretKey:  "error-secret",
			APIBaseURL: server.URL,
			HTTPClient: server.Client(),
		},
		WorkspaceID:    testWorkspaceID,
		AppID:          9301,
		PlatformID:     1,
		PlatformUserID: "provider-error-user",
		ProductID:      productID,
		IdempotencyKey: key,
	})
	if err == nil {
		t.Fatal("expected provider error")
	}

	var attemptStatus string
	var orderStatus string
	if err := env.db.QueryRowContext(env.ctx, `
SELECT pa.status::text, po.status::text
FROM payment_attempt pa
JOIN payment_order po ON po.id = pa.order_id
WHERE pa.workspace_id = $1
  AND pa.provider_code = $2
  AND pa.idempotency_key = $3`, testWorkspaceID, yookassa.ProviderCode, key).Scan(
		&attemptStatus,
		&orderStatus,
	); err != nil {
		t.Fatalf("read failed provider attempt: %v", err)
	}
	if attemptStatus != "failed" || orderStatus != "canceled" {
		t.Fatalf("attempt=%s order=%s, want failed/canceled", attemptStatus, orderStatus)
	}

	var reserved int64
	if err := env.db.QueryRowContext(env.ctx, `
SELECT COALESCE(SUM(reserved_count), 0)
FROM payment_product_limit_counter
WHERE workspace_id = $1 AND product_id = $2`, testWorkspaceID, productID).Scan(&reserved); err != nil {
		t.Fatalf("read released reservation: %v", err)
	}
	if reserved != 0 {
		t.Fatalf("reserved count = %d, want 0", reserved)
	}
}

func TestYooKassaAmbiguousProviderErrorRetriesSameAttempt(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "yookassa_ambiguous_retry",
		AssetCode:       yookassa.AssetCode,
		ListAmountMinor: 100,
	})

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, `{"type":"temporary_error"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"yk-recovered-payment",
			"status":"pending",
			"paid":false,
			"amount":{"value":"1.00","currency":"RUB"}
		}`))
	}))
	defer server.Close()

	key := "yookassa-ambiguous-retry"
	params := yookassa.CreatePaymentParams{
		Credentials: yookassa.Credentials{
			ShopID:     "retry-shop",
			SecretKey:  "retry-secret",
			APIBaseURL: server.URL,
			HTTPClient: server.Client(),
		},
		WorkspaceID:    testWorkspaceID,
		AppID:          9302,
		PlatformID:     1,
		PlatformUserID: "ambiguous-retry-user",
		ProductID:      productID,
		IdempotencyKey: key,
	}

	if _, err := env.api.Adapters.YooKassa.CreatePayment(env.ctx, params); err == nil {
		t.Fatal("expected ambiguous provider error")
	}

	recovered, err := env.api.Adapters.YooKassa.CreatePayment(env.ctx, params)
	if err != nil {
		t.Fatalf("retry ambiguous payment: %v", err)
	}
	if recovered.PaymentID != "yk-recovered-payment" {
		t.Fatalf("recovered payment id = %q", recovered.PaymentID)
	}
	if calls.Load() != 2 {
		t.Fatalf("provider calls = %d, want 2", calls.Load())
	}

	var orderCount int
	var attemptCount int
	var attemptStatus string
	if err := env.db.QueryRowContext(env.ctx, `
SELECT COUNT(DISTINCT po.id), COUNT(pa.id), MIN(pa.status::text)
FROM payment_attempt pa
JOIN payment_order po ON po.id = pa.order_id
WHERE pa.workspace_id = $1
  AND pa.provider_code = $2
  AND pa.idempotency_key = $3`, testWorkspaceID, yookassa.ProviderCode, key).Scan(
		&orderCount,
		&attemptCount,
		&attemptStatus,
	); err != nil {
		t.Fatalf("read recovered payment: %v", err)
	}
	if orderCount != 1 || attemptCount != 1 || attemptStatus != "pending" {
		t.Fatalf("orders=%d attempts=%d status=%s, want 1/1/pending", orderCount, attemptCount, attemptStatus)
	}
}

func TestPlategaUnknownCreationIsNotRetried(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "platega_unknown_creation",
		AssetCode:       platega.AssetCode,
		ListAmountMinor: 100,
	})

	var calls atomic.Int32
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("simulated connection loss")
	})}
	params := platega.CreatePaymentParams{
		Credentials: platega.Credentials{
			MerchantID: "unknown-merchant",
			Secret:     "unknown-secret",
			APIBaseURL: "https://platega.invalid",
			HTTPClient: httpClient,
		},
		WorkspaceID:    testWorkspaceID,
		AppID:          9401,
		PlatformID:     1,
		PlatformUserID: "platega-unknown-user",
		ProductID:      productID,
		IdempotencyKey: "platega-unknown-key",
	}
	if _, err := env.api.Adapters.Platega.CreatePayment(env.ctx, params); err == nil {
		t.Fatal("expected connection error")
	}
	if _, err := env.api.Adapters.Platega.CreatePayment(env.ctx, params); !errors.Is(err, platega.ErrTransactionStateUnknown) {
		t.Fatalf("retry error = %v, want ErrTransactionStateUnknown", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("platega calls = %d, want 1", calls.Load())
	}

	var attemptStatus string
	var orderStatus string
	if err := env.db.QueryRowContext(env.ctx, `
SELECT pa.status::text, po.status::text
FROM payment_attempt pa
JOIN payment_order po ON po.id = pa.order_id
WHERE pa.workspace_id = $1
  AND pa.provider_code = $2
  AND pa.idempotency_key = $3`, testWorkspaceID, platega.ProviderCode, params.IdempotencyKey).Scan(
		&attemptStatus,
		&orderStatus,
	); err != nil {
		t.Fatalf("read unknown platega attempt: %v", err)
	}
	if attemptStatus != "created" || orderStatus != "pending_payment" {
		t.Fatalf("attempt=%s order=%s, want created/pending_payment", attemptStatus, orderStatus)
	}
}

func TestPlategaWebhookRecoversLostCreateResponse(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "platega_webhook_recovery",
		AssetCode:       platega.AssetCode,
		ListAmountMinor: 100,
	})

	const transactionID = "platega-lost-response"
	const idempotencyKey = "platega-lost-response-key"
	var getCalls atomic.Int32
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			return nil, errors.New("simulated response loss")
		}
		if request.Method != http.MethodGet || !strings.HasSuffix(request.URL.Path, "/transaction/"+transactionID) {
			return nil, fmt.Errorf("unexpected platega request: %s %s", request.Method, request.URL.Path)
		}
		getCalls.Add(1)

		var orderPublicID string
		if err := env.db.QueryRowContext(env.ctx, `
SELECT po.public_id
FROM payment_attempt pa
JOIN payment_order po ON po.id = pa.order_id
WHERE pa.workspace_id = $1
  AND pa.provider_code = $2
  AND pa.idempotency_key = $3`, testWorkspaceID, platega.ProviderCode, idempotencyKey).Scan(&orderPublicID); err != nil {
			return nil, err
		}
		body := fmt.Sprintf(`{
			"id":%q,
			"status":"CONFIRMED",
			"paymentDetails":{"amount":1,"currency":"RUB"},
			"payload":%q
		}`, transactionID, orderPublicID)

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
	credentials := platega.Credentials{
		MerchantID: "recovery-merchant",
		Secret:     "recovery-secret",
		APIBaseURL: "https://platega.test",
		HTTPClient: httpClient,
	}
	if _, err := env.api.Adapters.Platega.CreatePayment(env.ctx, platega.CreatePaymentParams{
		Credentials:    credentials,
		WorkspaceID:    testWorkspaceID,
		AppID:          9402,
		PlatformID:     1,
		PlatformUserID: "platega-recovery-user",
		ProductID:      productID,
		IdempotencyKey: idempotencyKey,
	}); err == nil {
		t.Fatal("expected lost create response")
	}

	callback := []byte(`{
		"id":"platega-lost-response",
		"amount":1,
		"currency":"RUB",
		"status":"CONFIRMED",
		"paymentMethod":2
	}`)
	headers := make(http.Header)
	headers.Set("X-MerchantId", credentials.MerchantID)
	headers.Set("X-Secret", credentials.Secret)
	request := platega.WebhookRequest{
		Credentials: credentials,
		WorkspaceID: testWorkspaceID,
		Raw:         callback,
		Headers:     headers,
	}
	const workers = 8
	start := make(chan struct{})
	results := make(chan *platega.WebhookResult, workers)
	errorsCh := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			<-start
			result, err := env.api.Adapters.Platega.HandleWebhook(env.ctx, request)
			if err != nil {
				errorsCh <- err
				return
			}
			results <- result
		}()
	}
	close(start)
	group.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		t.Fatalf("concurrent Platega recovery: %v", err)
	}

	var result *platega.WebhookResult
	for current := range results {
		if result == nil {
			result = current
			continue
		}
		if current.OrderID != result.OrderID || current.AttemptID != result.AttemptID {
			t.Fatalf("concurrent recovery mixed records: first=%#v current=%#v", result, current)
		}
	}
	if result == nil {
		t.Fatal("concurrent recovery returned no result")
	}
	if result.FulfilledID == nil {
		t.Fatal("recovered callback did not fulfill order")
	}
	assertOrderStatus(t, env.ctx, env.db, result.OrderID, "fulfilled")
	assertAttemptStatus(t, env.ctx, env.db, result.AttemptID, "succeeded")

	recoveryCalls := getCalls.Load()
	again, err := env.api.Adapters.Platega.HandleWebhook(env.ctx, request)
	if err != nil {
		t.Fatalf("repeat recovered callback: %v", err)
	}
	if !again.AlreadyDone {
		t.Fatal("repeat recovered callback is not idempotent")
	}
	if recoveryCalls == 0 || recoveryCalls > workers {
		t.Fatalf("transaction recovery GET calls = %d, want 1..%d", recoveryCalls, workers)
	}
	if getCalls.Load() != recoveryCalls {
		t.Fatalf("idempotent callback made another recovery GET: before=%d after=%d", recoveryCalls, getCalls.Load())
	}
}

func TestPlategaWebhookRecoveryRejectsWrongWorkspaceAndAmount(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "platega_recovery_guards",
		AssetCode:       platega.AssetCode,
		ListAmountMinor: 100,
	})
	const idempotencyKey = "platega-recovery-guards-key"

	var orderPublicID string
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			return nil, errors.New("simulated response loss")
		}
		body := fmt.Sprintf(`{
			"id":"platega-guard-transaction",
			"status":"CONFIRMED",
			"paymentDetails":{"amount":2,"currency":"RUB"},
			"payload":%q
		}`, orderPublicID)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
	credentials := platega.Credentials{
		MerchantID: "guard-merchant",
		Secret:     "guard-secret",
		APIBaseURL: "https://platega.test",
		HTTPClient: httpClient,
	}
	if _, err := env.api.Adapters.Platega.CreatePayment(env.ctx, platega.CreatePaymentParams{
		Credentials:    credentials,
		WorkspaceID:    testWorkspaceID,
		AppID:          9403,
		PlatformID:     1,
		PlatformUserID: "platega-guard-user",
		ProductID:      productID,
		IdempotencyKey: idempotencyKey,
	}); err == nil {
		t.Fatal("expected lost create response")
	}
	if err := env.db.QueryRowContext(env.ctx, `
SELECT po.public_id
FROM payment_attempt pa
JOIN payment_order po ON po.id = pa.order_id
WHERE pa.idempotency_key = $1`, idempotencyKey).Scan(&orderPublicID); err != nil {
		t.Fatalf("read recovery order: %v", err)
	}

	callback := []byte(`{
		"id":"platega-guard-transaction",
		"amount":2,
		"currency":"RUB",
		"status":"CONFIRMED",
		"paymentMethod":2
	}`)
	headers := make(http.Header)
	headers.Set("X-MerchantId", credentials.MerchantID)
	headers.Set("X-Secret", credentials.Secret)
	request := platega.WebhookRequest{
		Credentials: credentials,
		WorkspaceID: testsupport.WorkspaceID("wrong-platega-workspace"),
		Raw:         callback,
		Headers:     headers,
	}
	if _, err := env.api.Adapters.Platega.HandleWebhook(env.ctx, request); !errors.Is(err, repository.ErrPaymentMismatch) {
		t.Fatalf("wrong workspace recovery error = %v", err)
	}

	request.WorkspaceID = testWorkspaceID
	if _, err := env.api.Adapters.Platega.HandleWebhook(env.ctx, request); !errors.Is(err, repository.ErrPaymentMismatch) {
		t.Fatalf("wrong amount recovery error = %v", err)
	}

	var attemptID uint64
	if err := env.db.QueryRowContext(env.ctx, `
SELECT id FROM payment_attempt WHERE idempotency_key = $1`, idempotencyKey).Scan(&attemptID); err != nil {
		t.Fatalf("read guarded attempt: %v", err)
	}
	assertAttemptStatus(t, env.ctx, env.db, attemptID, "created")
}

func TestPlategaBackgroundReconciliationRecoversWithoutCallback(t *testing.T) {
	const idempotencyKey = "platega-background-recovery-key"
	const transactionID = "platega-background-transaction"
	var database atomic.Pointer[sql.DB]
	var exportCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v2/transaction/process":
			http.Error(w, `{"error":"temporary"}`, http.StatusInternalServerError)
		case "/transaction/export/json":
			exportCalls.Add(1)
			db := database.Load()
			if db == nil {
				_, _ = w.Write([]byte(`[]`))
				return
			}

			var orderPublicID string
			err := db.QueryRowContext(context.Background(), `
SELECT po.public_id
FROM payment_attempt pa
JOIN payment_order po ON po.id = pa.order_id
WHERE pa.provider_code = $1
  AND pa.idempotency_key = $2
  AND pa.status = 'created'`, platega.ProviderCode, idempotencyKey).Scan(&orderPublicID)
			if errors.Is(err, sql.ErrNoRows) {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_, _ = fmt.Fprintf(w, `[{
				"recordId":%q,
				"amount":1,
				"currencyCode":"RUB",
				"status":"CONFIRMED",
				"paymentMethod":"SBPQR",
				"payload":%q
			}]`, transactionID, orderPublicID)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	credentials := platega.Credentials{
		MerchantID: "background-merchant",
		Secret:     "background-secret",
		APIBaseURL: server.URL,
		HTTPClient: server.Client(),
	}
	options := paymentTestOptions()
	options.OrderExpirationInterval = 5 * time.Millisecond
	options.OrderExpirationAge = time.Millisecond
	options.PlategaCredentialsResolver = func(_ context.Context, workspaceID string) (platega.Credentials, error) {
		if workspaceID != testWorkspaceID {
			return platega.Credentials{}, fmt.Errorf("unexpected workspace %s", workspaceID)
		}
		return credentials, nil
	}
	options.PlategaReconcileInterval = 10 * time.Millisecond
	options.PlategaReconcileMinAge = time.Millisecond
	options.PlategaReconcileBatch = 100
	env := setupPaymentIntegrationTestWithOptions(t, options)
	database.Store(env.db)

	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "platega_background_recovery",
		AssetCode:       platega.AssetCode,
		ListAmountMinor: 100,
	})
	if _, err := env.api.Adapters.Platega.CreatePayment(env.ctx, platega.CreatePaymentParams{
		Credentials:    credentials,
		WorkspaceID:    testWorkspaceID,
		AppID:          9404,
		PlatformID:     1,
		PlatformUserID: "platega-background-user",
		ProductID:      productID,
		IdempotencyKey: idempotencyKey,
	}); err == nil {
		t.Fatal("expected temporary create error")
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		var orderID uint64
		var attemptID uint64
		var orderStatus string
		var attemptStatus string
		err := env.db.QueryRowContext(env.ctx, `
SELECT po.id, pa.id, po.status::text, pa.status::text
FROM payment_attempt pa
JOIN payment_order po ON po.id = pa.order_id
WHERE pa.idempotency_key = $1`, idempotencyKey).Scan(
			&orderID,
			&attemptID,
			&orderStatus,
			&attemptStatus,
		)
		if err != nil {
			t.Fatalf("read reconciled payment: %v", err)
		}
		if orderStatus == "fulfilled" && attemptStatus == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("reconciliation timeout: order=%s attempt=%s", orderStatus, attemptStatus)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if exportCalls.Load() == 0 {
		t.Fatal("background reconciliation did not call Platega export")
	}
}

func TestPlategaReconciliationReleasesMissingTransaction(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:           "platega_missing_recovery",
		AssetCode:           platega.AssetCode,
		ListAmountMinor:     100,
		GlobalLimit:         1,
		GlobalInterval:      "ONCE",
		GlobalIntervalCount: 1,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/transaction/export/json" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		http.Error(w, `{"error":"temporary"}`, http.StatusInternalServerError)
	}))
	defer server.Close()
	credentials := platega.Credentials{
		MerchantID: "missing-merchant",
		Secret:     "missing-secret",
		APIBaseURL: server.URL,
		HTTPClient: server.Client(),
	}
	const idempotencyKey = "platega-missing-recovery-key"
	if _, err := env.api.Adapters.Platega.CreatePayment(env.ctx, platega.CreatePaymentParams{
		Credentials:    credentials,
		WorkspaceID:    testWorkspaceID,
		AppID:          9405,
		PlatformID:     1,
		PlatformUserID: "platega-missing-user",
		ProductID:      productID,
		IdempotencyKey: idempotencyKey,
	}); err == nil {
		t.Fatal("expected temporary create error")
	}
	if _, err := env.db.ExecContext(env.ctx, `
UPDATE payment_attempt
SET created_at = now() - INTERVAL '1 hour'
WHERE idempotency_key = $1`, idempotencyKey); err != nil {
		t.Fatalf("age missing attempt: %v", err)
	}
	reconciled, err := env.api.Adapters.Platega.ReconcilePending(env.ctx, platega.ReconcileParams{
		ResolveCredentials: func(context.Context, string) (platega.Credentials, error) {
			return credentials, nil
		},
		CreatedTo:    time.Now().UTC(),
		Limit:        100,
		MissingAfter: 30 * time.Minute,
	})
	if err != nil {
		t.Fatalf("reconcile missing transaction: %v", err)
	}
	if reconciled.Released != 1 {
		t.Fatalf("unexpected missing reconciliation result: %#v", reconciled)
	}

	var attemptStatus string
	var orderStatus string
	if err := env.db.QueryRowContext(env.ctx, `
SELECT pa.status::text, po.status::text
FROM payment_attempt pa
JOIN payment_order po ON po.id = pa.order_id
WHERE pa.idempotency_key = $1`, idempotencyKey).Scan(&attemptStatus, &orderStatus); err != nil {
		t.Fatalf("read released transaction: %v", err)
	}
	if attemptStatus != "failed" || orderStatus != "canceled" {
		t.Fatalf("attempt=%s order=%s, want failed/canceled", attemptStatus, orderStatus)
	}

	var reserved int64
	if err := env.db.QueryRowContext(env.ctx, `
SELECT COALESCE(SUM(reserved_count), 0)
FROM payment_product_limit_counter
WHERE workspace_id = $1 AND product_id = $2`, testWorkspaceID, productID).Scan(&reserved); err != nil {
		t.Fatalf("read released Platega reservation: %v", err)
	}
	if reserved != 0 {
		t.Fatalf("reserved count = %d, want 0", reserved)
	}
}

func TestPlategaTerminalWebhooksReleaseOrderAndLimits(t *testing.T) {
	env := setupPaymentIntegrationTest(t)

	var transactionSequence atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v2/transaction/process" {
			http.NotFound(w, request)
			return
		}

		transactionID := fmt.Sprintf("platega-terminal-%d", transactionSequence.Add(1))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"transactionId":%q,
			"status":"PENDING",
			"url":"https://pay.example/terminal"
		}`, transactionID)
	}))
	defer server.Close()

	credentials := platega.Credentials{
		MerchantID: "terminal-merchant",
		Secret:     "terminal-secret",
		APIBaseURL: server.URL,
		HTTPClient: server.Client(),
	}
	headers := make(http.Header)
	headers.Set("X-MerchantId", credentials.MerchantID)
	headers.Set("X-Secret", credentials.Secret)

	testCases := []struct {
		providerStatus string
		attemptStatus  string
		orderStatus    string
	}{
		{
			providerStatus: "CANCELED",
			attemptStatus:  "canceled",
			orderStatus:    "canceled",
		},
		{
			providerStatus: "EXPIRED",
			attemptStatus:  "expired",
			orderStatus:    "expired",
		},
		{
			providerStatus: "FAILED",
			attemptStatus:  "failed",
			orderStatus:    "failed",
		},
	}
	for index, testCase := range testCases {
		t.Run(testCase.providerStatus, func(t *testing.T) {
			productID := createPaymentProduct(t, env, testProductOptions{
				ProductID:           fmt.Sprintf("platega_terminal_%d", index),
				AssetCode:           platega.AssetCode,
				ListAmountMinor:     100,
				GlobalLimit:         1,
				GlobalInterval:      "ONCE",
				GlobalIntervalCount: 1,
			})
			created, err := env.api.Adapters.Platega.CreatePayment(env.ctx, platega.CreatePaymentParams{
				Credentials:    credentials,
				WorkspaceID:    testWorkspaceID,
				AppID:          9600 + int64(index),
				PlatformID:     1,
				PlatformUserID: fmt.Sprintf("platega-terminal-user-%d", index),
				ProductID:      productID,
				IdempotencyKey: fmt.Sprintf("platega-terminal-key-%d", index),
			})
			if err != nil {
				t.Fatalf("create terminal payment: %v", err)
			}

			callback := fmt.Sprintf(`{
				"id":%q,
				"amount":1,
				"currency":"RUB",
				"status":%q,
				"paymentMethod":2
			}`, created.TransactionID, testCase.providerStatus)
			request := platega.WebhookRequest{
				Credentials: credentials,
				WorkspaceID: testWorkspaceID,
				Raw:         []byte(callback),
				Headers:     headers,
			}
			if _, err := env.api.Adapters.Platega.HandleWebhook(env.ctx, request); err != nil {
				t.Fatalf("handle terminal webhook: %v", err)
			}
			if _, err := env.api.Adapters.Platega.HandleWebhook(env.ctx, request); err != nil {
				t.Fatalf("repeat terminal webhook: %v", err)
			}

			assertAttemptStatus(t, env.ctx, env.db, created.AttemptID, testCase.attemptStatus)
			assertOrderStatus(t, env.ctx, env.db, created.OrderID, testCase.orderStatus)

			var reserved int64
			if err := env.db.QueryRowContext(env.ctx, `
SELECT COALESCE(SUM(reserved_count), 0)
FROM payment_product_limit_counter
WHERE workspace_id = $1 AND product_id = $2`, testWorkspaceID, productID).Scan(&reserved); err != nil {
				t.Fatalf("read terminal reservation: %v", err)
			}
			if reserved != 0 {
				t.Fatalf("reserved count = %d, want 0", reserved)
			}
		})
	}
}

func TestPlategaChargebackCreatesExternalCallback(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:           "platega_chargeback_callback",
		AssetCode:           platega.AssetCode,
		ListAmountMinor:     100,
		GlobalLimit:         1,
		GlobalInterval:      "ONCE",
		GlobalIntervalCount: 1,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"transactionId":"platega-chargeback-payment",
			"status":"PENDING",
			"url":"https://pay.example/chargeback"
		}`))
	}))
	defer server.Close()

	credentials := platega.Credentials{
		MerchantID: "chargeback-merchant",
		Secret:     "chargeback-secret",
		APIBaseURL: server.URL,
		HTTPClient: server.Client(),
	}
	created, err := env.api.Adapters.Platega.CreatePayment(env.ctx, platega.CreatePaymentParams{
		Credentials:    credentials,
		WorkspaceID:    testWorkspaceID,
		AppID:          9701,
		PlatformID:     1,
		PlatformUserID: "platega-chargeback-user",
		ProductID:      productID,
		IdempotencyKey: "platega-chargeback-key",
	})
	if err != nil {
		t.Fatalf("create chargeback payment: %v", err)
	}

	headers := make(http.Header)
	headers.Set("X-MerchantId", credentials.MerchantID)
	headers.Set("X-Secret", credentials.Secret)
	callback := func(status string, amount int) platega.WebhookRequest {
		raw := fmt.Sprintf(`{
			"id":%q,
			"amount":%d,
			"currency":"RUB",
			"status":%q,
			"paymentMethod":2
		}`, created.TransactionID, amount, status)

		return platega.WebhookRequest{
			Credentials: credentials,
			WorkspaceID: testWorkspaceID,
			Raw:         []byte(raw),
			Headers:     headers,
		}
	}
	confirmed, err := env.api.Adapters.Platega.HandleWebhook(env.ctx, callback("CONFIRMED", 1))
	if err != nil {
		t.Fatalf("confirm chargeback payment: %v", err)
	}
	if confirmed.FulfilledID == nil {
		t.Fatal("confirmed payment has no fulfillment")
	}

	chargeback, err := env.api.Adapters.Platega.HandleWebhook(env.ctx, callback("CHARGEBACKED", 1))
	if err != nil {
		t.Fatalf("apply chargeback: %v", err)
	}
	if chargeback.FulfilledID == nil || *chargeback.FulfilledID != *confirmed.FulfilledID {
		t.Fatalf("chargeback fulfillment = %#v, want %d", chargeback.FulfilledID, *confirmed.FulfilledID)
	}
	duplicate, err := env.api.Adapters.Platega.HandleWebhook(env.ctx, callback("CHARGEBACKED", 1))
	if err != nil {
		t.Fatalf("repeat chargeback: %v", err)
	}
	if !duplicate.AlreadyDone {
		t.Fatal("repeat chargeback is not idempotent")
	}
	if _, err := env.api.Adapters.Platega.HandleWebhook(
		env.ctx,
		callback("CHARGEBACKED", 2),
	); !errors.Is(err, repository.ErrPaymentMismatch) {
		t.Fatalf("conflicting chargeback payload error = %v, want ErrPaymentMismatch", err)
	}

	assertAttemptStatus(t, env.ctx, env.db, created.AttemptID, "chargebacked")
	assertOrderStatus(t, env.ctx, env.db, created.OrderID, "chargebacked")
	assertCallbackEvent(
		t,
		env.ctx,
		env.db,
		CallbackEventPaymentOrderChargebacked,
		created.OrderID,
		1,
	)

	var fulfillmentStatus string
	if err := env.db.QueryRowContext(
		env.ctx,
		"SELECT status::text FROM payment_fulfillment WHERE order_id = $1",
		created.OrderID,
	).Scan(&fulfillmentStatus); err != nil {
		t.Fatalf("read chargeback fulfillment: %v", err)
	}
	if fulfillmentStatus != "revoked" {
		t.Fatalf("fulfillment status = %q, want revoked", fulfillmentStatus)
	}

	var callbackPayloadRaw []byte
	if err := env.db.QueryRowContext(env.ctx, `
SELECT payload
FROM payment_clb_event
WHERE event_type = $1 AND event_key = $2`,
		CallbackEventPaymentOrderChargebacked,
		fmt.Sprintf("%s:%d", CallbackEventPaymentOrderChargebacked, created.OrderID),
	).Scan(&callbackPayloadRaw); err != nil {
		t.Fatalf("read chargeback callback: %v", err)
	}
	var callbackPayload PaymentChargebackedCallbackPayload
	if err := json.Unmarshal(callbackPayloadRaw, &callbackPayload); err != nil {
		t.Fatalf("decode chargeback callback: %v", err)
	}
	if callbackPayload.OrderID != created.OrderID ||
		callbackPayload.AttemptID != created.AttemptID ||
		callbackPayload.FulfillmentID != *confirmed.FulfilledID ||
		callbackPayload.ProviderPaymentID != created.TransactionID ||
		callbackPayload.Reason != "provider_chargeback" ||
		len(callbackPayload.Rewards) != 1 {
		t.Fatalf("unexpected chargeback callback: %#v", callbackPayload)
	}
	callbackContext, err := newCallbackContext(callbackutil.Context{
		Context:   env.ctx,
		EventType: CallbackEventPaymentOrderChargebacked,
		Payload:   callbackPayloadRaw,
	})
	if err != nil {
		t.Fatalf("build external chargeback callback context: %v", err)
	}
	if callbackContext.PaymentChargebacked == nil || callbackContext.Payload == nil ||
		callbackContext.PaymentChargebacked.OrderID != created.OrderID ||
		len(callbackContext.Payload.Rewards) != 1 {
		t.Fatalf("unexpected external chargeback context: %#v", callbackContext)
	}
}

func TestPlategaReconciliationCompletesBoundPendingPayment(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "platega_bound_pending_reconciliation",
		AssetCode:       platega.AssetCode,
		ListAmountMinor: 100,
	})

	var orderPublicID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v2/transaction/process":
			_, _ = w.Write([]byte(`{
				"transactionId":"platega-bound-pending",
				"status":"PENDING",
				"url":"https://pay.example/pending"
			}`))
		case "/transaction/export/json":
			_, _ = fmt.Fprintf(w, `[{
				"recordId":"platega-bound-pending",
				"amount":1,
				"currencyCode":"RUB",
				"status":"CONFIRMED",
				"paymentMethod":"SBPQR",
				"payload":%q
			}]`, orderPublicID)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	credentials := platega.Credentials{
		MerchantID: "pending-merchant",
		Secret:     "pending-secret",
		APIBaseURL: server.URL,
		HTTPClient: server.Client(),
	}
	created, err := env.api.Adapters.Platega.CreatePayment(env.ctx, platega.CreatePaymentParams{
		Credentials:    credentials,
		WorkspaceID:    testWorkspaceID,
		AppID:          9702,
		PlatformID:     1,
		PlatformUserID: "platega-pending-user",
		ProductID:      productID,
		IdempotencyKey: "platega-pending-key",
	})
	if err != nil {
		t.Fatalf("create bound pending payment: %v", err)
	}
	orderPublicID = created.OrderPublicID

	result, err := env.api.Adapters.Platega.ReconcilePending(env.ctx, platega.ReconcileParams{
		ResolveCredentials: func(context.Context, string) (platega.Credentials, error) {
			return credentials, nil
		},
		CreatedTo: time.Now().UTC().Add(time.Second),
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("reconcile bound pending payment: %v", err)
	}
	if result.Scanned != 1 || result.Completed != 1 {
		t.Fatalf("unexpected reconciliation result: %#v", result)
	}
	assertAttemptStatus(t, env.ctx, env.db, created.AttemptID, "succeeded")
	assertOrderStatus(t, env.ctx, env.db, created.OrderID, "fulfilled")
}

func TestPlategaCreateReplayPreservesProviderStatus(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "platega_replay_status",
		AssetCode:       platega.AssetCode,
		ListAmountMinor: 100,
	})

	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"transactionId":"platega-replay-payment",
			"status":"PENDING",
			"url":"https://pay.example/replay"
		}`))
	}))
	defer server.Close()

	params := platega.CreatePaymentParams{
		Credentials: platega.Credentials{
			MerchantID: "replay-merchant",
			Secret:     "replay-secret",
			APIBaseURL: server.URL,
			HTTPClient: server.Client(),
		},
		WorkspaceID:    testWorkspaceID,
		AppID:          9703,
		PlatformID:     1,
		PlatformUserID: "platega-replay-user",
		ProductID:      productID,
		IdempotencyKey: "platega-replay-key",
	}
	created, err := env.api.Adapters.Platega.CreatePayment(env.ctx, params)
	if err != nil {
		t.Fatalf("create replay payment: %v", err)
	}
	replayed, err := env.api.Adapters.Platega.CreatePayment(env.ctx, params)
	if err != nil {
		t.Fatalf("replay payment: %v", err)
	}
	if replayed.Status != created.Status || replayed.Status != platega.StatusPending {
		t.Fatalf("replayed status = %q, original = %q", replayed.Status, created.Status)
	}
	if providerCalls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", providerCalls.Load())
	}
}

func TestPlategaExportRejectsUnsafeRedirect(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "platega_export_redirect",
		AssetCode:       platega.AssetCode,
		ListAmountMinor: 100,
	})

	var unsafeTargetReached atomic.Bool
	unsafeTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		unsafeTargetReached.Store(true)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer unsafeTarget.Close()

	var providerURL string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/transaction/process":
			http.Error(w, `{"error":"temporary"}`, http.StatusInternalServerError)
		case "/transaction/export/json":
			_, _ = fmt.Fprintf(w, "%q", providerURL+"/download")
		case "/download":
			http.Redirect(w, request, unsafeTarget.URL, http.StatusFound)
		default:
			http.NotFound(w, request)
		}
	}))
	defer provider.Close()
	providerURL = provider.URL

	credentials := platega.Credentials{
		MerchantID: "redirect-merchant",
		Secret:     "redirect-secret",
		APIBaseURL: provider.URL,
		HTTPClient: provider.Client(),
	}
	if _, err := env.api.Adapters.Platega.CreatePayment(env.ctx, platega.CreatePaymentParams{
		Credentials:    credentials,
		WorkspaceID:    testWorkspaceID,
		AppID:          9704,
		PlatformID:     1,
		PlatformUserID: "platega-redirect-user",
		ProductID:      productID,
		IdempotencyKey: "platega-redirect-key",
	}); err == nil {
		t.Fatal("expected temporary create error")
	}

	_, err := env.api.Adapters.Platega.ReconcilePending(env.ctx, platega.ReconcileParams{
		ResolveCredentials: func(context.Context, string) (platega.Credentials, error) {
			return credentials, nil
		},
		CreatedTo: time.Now().UTC().Add(time.Second),
		Limit:     100,
	})
	if !errors.Is(err, platega.ErrExportURLUnsafe) {
		t.Fatalf("unsafe redirect error = %v, want ErrExportURLUnsafe", err)
	}
	if unsafeTargetReached.Load() {
		t.Fatal("unsafe redirect target was requested")
	}
}

func TestPlategaExportLimitsInitialResponse(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	productID := createPaymentProduct(t, env, testProductOptions{
		ProductID:       "platega_export_initial_limit",
		AssetCode:       platega.AssetCode,
		ListAmountMinor: 100,
	})

	largePayload := strings.Repeat("a", (16<<20)+1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/transaction/process":
			http.Error(w, `{"error":"temporary"}`, http.StatusInternalServerError)
		case "/transaction/export/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `[{"recordId":"large","payload":%q}]`, largePayload)
		default:
			http.NotFound(w, request)
		}
	}))
	defer provider.Close()

	credentials := platega.Credentials{
		MerchantID: "large-merchant",
		Secret:     "large-secret",
		APIBaseURL: provider.URL,
		HTTPClient: provider.Client(),
	}
	if _, err := env.api.Adapters.Platega.CreatePayment(env.ctx, platega.CreatePaymentParams{
		Credentials:    credentials,
		WorkspaceID:    testWorkspaceID,
		AppID:          9705,
		PlatformID:     1,
		PlatformUserID: "platega-large-user",
		ProductID:      productID,
		IdempotencyKey: "platega-large-key",
	}); err == nil {
		t.Fatal("expected temporary create error")
	}

	_, err := env.api.Adapters.Platega.ReconcilePending(env.ctx, platega.ReconcileParams{
		ResolveCredentials: func(context.Context, string) (platega.Credentials, error) {
			return credentials, nil
		},
		CreatedTo: time.Now().UTC().Add(time.Second),
		Limit:     100,
	})
	if !errors.Is(err, platega.ErrExportResponseTooLarge) {
		t.Fatalf("large export error = %v, want ErrExportResponseTooLarge", err)
	}
}
