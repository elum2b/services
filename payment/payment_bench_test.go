package payment

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"github.com/elum-utils/sign/vkmashop"
	services "github.com/elum2b/services"
	utils "github.com/elum2b/services/internal/utils"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	paymentvkma "github.com/elum2b/services/payment/adapters/vkma"
	"github.com/elum2b/services/payment/repository"
	"github.com/elum2b/services/payment/service/checkout"
	"github.com/elum2b/services/payment/service/product"
	"github.com/elum2b/services/payment/service/subscription"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
	"github.com/sqlc-dev/pqtype"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	benchDBName           = "payment_bench"
	benchSeedDatabase     = true
	benchProductCount     = 1_000
	benchTransactionCount = 1_000
	benchBatchSize        = 1_000
	benchWorkspaceID      = "bbbbbbbb-bbbb-bbbb-bbbb-000000000001"
)

var benchAssets = []string{"RUB"}

var benchLocales = []string{"ru"}

type paymentBenchmarkEnv struct {
	ctx       context.Context
	db        *sql.DB
	api       *Payment
	q         *paymentsqlc.Queries
	products  []benchProduct
	keys      []string
	keyHashes []string
	orders    []uint64
	attempts  []uint64
}

type benchProduct struct {
	id       string
	itemID   string
	priceIDs map[string]uint64
}

var (
	paymentBenchOnce      sync.Once
	paymentBenchEnv       paymentBenchmarkEnv
	paymentBenchErr       error
	paymentBenchSeq       uint64
	paymentBenchRunNumber = uint64(time.Now().UnixNano())
	paymentBenchRunID     = strconv.FormatInt(time.Now().UnixNano(), 36)
)

func BenchmarkPaymentServiceMethods(b *testing.B) {
	env := setupPaymentBenchmark(b)
	env.db.SetMaxOpenConns(320)
	env.db.SetMaxIdleConns(320)

	productA := env.products[0]
	productB := env.products[1]
	key := env.keys[0]
	fulfilledAttemptID := env.attempts[0]
	providerPaymentID := "bench_pay_1"

	b.ReportAllocs()

	for _, locale := range benchLocales {
		for _, asset := range benchAssets {
			b.Run("Product.Get/"+locale+"/"+asset, func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					_, err := env.api.User.GetProduct(env.ctx, product.GetParams{
						Identity:  paymentTestIdentity(benchWorkspaceID, 7001, 1, "bench_user_"+strconv.Itoa(i%100_000)),
						ProductID: env.products[i%len(env.products)].id,
						AssetCode: asset,
						Locale:    locale,
					})
					benchNoError(b, err)
				}
			})
		}
	}

	for _, locale := range benchLocales {
		for _, asset := range benchAssets {
			b.Run("Product.GetByKey/"+locale+"/"+asset, func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					_, err := env.api.User.GetProductByKey(env.ctx, product.GetByKeyParams{
						Key:       env.keys[i%len(env.keys)],
						AssetCode: asset,
						Locale:    locale,
					})
					benchNoError(b, err)
				}
			})
		}
	}

	b.Run("Product.CreateKey", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := env.api.Admin.CreateProductKey(env.ctx, product.CreateKeyParams{
				WorkspaceID:    benchWorkspaceID,
				AppID:          7001,
				PlatformID:     1,
				PlatformUserID: "bench_key_target_" + strconv.Itoa(i),
				ProductID:      env.products[i%len(env.products)].id,
				MaxUses:        1_000_000,
			})
			benchNoError(b, err)
		}
	})

	for _, locale := range benchLocales {
		for _, asset := range benchAssets {
			b.Run("Checkout.CreateOrder/"+locale+"/"+asset, func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					_, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
						Identity:  paymentTestIdentity(benchWorkspaceID, 7001, 1, "bench_checkout_"+strconv.Itoa(i)),
						ProductID: env.products[i%len(env.products)].id,
						AssetCode: asset,
						Locale:    locale,
					})
					benchNoError(b, err)
				}
			})
		}
	}

	b.Run("Checkout.CreateOrderByKey", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			payerID := int64(1)
			payerUserID := "bench_payer_" + strconv.Itoa(i)
			_, err := env.api.User.CreateOrderByKey(env.ctx, checkout.CreateOrderByKeyParams{
				Key: key,
				Payer: &services.Actor{
					PlatformID:     payerID,
					PlatformUserID: payerUserID,
				},
				AssetCode: "RUB",
				Locale:    "ru",
			})
			benchNoError(b, err)
		}
	})

	b.Run("Checkout.CreateAttempt", func(b *testing.B) {
		orders := make([]*checkout.Order, b.N)
		b.StopTimer()
		for i := 0; i < b.N; i++ {
			seq := benchNextSeq()
			orders[i] = createBenchmarkOrder(b, env, productA.id, "RUB", benchRunValue("bench_attempt_order", seq))
		}
		b.StartTimer()
		for i := 0; i < b.N; i++ {
			seq := benchNextSeq()
			order := orders[i]
			paymentID := benchRunValue("bench_method_attempt", seq)
			_, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
				Identity:          paymentAttemptIdentity(order),
				OrderID:           order.ID,
				ProviderCode:      "yookassa",
				ProviderPaymentID: &paymentID,
			})
			benchNoError(b, err)
		}
	})

	b.Run("Checkout.CreateEvent", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			seq := benchNextSeq()
			eventID := benchRunValue("bench_method_event", seq)
			paymentID := benchRunValue("bench_method_event_pay", seq)
			_, err := env.api.Operational.CreateEvent(env.ctx, checkout.CreateEventParams{
				WorkspaceID:       benchWorkspaceID,
				ProviderCode:      "yookassa",
				AttemptID:         utils.Ref(int64(fulfilledAttemptID)),
				OrderID:           utils.Ref(int64(env.orders[0])),
				ProviderEventID:   &eventID,
				ProviderPaymentID: &paymentID,
				EventType:         "succeeded",
				EventStatus:       utils.Ref("succeeded"),
				PayloadHash:       benchHash(eventID),
				SignatureValid:    utils.Ref(true),
			})
			benchNoError(b, err)
		}
	})

	b.Run("Checkout.CompleteAttempt/idempotent", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := env.api.Operational.CompleteAttempt(env.ctx, checkout.CompleteAttemptParams{
				WorkspaceID:       benchWorkspaceID,
				AttemptID:         fulfilledAttemptID,
				ProviderCode:      "yookassa",
				ProviderPaymentID: &providerPaymentID,
				AmountMinor:       1000,
				AssetCode:         "RUB",
			})
			benchNoError(b, err)
		}
	})

	b.Run("Subscription.IsActive", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			active, err := env.api.User.IsSubscriptionActive(env.ctx, subscription.IsActiveParams{
				Identity:     paymentTestIdentity(benchWorkspaceID, 7001, 1, "bench_user_"+strconv.Itoa(i%100_000)),
				ProductID:    env.products[i%len(env.products)].id,
				ProviderCode: "vkma",
			})
			benchNoError(b, err)
			_ = active
		}
	})

	b.Run("VKMA.GetItemForWorkspace", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := env.api.Adapters.VKMA.GetItemForWorkspace(env.ctx, benchWorkspaceID, vkmashop.Params{
				NotificationType: vkmashop.GetItem,
				AppID:            7001,
				UserID:           9000 + i%100_000,
				Item:             productB.id,
				Lang:             "ru",
			})
			benchNoError(b, err)
		}
	})

	b.Run("VKMA.ChargeableForWorkspace/idempotent", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := env.api.Adapters.VKMA.ChargeableForWorkspace(env.ctx, benchWorkspaceID, vkmashop.Params{
				NotificationType: vkmashop.OrderStatusChange,
				Status:           vkmashop.Chargeable,
				AppID:            7001,
				UserID:           9000,
				Item:             productB.id,
				OrderID:          1,
				Lang:             "ru",
			})
			benchNoError(b, err)
		}
	})

	b.Run("VKMA.SubscriptionStatus", func(b *testing.B) {
		subscriptionIDs := make([]int, b.N)
		b.StopTimer()
		for i := 0; i < b.N; i++ {
			subscriptionIDs[i] = int((paymentBenchRunNumber % 1_000_000_000) + benchNextSeq())
			attempt, err := env.q.AdminGetPaymentAttempt(
				env.ctx,
				int64(env.attempts[i%len(env.attempts)]),
			)
			benchNoError(b, err)
			order, err := env.q.GetPaymentOrder(env.ctx, attempt.OrderID)
			benchNoError(b, err)
			_, err = env.q.UpsertPaymentSubscription(env.ctx, benchmarkUpsertSubscriptionParams(
				order.ProductID,
				uint64(order.ID),
				uint64(attempt.ID),
				strconv.Itoa(subscriptionIDs[i]),
			))
			benchNoError(b, err)
		}
		b.StartTimer()
		for i := 0; i < b.N; i++ {
			_, err := env.api.Adapters.VKMA.Canceled(env.ctx, benchWorkspaceID, vkmashop.Params{
				NotificationType: vkmashop.SubscriptionStatusChange,
				Status:           vkmashop.Canceled,
				AppID:            7001,
				UserID:           9000,
				SubscriptionID:   subscriptionIDs[i],
			})
			benchNoError(b, err)
		}
	})

	_ = paymentvkma.ProviderCode
}

func BenchmarkPaymentImportExport(b *testing.B) {
	env := setupPaymentIntegrationTest(b)
	workspaceID := "bbbbbbbb-bbbb-bbbb-bbbb-000000000101"
	productID := "import_export_product"
	groupCode := "import_export_group"
	itemID := "import_export_item"
	now := time.Now()
	from := now.Add(-time.Hour)
	until := now.Add(time.Hour)
	benchNoError(b, env.api.Admin.SaveProductGroup(env.ctx, product.UpsertGroupParams{
		WorkspaceID: workspaceID, Code: groupCode, TitleKey: utils.Ref(groupCode + ".title"),
		DescriptionKey: utils.Ref(groupCode + ".description"), Position: 1, IsActive: true,
	}))
	benchNoError(b, env.api.Admin.SaveProduct(env.ctx, product.UpsertParams{
		WorkspaceID: workspaceID, ID: productID, GroupCode: utils.Ref(groupCode),
		TitleKey: productID + ".title", DescriptionKey: utils.Ref(productID + ".description"),
		QuantityMode: "fixed", Position: 1, GlobalInterval: "UNLIMITED", UserInterval: "UNLIMITED",
		AvailableFrom: &from, AvailableUntil: &until, IsVisible: true,
	}))
	for _, localization := range []product.UpsertLocalizationParams{
		{WorkspaceID: workspaceID, Locale: "ru", LocalizationKey: groupCode + ".title", Value: "Group"},
		{WorkspaceID: workspaceID, Locale: "ru", LocalizationKey: groupCode + ".description", Value: "Group description"},
		{WorkspaceID: workspaceID, Locale: "ru", LocalizationKey: productID + ".title", Value: "Product"},
		{WorkspaceID: workspaceID, Locale: "ru", LocalizationKey: productID + ".description", Value: "Product description"},
		{WorkspaceID: workspaceID, Locale: "ru", LocalizationKey: itemID + ".title", Value: "Item"},
		{WorkspaceID: workspaceID, Locale: "ru", LocalizationKey: itemID + ".description", Value: "Item description"},
	} {
		benchNoError(b, env.api.Admin.SaveLocalization(env.ctx, localization))
	}
	benchNoError(b, env.api.Admin.AttachProductItem(env.ctx, product.AddItemParams{
		WorkspaceID: workspaceID, ProductID: productID, ItemID: itemID, Quantity: 25, Scale: 2,
	}))
	_, err := env.api.Admin.CreateCatalogPrice(env.ctx, product.CreatePriceParams{
		WorkspaceID: workspaceID, ProductID: productID, AssetCode: "RUB",
		ListAmountMinor: 1000, DiscountAmountMinor: 100, StartsAt: &from, EndsAt: &until,
	})
	benchNoError(b, err)
	pkg, err := env.api.Admin.Export(env.ctx, workspaceID, repository.ExportRequest{})
	benchNoError(b, err)
	b.ReportAllocs()
	b.Run("Export", func(b *testing.B) {
		for range b.N {
			_, err := env.api.Admin.Export(env.ctx, workspaceID, repository.ExportRequest{})
			benchNoError(b, err)
		}
	})
	b.Run("Import/update", func(b *testing.B) {
		for range b.N {
			_, err := env.api.Admin.Import(env.ctx, workspaceID, repository.ImportRequest{
				Package: pkg, ConflictStrategy: repository.ImportConflictUpdate,
			})
			benchNoError(b, err)
		}
	})
}

func BenchmarkPaymentSQLCQueries(b *testing.B) {
	env := setupPaymentBenchmark(b)
	q := env.q
	productA := env.products[0]
	priceID := productA.priceIDs["RUB"]
	orderID := env.orders[0]
	attemptID := env.attempts[0]

	b.ReportAllocs()

	b.Run("ListProviders", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.ListProviders(env.ctx)
			benchNoError(b, err)
		}
	})

	b.Run("ListAssets", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.ListAssets(env.ctx)
			benchNoError(b, err)
		}
	})

	b.Run("GetProviderAsset", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.GetProviderAsset(env.ctx, paymentsqlc.GetProviderAssetParams{ProviderCode: "yookassa", AssetCode: "RUB"})
			benchNoError(b, err)
		}
	})

	b.Run("UpsertProductGroup", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			err := q.UpsertProductGroup(env.ctx, paymentsqlc.UpsertProductGroupParams{
				WorkspaceID: benchWorkspaceID,
				Code:        "bench_group_write_" + strconv.Itoa(i),
				TitleKey:    sql.NullString{String: "bench.group.title", Valid: true},
				IsActive:    true,
			})
			benchNoError(b, err)
		}
	})

	b.Run("DeleteProductGroup", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			code := "bench_group_delete_" + strconv.Itoa(i)
			benchNoError(b, q.UpsertProductGroup(env.ctx, paymentsqlc.UpsertProductGroupParams{WorkspaceID: benchWorkspaceID, Code: code, IsActive: true}))
			_, err := q.DeleteProductGroup(env.ctx, paymentsqlc.DeleteProductGroupParams{WorkspaceID: benchWorkspaceID, Code: code})
			benchNoError(b, err)
		}
	})

	b.Run("UpsertLocalization", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			err := q.UpsertLocalization(env.ctx, paymentsqlc.UpsertLocalizationParams{
				WorkspaceID:     benchWorkspaceID,
				Locale:          benchLocales[i%len(benchLocales)],
				LocalizationKey: "bench.write.localization." + strconv.Itoa(i),
				Value:           "Benchmark value",
			})
			benchNoError(b, err)
		}
	})

	b.Run("DeleteLocalization", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			key := "bench.delete.localization." + strconv.Itoa(i)
			benchNoError(b, q.UpsertLocalization(env.ctx, paymentsqlc.UpsertLocalizationParams{WorkspaceID: benchWorkspaceID, Locale: "ru", LocalizationKey: key, Value: "delete"}))
			_, err := q.DeleteLocalization(env.ctx, paymentsqlc.DeleteLocalizationParams{WorkspaceID: benchWorkspaceID, Locale: "ru", LocalizationKey: key})
			benchNoError(b, err)
		}
	})

	b.Run("UpsertProduct", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			err := q.UpsertProduct(env.ctx, benchmarkUpsertProductParams("bench_write_product_"+strconv.Itoa(i), "bench_group_0"))
			benchNoError(b, err)
		}
	})

	b.Run("DeleteProduct", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			id := "bench_delete_product_" + strconv.Itoa(i)
			benchNoError(b, q.UpsertProduct(env.ctx, benchmarkUpsertProductParams(id, "bench_group_0")))
			_, err := q.DeleteProduct(env.ctx, paymentsqlc.DeleteProductParams{WorkspaceID: benchWorkspaceID, ID: id})
			benchNoError(b, err)
		}
	})

	b.Run("UpsertProductItem", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			err := q.UpsertProductItem(env.ctx, paymentsqlc.UpsertProductItemParams{
				WorkspaceID: benchWorkspaceID,
				ProductID:   env.products[i%len(env.products)].id,
				ItemID:      env.products[(i+1)%len(env.products)].itemID,
				RewardType:  paymentsqlc.PaymentProductItemRewardTypeQuantity,
				Quantity:    1,
			})
			benchNoError(b, err)
		}
	})

	b.Run("DeleteProductItem", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			p := env.products[i%len(env.products)]
			itemID := env.products[(i+2)%len(env.products)].itemID
			benchNoError(b, q.UpsertProductItem(env.ctx, paymentsqlc.UpsertProductItemParams{
				WorkspaceID: benchWorkspaceID,
				ProductID:   p.id,
				ItemID:      itemID,
				RewardType:  paymentsqlc.PaymentProductItemRewardTypeQuantity,
				Quantity:    1,
			}))
			_, err := q.DeleteProductItem(env.ctx, paymentsqlc.DeleteProductItemParams{WorkspaceID: benchWorkspaceID, ProductID: p.id, ItemID: itemID})
			benchNoError(b, err)
		}
	})

	b.Run("CreateProductPrice", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			seq := benchNextSeq()
			_, err := q.CreateProductPrice(env.ctx, benchmarkCreatePriceParams(productA.id, benchPriceStart(seq)))
			benchNoError(b, err)
		}
	})

	b.Run("UpdateProductPrice", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.UpdateProductPrice(env.ctx, paymentsqlc.UpdateProductPriceParams{
				ID:                  int64(priceID),
				WorkspaceID:         benchWorkspaceID,
				AssetCode:           "RUB",
				ListAmountMinor:     int64(1000 + i%100),
				DiscountAmountMinor: 0,
				StartsAt:            time.Now().Add(-24 * time.Hour),
				EndsAt:              time.Now().Add(24 * time.Hour),
			})
			benchNoError(b, err)
		}
	})

	b.Run("DeleteProductPrice", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			seq := benchNextSeq()
			id, err := q.CreateProductPrice(env.ctx, benchmarkCreatePriceParams(productA.id, benchPriceStart(seq)))
			benchNoError(b, err)
			_, err = q.DeleteProductPrice(env.ctx, paymentsqlc.DeleteProductPriceParams{WorkspaceID: benchWorkspaceID, ID: id})
			benchNoError(b, err)
		}
	})

	b.Run("GetCurrentProductPrice", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.GetCurrentProductPrice(env.ctx, paymentsqlc.GetCurrentProductPriceParams{
				WorkspaceID:   benchWorkspaceID,
				WorkspaceID_2: benchWorkspaceID,
				ProductID:     env.products[i%len(env.products)].id,
				AssetCode:     benchAssets[i%len(benchAssets)],
			})
			benchNoError(b, err)
		}
	})

	b.Run("GetProductRows", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			locale := benchLocales[i%len(benchLocales)]
			_, err := q.GetProductRows(env.ctx, paymentsqlc.GetProductRowsParams{
				ProductID:   env.products[i%len(env.products)].id,
				WorkspaceID: benchWorkspaceID,
				AssetCode:   benchAssets[i%len(benchAssets)],
				Locale:      locale,
			})
			benchNoError(b, err)
		}
	})

	b.Run("GetProductPreviewRows", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			locale := benchLocales[i%len(benchLocales)]
			_, err := q.GetProductPreviewRows(env.ctx, paymentsqlc.GetProductPreviewRowsParams{
				ProductID:   env.products[i%len(env.products)].id,
				WorkspaceID: benchWorkspaceID,
				Locale:      locale,
			})
			benchNoError(b, err)
		}
	})

	b.Run("ListProductPriceOptions", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.ListProductPriceOptions(env.ctx, paymentsqlc.ListProductPriceOptionsParams{WorkspaceID: benchWorkspaceID, ProductID: env.products[i%len(env.products)].id})
			benchNoError(b, err)
		}
	})

	b.Run("ListProductLocales", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.ListProductLocales(env.ctx, paymentsqlc.ListProductLocalesParams{WorkspaceID: benchWorkspaceID, ProductID: env.products[i%len(env.products)].id})
			benchNoError(b, err)
		}
	})

	b.Run("GetProductLimitCounterCount", func(b *testing.B) {
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		end := start.Add(24 * time.Hour)
		_, err := q.EnsureProductLimitCounter(env.ctx, paymentsqlc.EnsureProductLimitCounterParams{
			WorkspaceID:    benchWorkspaceID,
			PlatformID:     1,
			ProductID:      productA.id,
			CounterScope:   paymentsqlc.PaymentProductLimitCounterCounterScopeGlobal,
			PlatformUserID: "",
			WindowStart:    start,
			WindowEnd:      end,
		})
		benchNoError(b, err)
		for i := 0; i < b.N; i++ {
			_, err := q.GetProductLimitCounterCount(env.ctx, paymentsqlc.GetProductLimitCounterCountParams{
				WorkspaceID:    benchWorkspaceID,
				PlatformID:     1,
				ProductID:      productA.id,
				CounterScope:   paymentsqlc.PaymentProductLimitCounterCounterScopeGlobal,
				PlatformUserID: "",
				WindowStart:    start,
				WindowEnd:      end,
			})
			benchNoError(b, err)
		}
	})

	b.Run("GetPurchaseKeyByHash", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.GetPurchaseKeyByHash(env.ctx, env.keyHashes[i%len(env.keyHashes)])
			benchNoError(b, err)
		}
	})

	b.Run("LockPurchaseKeyByHash", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.LockPurchaseKeyByHash(env.ctx, env.keyHashes[i%len(env.keyHashes)])
			benchNoError(b, err)
		}
	})

	b.Run("CreatePurchaseKey", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			seq := benchNextSeq()
			_, err := q.CreatePurchaseKey(env.ctx, paymentsqlc.CreatePurchaseKeyParams{
				WorkspaceID:    benchWorkspaceID,
				KeyHash:        benchHash(benchRunValue("sqlc_create_key", seq)),
				AppID:          7001,
				PlatformID:     1,
				PlatformUserID: benchRunValue("bench_sqlc_key_user", seq),
				ProductID:      productA.id,
				MaxUses:        1_000_000,
			})
			benchNoError(b, err)
		}
	})

	b.Run("ReservePurchaseKeyUsage", func(b *testing.B) {
		keyID := int64(1)
		for i := 0; i < b.N; i++ {
			_, err := q.ReservePurchaseKeyUsage(env.ctx, keyID)
			benchNoError(b, err)
		}
	})

	b.Run("CreatePaymentOrder", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			seq := benchNextSeq()
			_, err := q.CreatePaymentOrder(env.ctx, benchmarkCreateOrderParams(productA, "RUB", priceID, benchRunValue("sqlc_create_order", seq)))
			benchNoError(b, err)
		}
	})

	b.Run("GetPaymentOrder", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.GetPaymentOrder(env.ctx, int64(env.orders[i%len(env.orders)]))
			benchNoError(b, err)
		}
	})

	b.Run("GetPaymentOrderByPublicID", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.GetPaymentOrderByPublicID(env.ctx, "bench-public-"+strconv.Itoa((i%len(env.orders))+1))
			benchNoError(b, err)
		}
	})

	b.Run("LockPaymentOrder", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.LockPaymentOrder(env.ctx, int64(env.orders[i%len(env.orders)]))
			benchNoError(b, err)
		}
	})

	b.Run("CreatePaymentAttempt", func(b *testing.B) {
		orderIDs := make([]uint64, b.N)
		b.StopTimer()
		for i := 0; i < b.N; i++ {
			seq := benchNextSeq()
			order := createBenchmarkOrder(b, env, productA.id, "RUB", benchRunValue("sqlc_attempt_order", seq))
			orderIDs[i] = order.ID
		}
		b.StartTimer()
		for i := 0; i < b.N; i++ {
			seq := benchNextSeq()
			_, err := q.CreatePaymentAttempt(env.ctx, benchmarkCreateAttemptParams(orderIDs[i], benchRunValue("sqlc_attempt", seq)))
			benchNoError(b, err)
		}
	})

	b.Run("GetPaymentAttemptByProviderPaymentID", func(b *testing.B) {
		yookassaAttemptCount := len(env.attempts) / len(benchAssets)
		if yookassaAttemptCount == 0 {
			yookassaAttemptCount = 1
		}
		for i := 0; i < b.N; i++ {
			paymentNumber := 1 + (i%yookassaAttemptCount)*len(benchAssets)
			_, err := q.GetPaymentAttemptByProviderPaymentID(env.ctx, paymentsqlc.GetPaymentAttemptByProviderPaymentIDParams{
				ProviderCode:      "yookassa",
				ProviderPaymentID: sql.NullString{String: "bench_pay_" + strconv.Itoa(paymentNumber), Valid: true},
			})
			benchNoError(b, err)
		}
	})

	b.Run("LockPaymentAttempt", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.LockPaymentAttempt(env.ctx, int64(env.attempts[i%len(env.attempts)]))
			benchNoError(b, err)
		}
	})

	b.Run("UpdatePaymentAttemptStatus", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			err := q.UpdatePaymentAttemptStatus(env.ctx, paymentsqlc.UpdatePaymentAttemptStatusParams{
				Status: paymentsqlc.PaymentAttemptStatusSucceeded,
				ID:     int64(attemptID),
			})
			benchNoError(b, err)
		}
	})

	b.Run("MarkOrderPaid", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.MarkOrderPaid(env.ctx, int64(orderID))
			benchNoError(b, err)
		}
	})

	b.Run("MarkOrderPendingPayment", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.MarkOrderPendingPayment(env.ctx, int64(orderID))
			benchNoError(b, err)
		}
	})

	b.Run("MarkOrderFulfilled", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.MarkOrderFulfilled(env.ctx, int64(orderID))
			benchNoError(b, err)
		}
	})

	b.Run("CreatePaymentEvent", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			seq := benchNextSeq()
			_, err := q.CreatePaymentEvent(env.ctx, benchmarkCreateEventParams(orderID, attemptID, benchRunValue("sqlc_event", seq)))
			benchNoError(b, err)
		}
	})

	b.Run("MarkPaymentEventProcessed", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			err := q.MarkPaymentEventProcessed(env.ctx, paymentsqlc.MarkPaymentEventProcessedParams{
				ProcessingStatus: paymentsqlc.PaymentEventProcessingStatusProcessed,
				ProcessingError:  sql.NullString{},
				ID:               int64(1),
			})
			benchNoError(b, err)
		}
	})

	b.Run("UpsertPaymentSubscription", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.UpsertPaymentSubscription(env.ctx, benchmarkUpsertSubscriptionParams(productA.id, orderID, attemptID, benchRunValue("sqlc_sub", uint64(i+1))))
			benchNoError(b, err)
		}
	})

	b.Run("UpdatePaymentSubscriptionStatus", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.UpdatePaymentSubscriptionStatusByProvider(env.ctx, paymentsqlc.UpdatePaymentSubscriptionStatusByProviderParams{
				ProviderCode:           "vkma",
				ProviderSubscriptionID: "bench_sub_1",
				Status:                 paymentsqlc.PaymentSubscriptionStatusActive,
			})
			benchNoError(b, err)
		}
	})

	b.Run("GetPaymentSubscriptionByProviderID", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.GetPaymentSubscriptionByProviderID(env.ctx, paymentsqlc.GetPaymentSubscriptionByProviderIDParams{
				ProviderCode:           "vkma",
				ProviderSubscriptionID: "bench_sub_" + strconv.Itoa((i%1000)+1),
			})
			benchNoError(b, err)
		}
	})

	b.Run("CountActivePaymentSubscriptions", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.CountActivePaymentSubscriptionsForProvider(env.ctx, paymentsqlc.CountActivePaymentSubscriptionsForProviderParams{
				PlatformID:     1,
				PlatformUserID: "bench_user_" + strconv.Itoa(i%100_000),
				WorkspaceID:    benchWorkspaceID,
				EndedAt:        sql.NullTime{Time: time.Now(), Valid: true},
				ProviderCode:   "vkma",
			})
			benchNoError(b, err)
		}
	})

	b.Run("CreateFulfillment", func(b *testing.B) {
		orderIDs := make([]uint64, b.N)
		attemptIDs := make([]uint64, b.N)
		b.StopTimer()
		for i := 0; i < b.N; i++ {
			seq := benchNextSeq()
			order := createBenchmarkOrder(b, env, productA.id, "RUB", benchRunValue("sqlc_fulfillment_order", seq))
			attemptID, err := q.CreatePaymentAttempt(env.ctx, benchmarkCreateAttemptParams(order.ID, benchRunValue("sqlc_fulfillment_attempt", seq)))
			benchNoError(b, err)
			orderIDs[i] = order.ID
			attemptIDs[i] = uint64(attemptID)
		}
		b.StartTimer()
		for i := 0; i < b.N; i++ {
			_, err := q.CreateFulfillment(env.ctx, paymentsqlc.CreateFulfillmentParams{
				OrderID:   int64(orderIDs[i]),
				AttemptID: int64(attemptIDs[i]),
				Status:    paymentsqlc.PaymentFulfillmentStatusSucceeded,
			})
			benchNoError(b, err)
		}
	})

	b.Run("CreateFulfillmentItem", func(b *testing.B) {
		fulfillmentIDs := make([]uint64, b.N)
		b.StopTimer()
		for i := 0; i < b.N; i++ {
			fulfillmentIDs[i] = createBenchmarkFulfillment(b, env, productA.id)
		}
		b.StartTimer()
		for i := 0; i < b.N; i++ {
			item := env.products[(i+10)%len(env.products)].itemID
			err := q.CreateFulfillmentItem(env.ctx, paymentsqlc.CreateFulfillmentItemParams{
				FulfillmentID: int64(fulfillmentIDs[i]),
				WorkspaceID:   benchWorkspaceID,
				ItemID:        item,
				RewardType:    paymentsqlc.PaymentFulfillmentItemRewardTypeQuantity,
				Quantity:      1,
			})
			benchNoError(b, err)
		}
	})

	b.Run("GetFulfillmentItemsForProduct", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := q.GetFulfillmentItemsForProduct(env.ctx, paymentsqlc.GetFulfillmentItemsForProductParams{
				WorkspaceID: benchWorkspaceID,
				ProductID:   env.products[i%len(env.products)].id,
			})
			benchNoError(b, err)
		}
	})
}

func setupPaymentBenchmark(b *testing.B) paymentBenchmarkEnv {
	b.Helper()

	paymentBenchOnce.Do(func() {
		paymentBenchEnv, paymentBenchErr = buildPaymentBenchmarkEnv(b)
	})
	if paymentBenchErr != nil {
		b.Fatal(paymentBenchErr)
	}
	return paymentBenchEnv
}

func paymentBenchmarkDatabaseExists(ctx context.Context, db *sql.DB, dbName string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM information_schema.schemata
WHERE schema_name = ?`, dbName).Scan(&count)
	if err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}
	return false, nil
}

func buildPaymentBenchmarkEnv(b *testing.B) (paymentBenchmarkEnv, error) {
	b.Helper()

	dsn := paymentTestDSN(b)
	dbName := benchDBName

	ctx := context.Background()

	adminDB, err := openMySQL(dsn, "")
	if err != nil {
		return paymentBenchmarkEnv{}, err
	}
	defer adminDB.Close()

	shouldSeedDatabase := benchSeedDatabase
	if benchSeedDatabase {
		if err := recreatePaymentTestDatabase(ctx, adminDB, dbName); err != nil {
			return paymentBenchmarkEnv{}, err
		}
	} else {
		exists, err := paymentBenchmarkDatabaseExists(ctx, adminDB, dbName)
		if err != nil {
			return paymentBenchmarkEnv{}, err
		}
		if !exists {
			if err := recreatePaymentTestDatabase(ctx, adminDB, dbName); err != nil {
				return paymentBenchmarkEnv{}, err
			}
			shouldSeedDatabase = true
			b.Logf("created benchmark database %s because it did not exist", dbName)
		}
	}

	appDB, err := openMySQL(dsn, dbName)
	if err != nil {
		return paymentBenchmarkEnv{}, err
	}
	appDB.SetMaxOpenConns(32)
	appDB.SetMaxIdleConns(32)

	if !shouldSeedDatabase {
		needsReset, err := benchmarkSchemaNeedsReset(ctx, appDB, dbName)
		if err != nil {
			appDB.Close()
			return paymentBenchmarkEnv{}, err
		}
		if needsReset {
			appDB.Close()
			if err := recreatePaymentTestDatabase(ctx, adminDB, dbName); err != nil {
				return paymentBenchmarkEnv{}, err
			}
			appDB, err = openMySQL(dsn, dbName)
			if err != nil {
				return paymentBenchmarkEnv{}, err
			}
			appDB.SetMaxOpenConns(32)
			appDB.SetMaxIdleConns(32)
			shouldSeedDatabase = true
			b.Logf("recreated benchmark database %s because schema is outdated", dbName)
		}
	}

	client, err := sqlwrap.New(appDB, paymentTestSQLOptions())
	if err != nil {
		appDB.Close()
		return paymentBenchmarkEnv{}, err
	}

	env := paymentBenchmarkEnv{
		ctx: ctx,
		db:  appDB,
	}

	productCount := benchProductCount
	transactionCount := benchTransactionCount
	batchSize := benchBatchSize

	repo := repository.NewPaymentRepository(client)
	if shouldSeedDatabase {
		if err := repo.Bootstrap(ctx, filepath.Join("sqlc", "schema.sql")); err != nil {
			appDB.Close()
			return paymentBenchmarkEnv{}, err
		}
	}

	preparedQ, err := paymentsqlc.Prepare(ctx, appDB)
	if err != nil {
		appDB.Close()
		return paymentBenchmarkEnv{}, err
	}
	env.q = preparedQ

	if !shouldSeedDatabase {
		b.Logf("using existing benchmark database %s", dbName)
		if err := loadBenchmarkDataset(ctx, env.q, &env); err != nil {
			appDB.Close()
			return paymentBenchmarkEnv{}, err
		}
		env.api, err = NewWithDatabase(ctx, appDB, paymentTestOptions())
		if err != nil {
			appDB.Close()
			return paymentBenchmarkEnv{}, err
		}
		return env, nil
	}

	b.Logf("seeding benchmark database %s: products=%d transactions=%d batch=%d", dbName, productCount, transactionCount, batchSize)

	products, keys, keyHashes, err := seedBenchmarkCatalog(ctx, env.q, productCount)
	if err != nil {
		appDB.Close()
		return paymentBenchmarkEnv{}, err
	}
	if err := rebuildBenchmarkProductCache(ctx, env.q); err != nil {
		appDB.Close()
		return paymentBenchmarkEnv{}, err
	}
	env.products = products
	env.keys = keys
	env.keyHashes = keyHashes

	orders, attempts, err := seedBenchmarkTransactions(ctx, appDB, products, transactionCount, batchSize)
	if err != nil {
		appDB.Close()
		return paymentBenchmarkEnv{}, err
	}
	env.orders = orders
	env.attempts = attempts
	env.api, err = NewWithDatabase(ctx, appDB, paymentTestOptions())
	if err != nil {
		appDB.Close()
		return paymentBenchmarkEnv{}, err
	}

	return env, nil
}

func seedBenchmarkCatalog(ctx context.Context, q *paymentsqlc.Queries, count int) ([]benchProduct, []string, []string, error) {
	now := time.Now()
	availableFrom := now.Add(-24 * time.Hour)
	availableUntil := now.Add(365 * 24 * time.Hour)
	priceStarts := now.Add(-24 * time.Hour)
	priceEnds := now.Add(365 * 24 * time.Hour)

	if err := q.UpsertProductGroup(ctx, paymentsqlc.UpsertProductGroupParams{
		WorkspaceID: benchWorkspaceID,
		Code:        "bench_group_0",
		TitleKey:    sql.NullString{String: "bench.group.title", Valid: true},
		IsActive:    true,
	}); err != nil {
		return nil, nil, nil, err
	}
	for _, locale := range benchLocales {
		if err := q.UpsertLocalization(ctx, paymentsqlc.UpsertLocalizationParams{
			WorkspaceID:     benchWorkspaceID,
			Locale:          locale,
			LocalizationKey: "bench.group.title",
			Value:           "Benchmark group " + locale,
		}); err != nil {
			return nil, nil, nil, err
		}
	}

	products := make([]benchProduct, 0, count)
	keys := make([]string, 0, count)
	keyHashes := make([]string, 0, count)
	for i := 0; i < count; i++ {
		productID := fmt.Sprintf("bench_product_%04d", i)
		itemID := fmt.Sprintf("bench_item_%04d", i)
		productTitleKey := productID + ".title"
		productDescriptionKey := productID + ".description"
		itemTitleKey := itemID + ".title"
		itemDescriptionKey := itemID + ".description"

		if err := q.UpsertProduct(ctx, paymentsqlc.UpsertProductParams{
			WorkspaceID:    benchWorkspaceID,
			ID:             productID,
			GroupCode:      sql.NullString{String: "bench_group_0", Valid: true},
			TitleKey:       productTitleKey,
			DescriptionKey: sql.NullString{String: productDescriptionKey, Valid: true},
			Target:         pqtype.NullRawMessage{RawMessage: []byte("{}"), Valid: true},
			ImageUrl:       sql.NullString{String: "https://example.com/bench.png", Valid: true},
			Position:       int32(i),
			QuantityMode:   paymentsqlc.PaymentProductQuantityModeFixed,
			GlobalInterval: paymentsqlc.PaymentProductGlobalIntervalUNLIMITED,
			UserInterval:   paymentsqlc.PaymentProductUserIntervalUNLIMITED,
			AvailableFrom:  availableFrom,
			AvailableUntil: availableUntil,
			IsVisible:      true,
		}); err != nil {
			return nil, nil, nil, err
		}
		if err := q.UpsertProductItem(ctx, paymentsqlc.UpsertProductItemParams{
			WorkspaceID: benchWorkspaceID,
			ProductID:   productID,
			ItemID:      itemID,
			RewardType:  paymentsqlc.PaymentProductItemRewardTypeQuantity,
			Quantity:    int64((i % 5) + 1),
		}); err != nil {
			return nil, nil, nil, err
		}

		for _, locale := range benchLocales {
			for _, entry := range []struct {
				key   string
				value string
			}{
				{productTitleKey, fmt.Sprintf("Product %04d %s", i, locale)},
				{productDescriptionKey, fmt.Sprintf("Product %04d description %s", i, locale)},
				{itemTitleKey, fmt.Sprintf("Item %04d %s", i, locale)},
				{itemDescriptionKey, fmt.Sprintf("Item %04d description %s", i, locale)},
			} {
				if err := q.UpsertLocalization(ctx, paymentsqlc.UpsertLocalizationParams{
					WorkspaceID:     benchWorkspaceID,
					Locale:          locale,
					LocalizationKey: entry.key,
					Value:           entry.value,
				}); err != nil {
					return nil, nil, nil, err
				}
			}
		}

		priceIDs := make(map[string]uint64, len(benchAssets))
		for assetIndex, asset := range benchAssets {
			id, err := q.CreateProductPrice(ctx, paymentsqlc.CreateProductPriceParams{
				WorkspaceID:         benchWorkspaceID,
				ProductID:           productID,
				AssetCode:           asset,
				ListAmountMinor:     int64(1000 + i + assetIndex*100),
				DiscountAmountMinor: int64(assetIndex % 3),
				StartsAt:            priceStarts,
				EndsAt:              priceEnds,
			})
			if err != nil {
				return nil, nil, nil, err
			}
			priceIDs[asset] = uint64(id)
		}
		if i == 1 && priceIDs["VOTE"] == 0 {
			id, err := q.CreateProductPrice(ctx, paymentsqlc.CreateProductPriceParams{
				WorkspaceID:         benchWorkspaceID,
				ProductID:           productID,
				AssetCode:           "VOTE",
				ListAmountMinor:     int64(1000 + i + len(benchAssets)*100),
				DiscountAmountMinor: 0,
				StartsAt:            priceStarts,
				EndsAt:              priceEnds,
			})
			if err != nil {
				return nil, nil, nil, err
			}
			priceIDs["VOTE"] = uint64(id)
		}

		key := "bench_key_" + strconv.Itoa(i)
		keyHash := benchHash(key)
		if _, err := q.CreatePurchaseKey(ctx, paymentsqlc.CreatePurchaseKeyParams{
			WorkspaceID:    benchWorkspaceID,
			KeyHash:        keyHash,
			AppID:          7001,
			PlatformID:     1,
			PlatformUserID: "bench_recipient_" + strconv.Itoa(i),
			ProductID:      productID,
			MaxUses:        1_000_000_000,
		}); err != nil {
			return nil, nil, nil, err
		}

		products = append(products, benchProduct{id: productID, itemID: itemID, priceIDs: priceIDs})
		keys = append(keys, key)
		keyHashes = append(keyHashes, keyHash)
	}

	return products, keys, keyHashes, nil
}

func loadBenchmarkDataset(ctx context.Context, q *paymentsqlc.Queries, env *paymentBenchmarkEnv) error {
	products, err := loadBenchmarkProducts(ctx, env.db)
	if err != nil {
		return err
	}
	if len(products) == 0 {
		return fmt.Errorf("benchmark database %s has no products for workspace %s; set benchSeedDatabase=true first", benchDBName, benchWorkspaceID)
	}

	keys := make([]string, 0, len(products))
	keyHashes := make([]string, 0, len(products))
	for i := range products {
		key := "bench_key_" + strconv.Itoa(i)
		keys = append(keys, key)
		keyHashes = append(keyHashes, benchHash(key))
	}
	if _, err := q.GetPurchaseKeyByHash(ctx, keyHashes[0]); err != nil {
		return fmt.Errorf("benchmark database %s has no expected purchase keys; set benchSeedDatabase=true first: %w", benchDBName, err)
	}
	if err := ensureBenchmarkProductCache(ctx, q); err != nil {
		return err
	}

	orders, err := loadBenchmarkIDs(ctx, env.db, "payment_order", "id", "workspace_id = ?", benchWorkspaceID)
	if err != nil {
		return err
	}
	attempts, err := loadBenchmarkIDs(ctx, env.db, "payment_attempt", "id", "id < 1000000000")
	if err != nil {
		return err
	}
	if len(orders) == 0 || len(attempts) == 0 {
		return fmt.Errorf("benchmark database %s has no seeded orders or attempts; set benchSeedDatabase=true first", benchDBName)
	}

	env.products = products
	env.keys = keys
	env.keyHashes = keyHashes
	env.orders = orders
	env.attempts = attempts
	return nil
}

func ensureBenchmarkProductCache(ctx context.Context, q *paymentsqlc.Queries) error {
	rows, err := q.GetProductRows(ctx, paymentsqlc.GetProductRowsParams{
		ProductID:   "bench_product_0000",
		WorkspaceID: benchWorkspaceID,
		AssetCode:   "RUB",
		Locale:      "ru",
	})
	if err == nil && len(rows) > 0 {
		return nil
	}
	return rebuildBenchmarkProductCache(ctx, q)
}

func benchmarkSchemaNeedsReset(ctx context.Context, db *sql.DB, dbName string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM information_schema.columns
WHERE table_schema = ?
  AND (
      (table_name = 'payment_event' AND column_name = 'payload')
   OR (table_name = 'payment_event' AND column_name = 'headers')
   OR (table_name = 'payment_attempt' AND column_name = 'request_payload')
   OR (table_name = 'payment_attempt' AND column_name = 'response_payload')
   OR (table_name = 'payment_attempt' AND column_name = 'metadata')
   OR (table_name = 'payment_order' AND column_name = 'product_snapshot')
   OR (table_name = 'payment_order' AND column_name = 'metadata')
   OR (table_name = 'payment_subscription' AND column_name = 'metadata')
   OR (table_name = 'payment_price' AND column_name = 'metadata')
   OR (table_name = 'payment_product' AND column_name = 'metadata')
   OR (table_name = 'payment_purchase_key' AND column_name = 'metadata')
   OR (table_name = 'payment_fulfillment_item' AND column_name = 'metadata')
  )`, dbName).Scan(&count)
	if err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}

	err = db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM information_schema.statistics
WHERE table_schema = ?
  AND table_name = 'payment_order'
  AND index_name IN (
      'payment_order_product_paid_idx',
      'payment_order_global_limit_idx',
      'payment_order_user_limit_idx'
  )`, dbName).Scan(&count)
	if err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}

	err = db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM (
    SELECT 'payment_provider_asset' AS table_name, 'payment_provider_asset_asset_active_idx' AS index_name
    UNION ALL SELECT 'payment_product', 'payment_product_workspace_window_idx'
    UNION ALL SELECT 'payment_price', 'payment_price_current_idx'
    UNION ALL SELECT 'payment_order_item', 'PRIMARY'
    UNION ALL SELECT 'payment_paid_order_index', 'payment_paid_order_global_window_idx'
    UNION ALL SELECT 'payment_paid_order_index', 'payment_paid_order_user_window_idx'
    UNION ALL SELECT 'payment_product_limit_counter', 'PRIMARY'
    UNION ALL SELECT 'payment_provider_cursor', 'PRIMARY'
    UNION ALL SELECT 'payment_provider_transaction', 'payment_provider_transaction_external_uq'
    UNION ALL SELECT 'payment_provider_transaction', 'payment_provider_transaction_sequence_idx'
    UNION ALL SELECT 'payment_stats_order_event', 'payment_stats_order_event_workspace_idx'
    UNION ALL SELECT 'payment_stats_daily_overview', 'PRIMARY'
    UNION ALL SELECT 'payment_stats_daily_buyer', 'PRIMARY'
    UNION ALL SELECT 'payment_subscription', 'payment_subscription_active_idx'
    UNION ALL SELECT 'payment_subscription', 'payment_subscription_active_product_idx'
    UNION ALL SELECT 'payment_subscription', 'payment_subscription_active_provider_idx'
    UNION ALL SELECT 'payment_subscription', 'payment_subscription_active_product_provider_idx'
) required
LEFT JOIN information_schema.statistics existing
  ON existing.table_schema = ?
 AND existing.table_name = required.table_name
 AND existing.index_name = required.index_name
WHERE existing.index_name IS NULL`, dbName).Scan(&count)
	if err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}

	var productCount int
	var priceCount int
	err = db.QueryRowContext(ctx, `
SELECT
    (SELECT COUNT(*) FROM payment_product WHERE workspace_id = ?),
    (SELECT COUNT(*) FROM payment_price WHERE workspace_id = ?)`,
		benchWorkspaceID, benchWorkspaceID).Scan(&productCount, &priceCount)
	if err != nil {
		return false, err
	}
	if productCount > 0 && priceCount < productCount*len(benchAssets) {
		return true, nil
	}

	err = db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM payment_attempt
WHERE id = 1000000001
  AND provider_code = 'vkma'
  AND provider_payment_id = '1'`).Scan(&count)
	if err != nil {
		return false, err
	}
	if count == 0 {
		return true, nil
	}

	err = db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM payment_order o
LEFT JOIN payment_order_item oi
  ON oi.order_id = o.id
WHERE o.workspace_id = ?
  AND oi.order_id IS NULL
LIMIT 1`, benchWorkspaceID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func rebuildBenchmarkProductCache(ctx context.Context, q *paymentsqlc.Queries) error {
	if _, err := q.DeleteWorkspaceProductCache(ctx, benchWorkspaceID); err != nil {
		return err
	}
	return q.RebuildWorkspaceProductCache(ctx, paymentsqlc.RebuildWorkspaceProductCacheParams{
		WorkspaceID:   benchWorkspaceID,
		WorkspaceID_2: benchWorkspaceID,
	})
}

func loadBenchmarkProducts(ctx context.Context, db *sql.DB) ([]benchProduct, error) {
	rows, err := db.QueryContext(ctx, `
SELECT
    p.id,
    COALESCE(MIN(pi.item_id), ''),
    COALESCE(MAX(CASE WHEN pp.asset_code = 'RUB' THEN pp.id END), 0),
    COALESCE(MAX(CASE WHEN pp.asset_code = 'VOTE' THEN pp.id END), 0),
    COALESCE(MAX(CASE WHEN pp.asset_code = 'TON' THEN pp.id END), 0),
    COALESCE(MAX(CASE WHEN pp.asset_code = 'USDT_TON' THEN pp.id END), 0),
    COALESCE(MAX(CASE WHEN pp.asset_code = 'MEMCOIN_TON' THEN pp.id END), 0),
    COALESCE(MAX(CASE WHEN pp.asset_code = 'XTR' THEN pp.id END), 0)
FROM payment_product p
LEFT JOIN payment_product_item pi
    ON pi.workspace_id = p.workspace_id
   AND pi.product_id = p.id
LEFT JOIN payment_price pp
    ON pp.workspace_id = p.workspace_id
   AND pp.product_id = p.id
WHERE p.workspace_id = ?
GROUP BY p.id
ORDER BY p.id
LIMIT ?`, benchWorkspaceID, benchProductCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	products := make([]benchProduct, 0, benchProductCount)
	for rows.Next() {
		product := benchProduct{priceIDs: make(map[string]uint64, len(benchAssets))}
		var rubID, voteID, tonID, usdtID, memcoinID, xtrID uint64
		if err := rows.Scan(&product.id, &product.itemID, &rubID, &voteID, &tonID, &usdtID, &memcoinID, &xtrID); err != nil {
			return nil, err
		}
		product.priceIDs["RUB"] = rubID
		product.priceIDs["VOTE"] = voteID
		product.priceIDs["TON"] = tonID
		product.priceIDs["USDT_TON"] = usdtID
		product.priceIDs["MEMCOIN_TON"] = memcoinID
		product.priceIDs["XTR"] = xtrID
		for _, asset := range benchAssets {
			if product.priceIDs[asset] == 0 {
				return nil, fmt.Errorf("benchmark product %s has no %s price; set benchSeedDatabase=true first", product.id, asset)
			}
		}
		products = append(products, product)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return products, nil
}

func loadBenchmarkIDs(ctx context.Context, db *sql.DB, table string, column string, where string, args ...any) ([]uint64, error) {
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT 10000", column, table, where, column)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]uint64, 0, 10000)
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func seedBenchmarkTransactions(ctx context.Context, db *sql.DB, products []benchProduct, count int, batchSize int) ([]uint64, []uint64, error) {
	orders := make([]uint64, minInt(count, 10_000))
	attempts := make([]uint64, minInt(count, 10_000))

	if err := bulkInsertBenchmarkOrders(ctx, db, products, count, batchSize); err != nil {
		return nil, nil, err
	}
	for i := range orders {
		orders[i] = uint64(i + 1)
	}
	if err := bulkInsertBenchmarkOrderItems(ctx, db, products, count, batchSize); err != nil {
		return nil, nil, err
	}

	if err := bulkInsertBenchmarkAttempts(ctx, db, count, batchSize); err != nil {
		return nil, nil, err
	}
	for i := range attempts {
		attempts[i] = uint64(i + 1)
	}

	eventCount := minInt(count, 10_000)
	if err := bulkInsertBenchmarkEvents(ctx, db, eventCount, batchSize); err != nil {
		return nil, nil, err
	}
	subscriptionCount := maxInt(1000, count/20)
	if subscriptionCount > count {
		subscriptionCount = count
	}
	if err := bulkInsertBenchmarkSubscriptions(ctx, db, products, subscriptionCount, batchSize); err != nil {
		return nil, nil, err
	}
	if err := seedBenchmarkFulfilledAttempt(ctx, db, products[1]); err != nil {
		return nil, nil, err
	}

	return orders, attempts, nil
}

func bulkInsertBenchmarkOrders(ctx context.Context, db *sql.DB, products []benchProduct, count int, batchSize int) error {
	columns := `(public_id, workspace_id, app_id, platform_id, platform_user_id, internal_user_id, payer_platform_id, payer_platform_user_id, payer_internal_user_id, purchase_key_id, product_id, price_id, asset_code, locale, list_amount_minor, discount_amount_minor, payable_amount_minor, status, reserved_until, paid_at, fulfilled_at, canceled_at, expires_at)`
	return bulkInsert(ctx, db, "payment_order", columns, count, batchSize, func(i int) string {
		product := products[(i-1)%len(products)]
		asset := benchAssets[(i-1)%len(benchAssets)]
		priceID := product.priceIDs[asset]
		status := "paid"
		fulfilledAt := "NULL"
		if i%3 == 0 {
			status = "fulfilled"
			fulfilledAt = "now()"
		}
		return fmt.Sprintf(
			"('bench-public-%d','%s',7001,1,'bench_user_%d',NULL,NULL,NULL,NULL,NULL,'%s',%d,'%s','%s',1000,0,1000,'%s',NULL,now(),%s,NULL,NULL)",
			i, benchWorkspaceID, i%100_000, product.id, priceID, asset, benchLocales[i%len(benchLocales)], status, fulfilledAt,
		)
	})
}

func bulkInsertBenchmarkOrderItems(ctx context.Context, db *sql.DB, products []benchProduct, count int, batchSize int) error {
	columns := `(order_id, workspace_id, item_id, quantity)`
	return bulkInsert(ctx, db, "payment_order_item", columns, count, batchSize, func(i int) string {
		product := products[(i-1)%len(products)]
		return fmt.Sprintf(
			"(%d,'%s','%s',1)",
			i, benchWorkspaceID, product.itemID,
		)
	})
}

func bulkInsertBenchmarkAttempts(ctx context.Context, db *sql.DB, count int, batchSize int) error {
	columns := `(order_id, workspace_id, provider_code, asset_code, amount_minor, status, provider_payment_id, provider_invoice_id, provider_charge_id, provider_subscription_id, idempotency_key, confirmation_url, return_url, expires_at)`
	return bulkInsert(ctx, db, "payment_attempt", columns, count, batchSize, func(i int) string {
		asset := benchAssets[(i-1)%len(benchAssets)]
		provider := providerForAsset(asset)
		return fmt.Sprintf(
			"(%d,'%s','%s','%s',1000,'succeeded','bench_pay_%d',NULL,NULL,NULL,'bench_idem_%d',NULL,NULL,NULL)",
			i, benchWorkspaceID, provider, asset, i, i,
		)
	})
}

func bulkInsertBenchmarkEvents(ctx context.Context, db *sql.DB, count int, batchSize int) error {
	columns := `(workspace_id, provider_code, attempt_id, order_id, provider_event_id, provider_payment_id, event_type, event_status, payload_hash, signature_valid, processing_status, processed_at)`
	return bulkInsert(ctx, db, "payment_event", columns, count, batchSize, func(i int) string {
		return fmt.Sprintf(
			"('%s','yookassa',%d,%d,'bench_event_%d','bench_pay_%d','succeeded','succeeded','%064x',true,'processed',now())",
			benchWorkspaceID, i, i, i, i, i,
		)
	})
}

func bulkInsertBenchmarkSubscriptions(ctx context.Context, db *sql.DB, products []benchProduct, count int, batchSize int) error {
	columns := `(workspace_id, provider_code, provider_subscription_id, app_id, platform_id, platform_user_id, internal_user_id, product_id, order_id, attempt_id, status, cancel_reason, started_at, ended_at)`
	return bulkInsert(ctx, db, "payment_subscription", columns, count, batchSize, func(i int) string {
		product := products[(i-1)%len(products)]
		return fmt.Sprintf(
			"('%s','vkma','bench_sub_%d',7001,1,'bench_user_%d',NULL,'%s',%d,%d,'active',NULL,now(),NULL)",
			benchWorkspaceID, i, i%100_000, product.id, i, i,
		)
	})
}

func seedBenchmarkFulfilledAttempt(ctx context.Context, db *sql.DB, product benchProduct) error {
	votePriceID := product.priceIDs["VOTE"]
	if votePriceID == 0 {
		var id uint64
		err := db.QueryRowContext(ctx, `
INSERT INTO payment_price (workspace_id, product_id, asset_code, list_amount_minor, discount_amount_minor, starts_at, ends_at)
VALUES ($1, $2, 'VOTE', 1000, 0, now() - INTERVAL '1 day', now() + INTERVAL '365 days')
RETURNING id`,
			benchWorkspaceID, product.id).Scan(&id)
		if err != nil {
			return err
		}
		votePriceID = id
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO payment_order (id, public_id, workspace_id, app_id, platform_id, platform_user_id, product_id, quantity, price_id, asset_code, locale, list_amount_minor, discount_amount_minor, payable_amount_minor, status, paid_at, fulfilled_at)
VALUES (1000000001, 'bench-vkma-public-1', $1, 7001, 1, '9000', $2, 1, $3, 'VOTE', 'ru', 1000, 0, 1000, 'fulfilled', now(), now())`,
		benchWorkspaceID, product.id, votePriceID); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO payment_attempt (id, order_id, workspace_id, provider_code, asset_code, amount_minor, status, provider_payment_id, idempotency_key)
VALUES (1000000001, 1000000001, $1, 'vkma', 'VOTE', 1000, 'succeeded', '1', 'vkma:1')`,
		benchWorkspaceID); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO payment_order_item (order_id, workspace_id, item_id, quantity)
VALUES (1000000001, $1, $2, 1)`,
		benchWorkspaceID, product.itemID); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO payment_subscription (workspace_id, provider_code, provider_subscription_id, app_id, platform_id, platform_user_id, product_id, order_id, attempt_id, status, started_at)
VALUES ($1, 'vkma', '1', 7001, 1, '9000', $2, 1000000001, 1000000001, 'active', now())`,
		benchWorkspaceID, product.id); err != nil {
		return err
	}
	return nil
}

func bulkInsert(ctx context.Context, db *sql.DB, table string, columns string, count int, batchSize int, value func(int) string) error {
	for start := 1; start <= count; start += batchSize {
		end := start + batchSize - 1
		if end > count {
			end = count
		}
		values := make([]string, 0, end-start+1)
		for i := start; i <= end; i++ {
			values = append(values, value(i))
		}
		query := fmt.Sprintf("INSERT INTO %s %s VALUES %s", table, columns, strings.Join(values, ","))
		if _, err := db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("bulk insert %s rows %d-%d: %w", table, start, end, err)
		}
	}
	return nil
}

func createBenchmarkOrder(b *testing.B, env paymentBenchmarkEnv, productID string, assetCode string, userID string) *checkout.Order {
	b.Helper()
	order, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
		Identity:  paymentTestIdentity(benchWorkspaceID, 7001, 1, userID),
		ProductID: productID,
		AssetCode: assetCode,
		Locale:    "ru",
	})
	benchNoError(b, err)
	return order
}

func createBenchmarkFulfillment(b *testing.B, env paymentBenchmarkEnv, productID string) uint64 {
	b.Helper()
	seq := benchNextSeq()
	order := createBenchmarkOrder(b, env, productID, "RUB", benchRunValue("bench_fulfillment_user", seq))
	attemptID, err := env.q.CreatePaymentAttempt(env.ctx, benchmarkCreateAttemptParams(order.ID, benchRunValue("bench_fulfillment_seed", seq)))
	benchNoError(b, err)
	fulfillmentID, err := env.q.CreateFulfillment(env.ctx, paymentsqlc.CreateFulfillmentParams{
		OrderID:   int64(order.ID),
		AttemptID: attemptID,
		Status:    paymentsqlc.PaymentFulfillmentStatusSucceeded,
	})
	benchNoError(b, err)
	return uint64(fulfillmentID)
}

func benchmarkUpsertProductParams(id string, groupCode string) paymentsqlc.UpsertProductParams {
	return paymentsqlc.UpsertProductParams{
		WorkspaceID:    benchWorkspaceID,
		ID:             id,
		GroupCode:      sql.NullString{String: groupCode, Valid: true},
		TitleKey:       id + ".title",
		DescriptionKey: sql.NullString{String: id + ".description", Valid: true},
		Target:         pqtype.NullRawMessage{RawMessage: []byte("{}"), Valid: true},
		Position:       1,
		QuantityMode:   paymentsqlc.PaymentProductQuantityModeFixed,
		GlobalInterval: paymentsqlc.PaymentProductGlobalIntervalUNLIMITED,
		UserInterval:   paymentsqlc.PaymentProductUserIntervalUNLIMITED,
		AvailableFrom:  time.Now().Add(-24 * time.Hour),
		AvailableUntil: time.Now().Add(365 * 24 * time.Hour),
		IsVisible:      true,
	}
}

func benchmarkCreatePriceParams(productID string, startsAt time.Time) paymentsqlc.CreateProductPriceParams {
	return paymentsqlc.CreateProductPriceParams{
		WorkspaceID:         benchWorkspaceID,
		ProductID:           productID,
		AssetCode:           "RUB",
		ListAmountMinor:     1000,
		DiscountAmountMinor: 0,
		StartsAt:            startsAt,
		EndsAt:              startsAt.Add(time.Hour),
	}
}

func benchPriceStart(seq uint64) time.Time {
	base := time.Unix(int64(1_800_000_000+(paymentBenchRunNumber%10_000_000)), 0).UTC()
	return base.Add(time.Duration(seq) * time.Second)
}

func benchmarkCreateOrderParams(product benchProduct, assetCode string, priceID uint64, publicID string) paymentsqlc.CreatePaymentOrderParams {
	return paymentsqlc.CreatePaymentOrderParams{
		PublicID:           publicID,
		WorkspaceID:        benchWorkspaceID,
		AppID:              7001,
		PlatformID:         1,
		PlatformUserID:     "bench_sqlc_order_user",
		ProductID:          product.id,
		Quantity:           1,
		PriceID:            int64(priceID),
		AssetCode:          assetCode,
		Locale:             "ru",
		ListAmountMinor:    1000,
		PayableAmountMinor: 1000,
		Status:             paymentsqlc.PaymentOrderStatusDraft,
	}
}

func benchmarkCreateAttemptParams(orderID uint64, providerPaymentID string) paymentsqlc.CreatePaymentAttemptParams {
	return paymentsqlc.CreatePaymentAttemptParams{
		OrderID:           int64(orderID),
		ProviderCode:      "yookassa",
		AssetCode:         "RUB",
		AmountMinor:       int64(1000),
		Status:            paymentsqlc.PaymentAttemptStatusPending,
		ProviderPaymentID: sql.NullString{String: providerPaymentID, Valid: true},
		IdempotencyKey:    sql.NullString{String: providerPaymentID + ":idem", Valid: true},
	}
}

func benchmarkCreateEventParams(orderID uint64, attemptID uint64, eventID string) paymentsqlc.CreatePaymentEventParams {
	return paymentsqlc.CreatePaymentEventParams{
		WorkspaceID:       benchWorkspaceID,
		ProviderCode:      "yookassa",
		AttemptID:         sql.NullInt64{Int64: int64(attemptID), Valid: true},
		OrderID:           sql.NullInt64{Int64: int64(orderID), Valid: true},
		ProviderEventID:   sql.NullString{String: eventID, Valid: true},
		ProviderPaymentID: sql.NullString{String: eventID + "_payment", Valid: true},
		EventType:         "succeeded",
		EventStatus:       sql.NullString{String: "succeeded", Valid: true},
		PayloadHash:       benchHash(eventID),
		SignatureValid:    sql.NullBool{Bool: true, Valid: true},
	}
}

func benchmarkUpsertSubscriptionParams(productID string, orderID uint64, attemptID uint64, providerSubscriptionID string) paymentsqlc.UpsertPaymentSubscriptionParams {
	return paymentsqlc.UpsertPaymentSubscriptionParams{
		WorkspaceID:            benchWorkspaceID,
		ProviderCode:           "vkma",
		ProviderSubscriptionID: providerSubscriptionID,
		AppID:                  7001,
		PlatformID:             1,
		PlatformUserID:         "bench_subscription_user",
		ProductID:              productID,
		OrderID:                sql.NullInt64{Int64: int64(orderID), Valid: true},
		AttemptID:              sql.NullInt64{Int64: int64(attemptID), Valid: true},
		Status:                 paymentsqlc.PaymentSubscriptionStatusActive,
		StartedAt:              time.Now(),
	}
}

func providerForAsset(asset string) string {
	switch asset {
	case "VOTE":
		return "vkma"
	case "XTR":
		return "telegram_stars"
	case "TON", "USDT_TON", "MEMCOIN_TON":
		return "ton"
	default:
		return "yookassa"
	}
}

func benchHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func benchNextSeq() uint64 {
	return atomic.AddUint64(&paymentBenchSeq, 1)
}

func benchRunValue(prefix string, seq uint64) string {
	return prefix + "_" + paymentBenchRunID + "_" + strconv.FormatUint(seq, 10)
}

func benchNoError(b *testing.B, err error) {
	b.Helper()
	if err != nil {
		b.Fatal(err)
	}
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func BenchmarkPaymentCompleteAttemptBreakdown(b *testing.B) {
	env := setupPaymentBenchmark(b)

	b.Run("unlimited", func(b *testing.B) {
		benchmarkCompleteAttemptBreakdown(b, env, env.products[0].id, false)
	})
}

type completeAttemptBreakdownTotals struct {
	beginTx                time.Duration
	lockAttempt            time.Duration
	lockOrder              time.Duration
	updateAttempt          time.Duration
	markOrderPaid          time.Duration
	insertPaidOrderIndex   time.Duration
	getLimitConfig         time.Duration
	globalNow              time.Duration
	globalEnsureCounter    time.Duration
	globalIncrementCounter time.Duration
	userNow                time.Duration
	userEnsureCounter      time.Duration
	userIncrementCounter   time.Duration
	createFulfillment      time.Duration
	getFulfillmentItems    time.Duration
	createFulfillmentItems time.Duration
	markOrderFulfilled     time.Duration
	markPaidIndexFulfilled time.Duration
	rollback               time.Duration
}

func benchmarkCompleteAttemptBreakdown(b *testing.B, env paymentBenchmarkEnv, productID string, withLimits bool) {
	b.Helper()

	var totals completeAttemptBreakdownTotals
	b.ReportAllocs()

	b.StopTimer()
	seq := benchNextSeq()
	order := createBenchmarkOrder(b, env, productID, "RUB", benchRunValue("breakdown_user", seq))
	providerPaymentID := benchRunValue("breakdown_pay", seq)
	attemptID, err := env.q.CreatePaymentAttempt(env.ctx, benchmarkCreateAttemptParams(order.ID, providerPaymentID))
	benchNoError(b, err)
	b.StartTimer()

	for i := 0; i < b.N; i++ {
		start := time.Now()
		tx, err := env.db.BeginTx(env.ctx, nil)
		totals.beginTx += time.Since(start)
		benchNoError(b, err)

		qtx := env.q.WithTx(tx)

		start = time.Now()
		attempt, err := qtx.LockPaymentAttempt(env.ctx, attemptID)
		totals.lockAttempt += time.Since(start)
		benchNoError(b, err)

		start = time.Now()
		lockedOrder, err := qtx.LockPaymentOrder(env.ctx, attempt.OrderID)
		totals.lockOrder += time.Since(start)
		benchNoError(b, err)

		start = time.Now()
		err = qtx.UpdatePaymentAttemptStatus(env.ctx, paymentsqlc.UpdatePaymentAttemptStatusParams{
			Status: paymentsqlc.PaymentAttemptStatusSucceeded,
			ID:     attempt.ID,
		})
		totals.updateAttempt += time.Since(start)
		benchNoError(b, err)

		start = time.Now()
		_, err = qtx.MarkOrderPaid(env.ctx, lockedOrder.ID)
		totals.markOrderPaid += time.Since(start)
		benchNoError(b, err)

		start = time.Now()
		_, err = qtx.InsertPaidOrderIndexFromOrder(env.ctx, lockedOrder.ID)
		totals.insertPaidOrderIndex += time.Since(start)
		benchNoError(b, err)

		start = time.Now()
		config, err := qtx.GetProductLimitConfig(env.ctx, paymentsqlc.GetProductLimitConfigParams{
			WorkspaceID: lockedOrder.WorkspaceID,
			ID:          lockedOrder.ProductID,
		})
		totals.getLimitConfig += time.Since(start)
		benchNoError(b, err)

		if withLimits {
			globalStart, globalEnd := benchmarkLimitWindow(string(config.GlobalInterval), config.GlobalIntervalCount)

			start = time.Now()
			now, err := benchmarkDatabaseNowTx(env.ctx, tx)
			totals.globalNow += time.Since(start)
			benchNoError(b, err)
			_ = now

			start = time.Now()
			_, err = qtx.EnsureProductLimitCounter(env.ctx, paymentsqlc.EnsureProductLimitCounterParams{
				WorkspaceID:    lockedOrder.WorkspaceID,
				PlatformID:     lockedOrder.PlatformID,
				ProductID:      lockedOrder.ProductID,
				CounterScope:   paymentsqlc.PaymentProductLimitCounterCounterScopeGlobal,
				PlatformUserID: "",
				WindowStart:    globalStart,
				WindowEnd:      globalEnd,
			})
			totals.globalEnsureCounter += time.Since(start)
			benchNoError(b, err)

			start = time.Now()
			_, err = qtx.IncrementProductLimitCounter(env.ctx, paymentsqlc.IncrementProductLimitCounterParams{
				WorkspaceID:    lockedOrder.WorkspaceID,
				PlatformID:     lockedOrder.PlatformID,
				ProductID:      lockedOrder.ProductID,
				CounterScope:   paymentsqlc.PaymentProductLimitCounterCounterScopeGlobal,
				PlatformUserID: "",
				WindowStart:    globalStart,
				WindowEnd:      globalEnd,
				PaidCount:      int64(config.GlobalLimit),
			})
			totals.globalIncrementCounter += time.Since(start)
			benchNoError(b, err)

			userStart, userEnd := benchmarkLimitWindow(string(config.UserInterval), config.UserIntervalCount)

			start = time.Now()
			now, err = benchmarkDatabaseNowTx(env.ctx, tx)
			totals.userNow += time.Since(start)
			benchNoError(b, err)
			_ = now

			start = time.Now()
			_, err = qtx.EnsureProductLimitCounter(env.ctx, paymentsqlc.EnsureProductLimitCounterParams{
				WorkspaceID:    lockedOrder.WorkspaceID,
				PlatformID:     lockedOrder.PlatformID,
				ProductID:      lockedOrder.ProductID,
				CounterScope:   paymentsqlc.PaymentProductLimitCounterCounterScopeUser,
				PlatformUserID: lockedOrder.PlatformUserID,
				WindowStart:    userStart,
				WindowEnd:      userEnd,
			})
			totals.userEnsureCounter += time.Since(start)
			benchNoError(b, err)

			start = time.Now()
			_, err = qtx.IncrementProductLimitCounter(env.ctx, paymentsqlc.IncrementProductLimitCounterParams{
				WorkspaceID:    lockedOrder.WorkspaceID,
				PlatformID:     lockedOrder.PlatformID,
				ProductID:      lockedOrder.ProductID,
				CounterScope:   paymentsqlc.PaymentProductLimitCounterCounterScopeUser,
				PlatformUserID: lockedOrder.PlatformUserID,
				WindowStart:    userStart,
				WindowEnd:      userEnd,
				PaidCount:      int64(config.UserLimit),
			})
			totals.userIncrementCounter += time.Since(start)
			benchNoError(b, err)
		}

		start = time.Now()
		fulfillmentID, err := qtx.CreateFulfillment(env.ctx, paymentsqlc.CreateFulfillmentParams{
			OrderID:        lockedOrder.ID,
			AttemptID:      attempt.ID,
			InternalUserID: lockedOrder.InternalUserID,
			Status:         paymentsqlc.PaymentFulfillmentStatusSucceeded,
		})
		totals.createFulfillment += time.Since(start)
		benchNoError(b, err)

		start = time.Now()
		items, err := qtx.GetFulfillmentItemsForProduct(env.ctx, paymentsqlc.GetFulfillmentItemsForProductParams{
			WorkspaceID: lockedOrder.WorkspaceID,
			ProductID:   lockedOrder.ProductID,
		})
		totals.getFulfillmentItems += time.Since(start)
		benchNoError(b, err)

		for _, item := range items {
			start = time.Now()
			err = qtx.CreateFulfillmentItem(env.ctx, paymentsqlc.CreateFulfillmentItemParams{
				FulfillmentID: fulfillmentID,
				WorkspaceID:   lockedOrder.WorkspaceID,
				ItemID:        item.ItemID,
				RewardType:    paymentsqlc.PaymentFulfillmentItemRewardTypeQuantity,
				Quantity:      item.Quantity,
			})
			totals.createFulfillmentItems += time.Since(start)
			benchNoError(b, err)
		}

		start = time.Now()
		_, err = qtx.MarkOrderFulfilled(env.ctx, lockedOrder.ID)
		totals.markOrderFulfilled += time.Since(start)
		benchNoError(b, err)

		start = time.Now()
		_, err = qtx.MarkPaidOrderIndexFulfilled(env.ctx, lockedOrder.ID)
		totals.markPaidIndexFulfilled += time.Since(start)
		benchNoError(b, err)

		start = time.Now()
		err = tx.Rollback()
		totals.rollback += time.Since(start)
		benchNoError(b, err)
	}

	reportCompleteAttemptBreakdownMetric(b, "begin-tx", totals.beginTx, b.N)
	reportCompleteAttemptBreakdownMetric(b, "lock-attempt", totals.lockAttempt, b.N)
	reportCompleteAttemptBreakdownMetric(b, "lock-order", totals.lockOrder, b.N)
	reportCompleteAttemptBreakdownMetric(b, "update-attempt", totals.updateAttempt, b.N)
	reportCompleteAttemptBreakdownMetric(b, "mark-order-paid", totals.markOrderPaid, b.N)
	reportCompleteAttemptBreakdownMetric(b, "insert-paid-index", totals.insertPaidOrderIndex, b.N)
	reportCompleteAttemptBreakdownMetric(b, "get-limit-config", totals.getLimitConfig, b.N)
	reportCompleteAttemptBreakdownMetric(b, "global-now", totals.globalNow, b.N)
	reportCompleteAttemptBreakdownMetric(b, "global-ensure-counter", totals.globalEnsureCounter, b.N)
	reportCompleteAttemptBreakdownMetric(b, "global-increment-counter", totals.globalIncrementCounter, b.N)
	reportCompleteAttemptBreakdownMetric(b, "user-now", totals.userNow, b.N)
	reportCompleteAttemptBreakdownMetric(b, "user-ensure-counter", totals.userEnsureCounter, b.N)
	reportCompleteAttemptBreakdownMetric(b, "user-increment-counter", totals.userIncrementCounter, b.N)
	reportCompleteAttemptBreakdownMetric(b, "create-fulfillment", totals.createFulfillment, b.N)
	reportCompleteAttemptBreakdownMetric(b, "get-fulfillment-items", totals.getFulfillmentItems, b.N)
	reportCompleteAttemptBreakdownMetric(b, "create-fulfillment-items", totals.createFulfillmentItems, b.N)
	reportCompleteAttemptBreakdownMetric(b, "mark-order-fulfilled", totals.markOrderFulfilled, b.N)
	reportCompleteAttemptBreakdownMetric(b, "mark-paid-index-fulfilled", totals.markPaidIndexFulfilled, b.N)
	reportCompleteAttemptBreakdownMetric(b, "rollback", totals.rollback, b.N)
}

func benchmarkDatabaseNowTx(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	var now time.Time
	if err := tx.QueryRowContext(ctx, "SELECT NOW()").Scan(&now); err != nil {
		return time.Time{}, err
	}
	return now, nil
}

func benchmarkLimitWindow(interval string, intervalCount int32) (time.Time, time.Time) {
	now := time.Now()
	count := int(intervalCount)
	if count <= 0 {
		count = 1
	}
	anchor := time.Date(2024, 1, 1, 0, 0, 0, 0, now.Location())

	switch interval {
	case "DAY":
		return benchmarkFixedLimitWindow(anchor, now, time.Duration(count)*24*time.Hour)
	case "HOUR":
		return benchmarkFixedLimitWindow(anchor, now, time.Duration(count)*time.Hour)
	case "MINUTE":
		return benchmarkFixedLimitWindow(anchor, now, time.Duration(count)*time.Minute)
	case "SECOND":
		return benchmarkFixedLimitWindow(anchor, now, time.Duration(count)*time.Second)
	default:
		return benchmarkFixedLimitWindow(anchor, now, time.Duration(count)*24*time.Hour)
	}
}

func benchmarkFixedLimitWindow(anchor time.Time, now time.Time, span time.Duration) (time.Time, time.Time) {
	if span <= 0 {
		span = 24 * time.Hour
	}
	elapsed := now.Sub(anchor)
	index := elapsed / span
	start := anchor.Add(index * span)
	return start, start.Add(span)
}

func reportCompleteAttemptBreakdownMetric(b *testing.B, name string, total time.Duration, iterations int) {
	b.Helper()
	if total == 0 || iterations == 0 {
		return
	}
	b.ReportMetric(float64(total.Nanoseconds())/float64(iterations), name+"-ns/op")
}

const paymentConstraintBenchDB = "payment_fk_lab"

func BenchmarkPaymentConstraintImpact(b *testing.B) {
	env := setupPaymentConstraintBenchmark(b)

	b.Run("CreatePaymentAttempt/full", func(b *testing.B) {
		benchmarkPaymentAttemptInsertVariant(b, env, "payment_attempt_full")
	})

	b.Run("CreatePaymentAttempt/no_fk", func(b *testing.B) {
		benchmarkPaymentAttemptInsertVariant(b, env, "payment_attempt_no_fk")
	})

	b.Run("CreatePaymentAttempt/no_fk_no_secondary_indexes", func(b *testing.B) {
		benchmarkPaymentAttemptInsertVariant(b, env, "payment_attempt_heap")
	})

	b.Run("CreateFulfillment/full", func(b *testing.B) {
		benchmarkFulfillmentInsertVariant(b, env, "payment_fulfillment_full")
	})

	b.Run("CreateFulfillment/no_fk", func(b *testing.B) {
		benchmarkFulfillmentInsertVariant(b, env, "payment_fulfillment_no_fk")
	})

	b.Run("CreateFulfillment/no_fk_no_secondary_indexes", func(b *testing.B) {
		benchmarkFulfillmentInsertVariant(b, env, "payment_fulfillment_heap")
	})

	b.Run("MarkOrderFulfilled/full", func(b *testing.B) {
		benchmarkMarkOrderFulfilledVariant(b, env, "payment_order_full")
	})

	b.Run("MarkOrderFulfilled/no_secondary_indexes", func(b *testing.B) {
		benchmarkMarkOrderFulfilledVariant(b, env, "payment_order_heap")
	})
}

type paymentConstraintBenchEnv struct {
	ctx context.Context
	db  *sql.DB
}

func setupPaymentConstraintBenchmark(b *testing.B) paymentConstraintBenchEnv {
	b.Helper()

	ctx := context.Background()
	dsn := paymentTestDSN(b)

	adminDB, err := openMySQL(dsn, "")
	if err != nil {
		b.Fatalf("open admin mysql connection: %v", err)
	}
	defer adminDB.Close()

	if err := recreatePaymentTestDatabase(ctx, adminDB, paymentConstraintBenchDB); err != nil {
		b.Fatalf("recreate constraint benchmark database: %v", err)
	}

	appDB, err := openMySQL(dsn, paymentConstraintBenchDB)
	if err != nil {
		b.Fatalf("open benchmark mysql connection: %v", err)
	}
	b.Cleanup(func() {
		_ = appDB.Close()
	})

	env := paymentConstraintBenchEnv{
		ctx: ctx,
		db:  appDB,
	}
	preparePaymentConstraintBenchmarkSchema(b, env)
	return env
}

func preparePaymentConstraintBenchmarkSchema(b *testing.B, env paymentConstraintBenchEnv) {
	b.Helper()

	statements := []string{
		`CREATE TABLE payment_provider_parent (
			code VARCHAR(32) NOT NULL PRIMARY KEY
		)`,
		`CREATE TABLE payment_asset_parent (
			code VARCHAR(32) NOT NULL PRIMARY KEY
		)`,
		`CREATE TABLE payment_order_parent (
			id BIGINT NOT NULL PRIMARY KEY
		)`,
		`CREATE TABLE payment_attempt_parent (
			id BIGINT NOT NULL PRIMARY KEY,
			order_id BIGINT NOT NULL
		)`,
		`CREATE TABLE payment_attempt_full (
			id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
			order_id BIGINT NOT NULL,
			provider_code VARCHAR(32) NOT NULL,
			asset_code VARCHAR(32) NOT NULL,
			amount_minor BIGINT NOT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'created',
			provider_payment_id VARCHAR(128) NULL,
			provider_invoice_id VARCHAR(128) NULL,
			provider_charge_id VARCHAR(128) NULL,
			provider_subscription_id VARCHAR(128) NULL,
			idempotency_key VARCHAR(128) NULL,
			confirmation_url TEXT NULL,
			return_url TEXT NULL,
			expires_at TIMESTAMPTZ NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT payment_attempt_idempotency_uq UNIQUE (idempotency_key),
			CONSTRAINT payment_attempt_provider_payment_uq UNIQUE (provider_code, provider_payment_id),
			CONSTRAINT payment_attempt_provider_charge_uq UNIQUE (provider_code, provider_charge_id),
			CONSTRAINT payment_attempt_order_fk FOREIGN KEY (order_id) REFERENCES payment_order_parent (id) ON DELETE CASCADE,
			CONSTRAINT payment_attempt_provider_fk FOREIGN KEY (provider_code) REFERENCES payment_provider_parent (code),
			CONSTRAINT payment_attempt_asset_fk FOREIGN KEY (asset_code) REFERENCES payment_asset_parent (code)
		)`,
		`CREATE INDEX payment_attempt_full_order_idx ON payment_attempt_full (order_id)`,
		`CREATE INDEX payment_attempt_full_provider_status_idx ON payment_attempt_full (provider_code, status, created_at)`,
		`CREATE TABLE payment_attempt_no_fk (
			id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
			order_id BIGINT NOT NULL,
			provider_code VARCHAR(32) NOT NULL,
			asset_code VARCHAR(32) NOT NULL,
			amount_minor BIGINT NOT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'created',
			provider_payment_id VARCHAR(128) NULL,
			provider_invoice_id VARCHAR(128) NULL,
			provider_charge_id VARCHAR(128) NULL,
			provider_subscription_id VARCHAR(128) NULL,
			idempotency_key VARCHAR(128) NULL,
			confirmation_url TEXT NULL,
			return_url TEXT NULL,
			expires_at TIMESTAMPTZ NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT payment_attempt_no_fk_idempotency_uq UNIQUE (idempotency_key),
			CONSTRAINT payment_attempt_no_fk_provider_payment_uq UNIQUE (provider_code, provider_payment_id),
			CONSTRAINT payment_attempt_no_fk_provider_charge_uq UNIQUE (provider_code, provider_charge_id)
		)`,
		`CREATE INDEX payment_attempt_no_fk_order_idx ON payment_attempt_no_fk (order_id)`,
		`CREATE INDEX payment_attempt_no_fk_provider_status_idx ON payment_attempt_no_fk (provider_code, status, created_at)`,
		`CREATE TABLE payment_attempt_heap (
			id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
			order_id BIGINT NOT NULL,
			provider_code VARCHAR(32) NOT NULL,
			asset_code VARCHAR(32) NOT NULL,
			amount_minor BIGINT NOT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'created',
			provider_payment_id VARCHAR(128) NULL,
			provider_invoice_id VARCHAR(128) NULL,
			provider_charge_id VARCHAR(128) NULL,
			provider_subscription_id VARCHAR(128) NULL,
			idempotency_key VARCHAR(128) NULL,
			confirmation_url TEXT NULL,
			return_url TEXT NULL,
			expires_at TIMESTAMPTZ NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE payment_fulfillment_full (
			id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
			order_id BIGINT NOT NULL,
			attempt_id BIGINT NOT NULL,
			internal_user_id BIGINT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'pending',
			error TEXT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			fulfilled_at TIMESTAMPTZ NULL,
			revoked_at TIMESTAMPTZ NULL,
			CONSTRAINT payment_fulfillment_order_uq UNIQUE (order_id),
			CONSTRAINT payment_fulfillment_order_fk FOREIGN KEY (order_id) REFERENCES payment_order_parent (id),
			CONSTRAINT payment_fulfillment_attempt_fk FOREIGN KEY (attempt_id) REFERENCES payment_attempt_parent (id)
		)`,
		`CREATE INDEX payment_fulfillment_full_user_status_idx ON payment_fulfillment_full (internal_user_id, status)`,
		`CREATE TABLE payment_fulfillment_no_fk (
			id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
			order_id BIGINT NOT NULL,
			attempt_id BIGINT NOT NULL,
			internal_user_id BIGINT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'pending',
			error TEXT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			fulfilled_at TIMESTAMPTZ NULL,
			revoked_at TIMESTAMPTZ NULL,
			CONSTRAINT payment_fulfillment_no_fk_order_uq UNIQUE (order_id)
		)`,
		`CREATE INDEX payment_fulfillment_no_fk_user_status_idx ON payment_fulfillment_no_fk (internal_user_id, status)`,
		`CREATE TABLE payment_fulfillment_heap (
			id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
			order_id BIGINT NOT NULL,
			attempt_id BIGINT NOT NULL,
			internal_user_id BIGINT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'pending',
			error TEXT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			fulfilled_at TIMESTAMPTZ NULL,
			revoked_at TIMESTAMPTZ NULL
		)`,
		`CREATE TABLE payment_order_full (
			id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
			public_id CHAR(36) NOT NULL,
			workspace_id CHAR(36) NOT NULL,
			app_id BIGINT NOT NULL,
			platform_id BIGINT NOT NULL,
			platform_user_id VARCHAR(128) NOT NULL,
			internal_user_id BIGINT NULL,
			payer_platform_id BIGINT NULL,
			payer_platform_user_id VARCHAR(128) NULL,
			payer_internal_user_id BIGINT NULL,
			purchase_key_id BIGINT NULL,
			product_id VARCHAR(64) NOT NULL,
			price_id BIGINT NOT NULL,
			asset_code VARCHAR(32) NOT NULL,
			locale VARCHAR(16) NOT NULL DEFAULT 'ru',
			list_amount_minor BIGINT NOT NULL,
			discount_amount_minor BIGINT NOT NULL DEFAULT 0,
			payable_amount_minor BIGINT NOT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'draft',
			reserved_until TIMESTAMPTZ NULL,
			paid_at TIMESTAMPTZ NULL,
			fulfilled_at TIMESTAMPTZ NULL,
			canceled_at TIMESTAMPTZ NULL,
			expires_at TIMESTAMPTZ NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT payment_order_full_public_id_uq UNIQUE (public_id)
		)`,
		`CREATE INDEX payment_order_full_user_product_status_idx ON payment_order_full (workspace_id, platform_id, platform_user_id, product_id, status)`,
		`CREATE INDEX payment_order_full_payer_idx ON payment_order_full (app_id, payer_platform_id, payer_platform_user_id)`,
		`CREATE INDEX payment_order_full_purchase_key_idx ON payment_order_full (purchase_key_id)`,
		`CREATE INDEX payment_order_full_status_created_idx ON payment_order_full (status, created_at)`,
		`CREATE TABLE payment_order_heap (
			id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
			public_id CHAR(36) NOT NULL,
			workspace_id CHAR(36) NOT NULL,
			app_id BIGINT NOT NULL,
			platform_id BIGINT NOT NULL,
			platform_user_id VARCHAR(128) NOT NULL,
			internal_user_id BIGINT NULL,
			payer_platform_id BIGINT NULL,
			payer_platform_user_id VARCHAR(128) NULL,
			payer_internal_user_id BIGINT NULL,
			purchase_key_id BIGINT NULL,
			product_id VARCHAR(64) NOT NULL,
			price_id BIGINT NOT NULL,
			asset_code VARCHAR(32) NOT NULL,
			locale VARCHAR(16) NOT NULL DEFAULT 'ru',
			list_amount_minor BIGINT NOT NULL,
			discount_amount_minor BIGINT NOT NULL DEFAULT 0,
			payable_amount_minor BIGINT NOT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'draft',
			reserved_until TIMESTAMPTZ NULL,
			paid_at TIMESTAMPTZ NULL,
			fulfilled_at TIMESTAMPTZ NULL,
			canceled_at TIMESTAMPTZ NULL,
			expires_at TIMESTAMPTZ NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
	}

	for _, stmt := range statements {
		if _, err := env.db.ExecContext(env.ctx, stmt); err != nil {
			b.Fatalf("prepare constraint benchmark schema: %v", err)
		}
	}

	if _, err := env.db.ExecContext(env.ctx, `INSERT INTO payment_provider_parent (code) VALUES ('yookassa')`); err != nil {
		b.Fatalf("seed provider parent: %v", err)
	}
	if _, err := env.db.ExecContext(env.ctx, `INSERT INTO payment_asset_parent (code) VALUES ('RUB')`); err != nil {
		b.Fatalf("seed asset parent: %v", err)
	}
}

func benchmarkPaymentAttemptInsertVariant(b *testing.B, env paymentConstraintBenchEnv, table string) {
	b.Helper()
	idBase := int64(benchNextSeq()) * 1_000_000

	insertOrder, err := env.db.PrepareContext(env.ctx, `INSERT INTO payment_order_parent (id) VALUES ($1)`)
	if err != nil {
		b.Fatalf("prepare order parent insert: %v", err)
	}
	defer insertOrder.Close()

	insertAttempt, err := env.db.PrepareContext(env.ctx, fmt.Sprintf(
		`INSERT INTO %s (
			order_id, provider_code, asset_code, amount_minor, status,
			provider_payment_id, provider_invoice_id, provider_charge_id,
			provider_subscription_id, idempotency_key, confirmation_url, return_url, expires_at
		) VALUES ($1, 'yookassa', 'RUB', 1000, 'pending', $2, NULL, NULL, NULL, $3, NULL, NULL, NULL)`,
		table,
	))
	if err != nil {
		b.Fatalf("prepare attempt insert: %v", err)
	}
	defer insertAttempt.Close()

	orderIDs := make([]uint64, b.N)
	paymentIDs := make([]string, b.N)
	idempotencyKeys := make([]string, b.N)
	b.StopTimer()
	for i := 0; i < b.N; i++ {
		orderID := uint64(idBase + int64(i) + 1)
		orderIDs[i] = orderID
		paymentIDs[i] = fmt.Sprintf("%s-pay-%d", table, idBase+int64(i)+1)
		idempotencyKeys[i] = fmt.Sprintf("%s-idem-%d", table, idBase+int64(i)+1)
		if _, err := insertOrder.ExecContext(env.ctx, orderID); err != nil {
			b.Fatalf("insert order parent: %v", err)
		}
	}
	b.StartTimer()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := insertAttempt.ExecContext(env.ctx, orderIDs[i], paymentIDs[i], idempotencyKeys[i]); err != nil {
			b.Fatalf("insert payment attempt: %v", err)
		}
	}
}

func benchmarkFulfillmentInsertVariant(b *testing.B, env paymentConstraintBenchEnv, table string) {
	b.Helper()
	idBase := int64(benchNextSeq()) * 1_000_000

	parentInsertOrder, err := env.db.PrepareContext(env.ctx, `INSERT INTO payment_order_parent (id) VALUES ($1)`)
	if err != nil {
		b.Fatalf("prepare order parent insert: %v", err)
	}
	defer parentInsertOrder.Close()

	parentInsertAttempt, err := env.db.PrepareContext(env.ctx, `INSERT INTO payment_attempt_parent (id, order_id) VALUES ($1, $2)`)
	if err != nil {
		b.Fatalf("prepare attempt parent insert: %v", err)
	}
	defer parentInsertAttempt.Close()

	insertFulfillment, err := env.db.PrepareContext(env.ctx, fmt.Sprintf(
		`INSERT INTO %s (order_id, attempt_id, internal_user_id, status) VALUES ($1, $2, $3, 'succeeded')`,
		table,
	))
	if err != nil {
		b.Fatalf("prepare fulfillment insert: %v", err)
	}
	defer insertFulfillment.Close()

	orderIDs := make([]uint64, b.N)
	attemptIDs := make([]uint64, b.N)
	b.StopTimer()
	for i := 0; i < b.N; i++ {
		orderID := uint64(idBase + int64(i) + 1)
		attemptID := uint64(idBase + int64(i) + 1)
		orderIDs[i] = orderID
		attemptIDs[i] = attemptID
		if _, err := parentInsertOrder.ExecContext(env.ctx, orderID); err != nil {
			b.Fatalf("insert order parent: %v", err)
		}
		if _, err := parentInsertAttempt.ExecContext(env.ctx, attemptID, orderID); err != nil {
			b.Fatalf("insert attempt parent: %v", err)
		}
	}
	b.StartTimer()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := insertFulfillment.ExecContext(env.ctx, orderIDs[i], attemptIDs[i], orderIDs[i]); err != nil {
			b.Fatalf("insert fulfillment: %v", err)
		}
	}
}

func benchmarkMarkOrderFulfilledVariant(b *testing.B, env paymentConstraintBenchEnv, table string) {
	b.Helper()
	idBase := int64(benchNextSeq()) * 1_000_000

	insertOrder, err := env.db.PrepareContext(env.ctx, fmt.Sprintf(
		`INSERT INTO %s (
			public_id, workspace_id, app_id, platform_id, platform_user_id, product_id, price_id, asset_code, locale,
			list_amount_minor, discount_amount_minor, payable_amount_minor, status, paid_at
		) VALUES ($1, 'bench-workspace', 1, 1, 'user', 'product', 1, 'RUB', 'ru', 1000, 0, 1000, 'paid', now())
		RETURNING id`,
		table,
	))
	if err != nil {
		b.Fatalf("prepare order insert: %v", err)
	}
	defer insertOrder.Close()

	updateOrder, err := env.db.PrepareContext(env.ctx, fmt.Sprintf(
		`UPDATE %s
		SET status = 'fulfilled',
		    fulfilled_at = COALESCE(fulfilled_at, now()),
		    updated_at = now()
		WHERE id = $1
		  AND status IN ('paid', 'fulfilled')`,
		table,
	))
	if err != nil {
		b.Fatalf("prepare order update: %v", err)
	}
	defer updateOrder.Close()

	orderIDs := make([]int64, b.N)
	b.StopTimer()
	for i := 0; i < b.N; i++ {
		publicID := fmt.Sprintf("%s-%d", table, idBase+int64(i)+1)
		if err := insertOrder.QueryRowContext(env.ctx, publicID).Scan(&orderIDs[i]); err != nil {
			b.Fatalf("read seeded order id: %v", err)
		}
	}
	b.StartTimer()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := updateOrder.ExecContext(env.ctx, orderIDs[i]); err != nil {
			b.Fatalf("mark order fulfilled: %v", err)
		}
	}
}

const paymentLatencyWarmup = 5

func BenchmarkPaymentLatencyPercentiles(b *testing.B) {
	env := setupPaymentBenchmark(b)
	q := env.q
	productA := env.products[0]
	productB := env.products[1]
	priceID := productA.priceIDs["RUB"]

	b.Run("service/Product.CreateKey", func(b *testing.B) {
		measurePaymentLatency(b, func(i int) error {
			_, err := env.api.Admin.CreateProductKey(env.ctx, product.CreateKeyParams{
				WorkspaceID:    benchWorkspaceID,
				AppID:          7001,
				PlatformID:     1,
				PlatformUserID: "latency_key_target_" + strconv.Itoa(i),
				ProductID:      env.products[i%len(env.products)].id,
				MaxUses:        1_000_000,
			})
			return err
		})
	})

	b.Run("service/Checkout.CreateOrder", func(b *testing.B) {
		measurePaymentLatency(b, func(i int) error {
			_, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
				Identity:  paymentTestIdentity(benchWorkspaceID, 7001, 1, "latency_checkout_"+strconv.Itoa(i)),
				ProductID: env.products[i%len(env.products)].id,
				AssetCode: "RUB",
				Locale:    "ru",
			})
			return err
		})
	})

	b.Run("service/VKMA.ChargeableForWorkspace.idempotent", func(b *testing.B) {
		measurePaymentLatency(b, func(i int) error {
			_, err := env.api.Adapters.VKMA.ChargeableForWorkspace(env.ctx, benchWorkspaceID, vkmashop.Params{
				NotificationType: vkmashop.OrderStatusChange,
				Status:           vkmashop.Chargeable,
				AppID:            7001,
				UserID:           9000,
				Item:             productB.id,
				OrderID:          1,
				Lang:             "ru",
			})
			return err
		})
	})

	b.Run("service/VKMA.SubscriptionStatus", func(b *testing.B) {
		subscriptionIDs := make([]int, b.N+paymentLatencyWarmup)
		b.StopTimer()
		for i := range subscriptionIDs {
			subscriptionIDs[i] = int((paymentBenchRunNumber % 1_000_000_000) + benchNextSeq())
			attempt, err := q.AdminGetPaymentAttempt(
				env.ctx,
				int64(env.attempts[i%len(env.attempts)]),
			)
			benchNoError(b, err)
			order, err := q.GetPaymentOrder(env.ctx, attempt.OrderID)
			benchNoError(b, err)
			_, err = q.UpsertPaymentSubscription(env.ctx, benchmarkUpsertSubscriptionParams(
				order.ProductID,
				uint64(order.ID),
				uint64(attempt.ID),
				strconv.Itoa(subscriptionIDs[i]),
			))
			benchNoError(b, err)
		}
		b.StartTimer()

		measurePaymentLatency(b, func(i int) error {
			_, err := env.api.Adapters.VKMA.Canceled(env.ctx, testWorkspaceID, vkmashop.Params{
				NotificationType: vkmashop.SubscriptionStatusChange,
				Status:           vkmashop.Canceled,
				AppID:            7001,
				UserID:           9000,
				SubscriptionID:   subscriptionIDs[i],
			})
			return err
		})
	})

	b.Run("sqlc/UpsertProductGroup", func(b *testing.B) {
		measurePaymentLatency(b, func(i int) error {
			return q.UpsertProductGroup(env.ctx, paymentsqlc.UpsertProductGroupParams{
				WorkspaceID: benchWorkspaceID,
				Code:        "latency_group_write_" + strconv.Itoa(i),
				TitleKey:    sql.NullString{String: "bench.group.title", Valid: true},
				IsActive:    true,
			})
		})
	})

	b.Run("sqlc/DeleteProductGroup", func(b *testing.B) {
		codes := make([]string, b.N+paymentLatencyWarmup)
		b.StopTimer()
		for i := range codes {
			codes[i] = "latency_group_delete_" + strconv.Itoa(i)
			benchNoError(b, q.UpsertProductGroup(env.ctx, paymentsqlc.UpsertProductGroupParams{
				WorkspaceID: benchWorkspaceID,
				Code:        codes[i],
				IsActive:    true,
			}))
		}
		b.StartTimer()

		measurePaymentLatency(b, func(i int) error {
			_, err := q.DeleteProductGroup(env.ctx, paymentsqlc.DeleteProductGroupParams{
				WorkspaceID: benchWorkspaceID,
				Code:        codes[i],
			})
			return err
		})
	})

	b.Run("sqlc/UpsertProduct", func(b *testing.B) {
		measurePaymentLatency(b, func(i int) error {
			return q.UpsertProduct(env.ctx, benchmarkUpsertProductParams("latency_product_write_"+strconv.Itoa(i), "bench_group_0"))
		})
	})

	b.Run("sqlc/DeleteProduct", func(b *testing.B) {
		ids := make([]string, b.N+paymentLatencyWarmup)
		b.StopTimer()
		for i := range ids {
			ids[i] = "latency_product_delete_" + strconv.Itoa(i)
			benchNoError(b, q.UpsertProduct(env.ctx, benchmarkUpsertProductParams(ids[i], "bench_group_0")))
		}
		b.StartTimer()

		measurePaymentLatency(b, func(i int) error {
			_, err := q.DeleteProduct(env.ctx, paymentsqlc.DeleteProductParams{
				WorkspaceID: benchWorkspaceID,
				ID:          ids[i],
			})
			return err
		})
	})

	b.Run("sqlc/CreateProductPrice", func(b *testing.B) {
		measurePaymentLatency(b, func(i int) error {
			seq := benchNextSeq()
			_, err := q.CreateProductPrice(env.ctx, benchmarkCreatePriceParams(productA.id, benchPriceStart(seq)))
			return err
		})
	})

	b.Run("sqlc/UpdateProductPrice", func(b *testing.B) {
		measurePaymentLatency(b, func(i int) error {
			_, err := q.UpdateProductPrice(env.ctx, paymentsqlc.UpdateProductPriceParams{
				ID:                  int64(priceID),
				WorkspaceID:         benchWorkspaceID,
				AssetCode:           "RUB",
				ListAmountMinor:     int64(1000 + i%100),
				DiscountAmountMinor: 0,
				StartsAt:            time.Now().Add(-24 * time.Hour),
				EndsAt:              time.Now().Add(24 * time.Hour),
			})
			return err
		})
	})

	b.Run("sqlc/DeleteProductPrice", func(b *testing.B) {
		priceIDs := make([]uint64, b.N+paymentLatencyWarmup)
		b.StopTimer()
		for i := range priceIDs {
			seq := benchNextSeq()
			id, err := q.CreateProductPrice(env.ctx, benchmarkCreatePriceParams(productA.id, benchPriceStart(seq)))
			benchNoError(b, err)
			priceIDs[i] = uint64(id)
		}
		b.StartTimer()

		measurePaymentLatency(b, func(i int) error {
			_, err := q.DeleteProductPrice(env.ctx, paymentsqlc.DeleteProductPriceParams{
				WorkspaceID: benchWorkspaceID,
				ID:          int64(priceIDs[i]),
			})
			return err
		})
	})

	b.Run("sqlc/GetProductLimitCounterCount", func(b *testing.B) {
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		end := start.Add(24 * time.Hour)
		_, err := q.EnsureProductLimitCounter(env.ctx, paymentsqlc.EnsureProductLimitCounterParams{
			WorkspaceID:    benchWorkspaceID,
			PlatformID:     1,
			ProductID:      productA.id,
			CounterScope:   paymentsqlc.PaymentProductLimitCounterCounterScopeGlobal,
			PlatformUserID: "",
			WindowStart:    start,
			WindowEnd:      end,
		})
		benchNoError(b, err)
		measurePaymentLatency(b, func(i int) error {
			_, err := q.GetProductLimitCounterCount(env.ctx, paymentsqlc.GetProductLimitCounterCountParams{
				WorkspaceID:    benchWorkspaceID,
				PlatformID:     1,
				ProductID:      productA.id,
				CounterScope:   paymentsqlc.PaymentProductLimitCounterCounterScopeGlobal,
				PlatformUserID: "",
				WindowStart:    start,
				WindowEnd:      end,
			})
			return err
		})
	})

	b.Run("sqlc/CreatePurchaseKey", func(b *testing.B) {
		measurePaymentLatency(b, func(i int) error {
			seq := benchNextSeq()
			_, err := q.CreatePurchaseKey(env.ctx, paymentsqlc.CreatePurchaseKeyParams{
				WorkspaceID:    benchWorkspaceID,
				KeyHash:        benchHash(benchRunValue("latency_create_key", seq)),
				AppID:          7001,
				PlatformID:     1,
				PlatformUserID: benchRunValue("latency_key_user", seq),
				ProductID:      productA.id,
				MaxUses:        1_000_000,
			})
			return err
		})
	})

	b.Run("sqlc/ReservePurchaseKeyUsage", func(b *testing.B) {
		measurePaymentLatency(b, func(i int) error {
			_, err := q.ReservePurchaseKeyUsage(env.ctx, int64(1))
			return err
		})
	})

	b.Run("sqlc/CreatePaymentOrder", func(b *testing.B) {
		measurePaymentLatency(b, func(i int) error {
			seq := benchNextSeq()
			_, err := q.CreatePaymentOrder(env.ctx, benchmarkCreateOrderParams(productA, "RUB", priceID, benchRunValue("latency_order", seq)))
			return err
		})
	})

	b.Run("sqlc/CreatePaymentAttempt", func(b *testing.B) {
		orderIDs := make([]uint64, b.N+paymentLatencyWarmup)
		b.StopTimer()
		for i := range orderIDs {
			seq := benchNextSeq()
			order := createBenchmarkOrder(b, env, productA.id, "RUB", benchRunValue("latency_attempt_order", seq))
			orderIDs[i] = order.ID
		}
		b.StartTimer()

		measurePaymentLatency(b, func(i int) error {
			seq := benchNextSeq()
			_, err := q.CreatePaymentAttempt(env.ctx, benchmarkCreateAttemptParams(orderIDs[i], benchRunValue("latency_attempt", seq)))
			return err
		})
	})

	b.Run("sqlc/CreateFulfillment", func(b *testing.B) {
		type fulfillmentInput struct {
			orderID   uint64
			attemptID uint64
		}
		inputs := make([]fulfillmentInput, b.N+paymentLatencyWarmup)
		b.StopTimer()
		for i := range inputs {
			seq := benchNextSeq()
			order := createBenchmarkOrder(b, env, productA.id, "RUB", benchRunValue("latency_fulfillment_order", seq))
			attemptID, err := q.CreatePaymentAttempt(env.ctx, benchmarkCreateAttemptParams(order.ID, benchRunValue("latency_fulfillment_attempt", seq)))
			benchNoError(b, err)
			inputs[i] = fulfillmentInput{orderID: order.ID, attemptID: uint64(attemptID)}
		}
		b.StartTimer()

		measurePaymentLatency(b, func(i int) error {
			_, err := q.CreateFulfillment(env.ctx, paymentsqlc.CreateFulfillmentParams{
				OrderID:   int64(inputs[i].orderID),
				AttemptID: int64(inputs[i].attemptID),
				Status:    paymentsqlc.PaymentFulfillmentStatusSucceeded,
			})
			return err
		})
	})
}

func BenchmarkPaymentParallelLatencyPercentiles(b *testing.B) {
	env := setupPaymentBenchmark(b)
	q := env.q
	productA := env.products[0]
	productB := env.products[1]
	priceID := productA.priceIDs["RUB"]

	b.Run("service/Product.Get", func(b *testing.B) {
		measurePaymentParallelLatency(b, func(i int) error {
			_, err := env.api.User.GetProduct(env.ctx, product.GetParams{
				Identity:  paymentTestIdentity(benchWorkspaceID, 7001, 1, "parallel_get_user_"+strconv.Itoa(i%100_000)),
				ProductID: env.products[i%len(env.products)].id,
				AssetCode: "RUB",
				Locale:    "ru",
			})
			return err
		})
	})

	b.Run("service/Product.CreateKey", func(b *testing.B) {
		measurePaymentParallelLatency(b, func(i int) error {
			seq := benchNextSeq()
			_, err := env.api.Admin.CreateProductKey(env.ctx, product.CreateKeyParams{
				WorkspaceID:    benchWorkspaceID,
				AppID:          7001,
				PlatformID:     1,
				PlatformUserID: benchRunValue("parallel_key_target", seq),
				ProductID:      env.products[i%len(env.products)].id,
				MaxUses:        1_000_000,
			})
			return err
		})
	})

	b.Run("service/Checkout.CreateOrder", func(b *testing.B) {
		measurePaymentParallelLatency(b, func(i int) error {
			seq := benchNextSeq()
			_, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
				Identity:  paymentTestIdentity(benchWorkspaceID, 7001, 1, benchRunValue("parallel_checkout", seq)),
				ProductID: env.products[i%len(env.products)].id,
				AssetCode: "RUB",
				Locale:    "ru",
			})
			return err
		})
	})

	b.Run("service/VKMA.ChargeableForWorkspace.idempotent", func(b *testing.B) {
		measurePaymentParallelLatency(b, func(i int) error {
			_, err := env.api.Adapters.VKMA.ChargeableForWorkspace(env.ctx, benchWorkspaceID, vkmashop.Params{
				NotificationType: vkmashop.OrderStatusChange,
				Status:           vkmashop.Chargeable,
				AppID:            7001,
				UserID:           9000,
				Item:             productB.id,
				OrderID:          1,
				Lang:             "ru",
			})
			return err
		})
	})

	b.Run("service/Subscription.IsActive", func(b *testing.B) {
		measurePaymentParallelLatency(b, func(i int) error {
			_, err := env.api.User.IsSubscriptionActive(env.ctx, subscription.IsActiveParams{
				Identity:     paymentTestIdentity(benchWorkspaceID, 7001, 1, "bench_user_"+strconv.Itoa(i%100_000)),
				ProductID:    env.products[i%len(env.products)].id,
				ProviderCode: "vkma",
			})
			return err
		})
	})

	b.Run("sqlc/GetProductRows", func(b *testing.B) {
		measurePaymentParallelLatency(b, func(i int) error {
			_, err := q.GetProductRows(env.ctx, paymentsqlc.GetProductRowsParams{
				ProductID:   env.products[i%len(env.products)].id,
				WorkspaceID: benchWorkspaceID,
				AssetCode:   "RUB",
				Locale:      "ru",
			})
			return err
		})
	})

	b.Run("sqlc/GetProductLimitCounterCount", func(b *testing.B) {
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		end := start.Add(24 * time.Hour)
		_, err := q.EnsureProductLimitCounter(env.ctx, paymentsqlc.EnsureProductLimitCounterParams{
			WorkspaceID:    benchWorkspaceID,
			PlatformID:     1,
			ProductID:      productA.id,
			CounterScope:   paymentsqlc.PaymentProductLimitCounterCounterScopeGlobal,
			PlatformUserID: "",
			WindowStart:    start,
			WindowEnd:      end,
		})
		benchNoError(b, err)
		measurePaymentParallelLatency(b, func(i int) error {
			_, err := q.GetProductLimitCounterCount(env.ctx, paymentsqlc.GetProductLimitCounterCountParams{
				WorkspaceID:    benchWorkspaceID,
				PlatformID:     1,
				ProductID:      productA.id,
				CounterScope:   paymentsqlc.PaymentProductLimitCounterCounterScopeGlobal,
				PlatformUserID: "",
				WindowStart:    start,
				WindowEnd:      end,
			})
			return err
		})
	})

	b.Run("sqlc/CreatePurchaseKey", func(b *testing.B) {
		measurePaymentParallelLatency(b, func(i int) error {
			seq := benchNextSeq()
			_, err := q.CreatePurchaseKey(env.ctx, paymentsqlc.CreatePurchaseKeyParams{
				WorkspaceID:    benchWorkspaceID,
				KeyHash:        benchHash(benchRunValue("parallel_create_key", seq)),
				AppID:          7001,
				PlatformID:     1,
				PlatformUserID: benchRunValue("parallel_key_user", seq),
				ProductID:      productA.id,
				MaxUses:        1_000_000,
			})
			return err
		})
	})

	b.Run("sqlc/CreatePaymentOrder", func(b *testing.B) {
		measurePaymentParallelLatency(b, func(i int) error {
			seq := benchNextSeq()
			_, err := q.CreatePaymentOrder(env.ctx, benchmarkCreateOrderParams(productA, "RUB", priceID, benchRunValue("parallel_order", seq)))
			return err
		})
	})

	b.Run("sqlc/CreatePaymentAttempt", func(b *testing.B) {
		orderIDs := make([]uint64, b.N+paymentLatencyWarmup)
		b.StopTimer()
		for i := range orderIDs {
			seq := benchNextSeq()
			order := createBenchmarkOrder(b, env, productA.id, "RUB", benchRunValue("parallel_attempt_order", seq))
			orderIDs[i] = order.ID
		}
		b.StartTimer()

		measurePaymentParallelLatency(b, func(i int) error {
			seq := benchNextSeq()
			_, err := q.CreatePaymentAttempt(env.ctx, benchmarkCreateAttemptParams(orderIDs[i], benchRunValue("parallel_attempt", seq)))
			return err
		})
	})

	b.Run("sqlc/CreateFulfillment", func(b *testing.B) {
		type fulfillmentInput struct {
			orderID   uint64
			attemptID uint64
		}
		inputs := make([]fulfillmentInput, b.N+paymentLatencyWarmup)
		b.StopTimer()
		for i := range inputs {
			seq := benchNextSeq()
			order := createBenchmarkOrder(b, env, productA.id, "RUB", benchRunValue("parallel_fulfillment_order", seq))
			attemptID, err := q.CreatePaymentAttempt(env.ctx, benchmarkCreateAttemptParams(order.ID, benchRunValue("parallel_fulfillment_attempt", seq)))
			benchNoError(b, err)
			inputs[i] = fulfillmentInput{orderID: order.ID, attemptID: uint64(attemptID)}
		}
		b.StartTimer()

		measurePaymentParallelLatency(b, func(i int) error {
			_, err := q.CreateFulfillment(env.ctx, paymentsqlc.CreateFulfillmentParams{
				OrderID:   int64(inputs[i].orderID),
				AttemptID: int64(inputs[i].attemptID),
				Status:    paymentsqlc.PaymentFulfillmentStatusSucceeded,
			})
			return err
		})
	})
}

func measurePaymentLatency(b *testing.B, run func(int) error) {
	b.Helper()
	b.ReportAllocs()

	for i := 0; i < paymentLatencyWarmup; i++ {
		benchNoError(b, run(i))
	}

	samples := make([]time.Duration, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		err := run(i + paymentLatencyWarmup)
		samples[i] = time.Since(start)
		benchNoError(b, err)
	}
	b.StopTimer()

	reportPaymentLatency(b, samples)
}

func measurePaymentParallelLatency(b *testing.B, run func(int) error) {
	b.Helper()
	b.ReportAllocs()

	for i := 0; i < paymentLatencyWarmup; i++ {
		benchNoError(b, run(i))
	}

	samples := make([]time.Duration, b.N)
	var (
		sampleIndex uint64
		firstErr    error
		errOnce     sync.Once
	)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			index := int(atomic.AddUint64(&sampleIndex, 1) - 1)
			if index >= len(samples) {
				continue
			}
			start := time.Now()
			err := run(index + paymentLatencyWarmup)
			samples[index] = time.Since(start)
			if err != nil {
				errOnce.Do(func() {
					firstErr = err
				})
			}
		}
	})
	b.StopTimer()

	if firstErr != nil {
		b.Fatal(firstErr)
	}
	reportPaymentLatency(b, samples)
}

func reportPaymentLatency(b *testing.B, samples []time.Duration) {
	b.Helper()
	if len(samples) == 0 {
		return
	}

	sort.Slice(samples, func(i int, j int) bool {
		return samples[i] < samples[j]
	})

	b.ReportMetric(float64(latencyPercentile(samples, 0.50).Nanoseconds()), "p50-ns/op")
	b.ReportMetric(float64(latencyPercentile(samples, 0.95).Nanoseconds()), "p95-ns/op")
	b.ReportMetric(float64(latencyPercentile(samples, 0.99).Nanoseconds()), "p99-ns/op")
	b.ReportMetric(float64(samples[len(samples)-1].Nanoseconds()), "max-ns/op")
}

func latencyPercentile(samples []time.Duration, percentile float64) time.Duration {
	if len(samples) == 1 {
		return samples[0]
	}
	index := int(float64(len(samples)-1) * percentile)
	return samples[index]
}

const paymentPurchaseStatsTrigger = `
CREATE OR REPLACE FUNCTION payment_order_create_purchase_stats_fn()
RETURNS trigger AS $$
BEGIN
    IF NEW.status = 'fulfilled' AND OLD.status <> 'fulfilled' THEN
        INSERT INTO payment_stats_event (
            event_type, source_id, workspace_id, product_id,
            app_id, platform_id, platform_user_id, quantity,
            asset_code, amount_minor, occurred_at
        )
        VALUES (
            'purchase', NEW.id, NEW.workspace_id, NEW.product_id,
            NEW.app_id,
            COALESCE(NEW.payer_platform_id, NEW.platform_id),
            COALESCE(NEW.payer_platform_user_id, NEW.platform_user_id),
            NEW.quantity,
            NEW.asset_code, NEW.payable_amount_minor, COALESCE(NEW.fulfilled_at, now())
        )
        ON CONFLICT (event_type, source_id) DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER payment_order_create_purchase_stats
AFTER UPDATE ON payment_order
FOR EACH ROW EXECUTE FUNCTION payment_order_create_purchase_stats_fn();`

func BenchmarkPaymentAdminStats(b *testing.B) {
	env := setupPaymentBenchmark(b)
	seedPaymentBenchmarkStats(b, env)
	productID := env.products[0].id
	from := time.Now().Add(-365 * 24 * time.Hour)
	until := time.Now().Add(24 * time.Hour)

	b.Run("GetStats", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := env.api.Admin.GetStats(env.ctx, benchWorkspaceID)
			benchNoError(b, err)
		}
	})
	b.Run("GetProductStats", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := env.api.Admin.GetProductStats(env.ctx, benchWorkspaceID, productID)
			benchNoError(b, err)
		}
	})
	b.Run("ListDailyStats", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := env.api.Admin.ListDailyStats(env.ctx, benchWorkspaceID, productID, from, until)
			benchNoError(b, err)
		}
	})
	b.Run("ListDailyOverview", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := env.api.Admin.ListDailyOverview(env.ctx, benchWorkspaceID, from, until)
			benchNoError(b, err)
		}
	})
	b.Run("RefreshDailyStats", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			benchNoError(b, env.api.Admin.RefreshDailyStats(env.ctx, benchWorkspaceID, from, until))
		}
	})
}

func BenchmarkPaymentPurchaseStatsTrigger(b *testing.B) {
	env := setupPaymentBenchmark(b)

	if _, err := env.db.ExecContext(env.ctx, "DROP TRIGGER IF EXISTS payment_order_create_purchase_stats ON payment_order"); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		_, _ = env.db.ExecContext(context.Background(), "DROP TRIGGER IF EXISTS payment_order_create_purchase_stats ON payment_order")
		_, _ = env.db.ExecContext(context.Background(), paymentPurchaseStatsTrigger)
	})
	b.Run("without_trigger", func(b *testing.B) {
		benchmarkFulfillmentUpdates(b, env)
	})

	if _, err := env.db.ExecContext(env.ctx, paymentPurchaseStatsTrigger); err != nil {
		b.Fatal(err)
	}
	b.Run("with_trigger", func(b *testing.B) {
		benchmarkFulfillmentUpdates(b, env)
	})
}

func seedPaymentBenchmarkStats(b *testing.B, env paymentBenchmarkEnv) {
	b.Helper()
	_, err := env.db.ExecContext(env.ctx, `
INSERT INTO payment_stats_event (
    event_type, source_id, workspace_id, product_id,
    app_id, platform_id, platform_user_id, quantity,
    asset_code, amount_minor, occurred_at
)
SELECT
    'purchase', o.id, o.workspace_id, o.product_id,
    o.app_id,
    COALESCE(o.payer_platform_id, o.platform_id),
    COALESCE(o.payer_platform_user_id, o.platform_user_id),
    o.quantity,
    o.asset_code, o.payable_amount_minor, COALESCE(o.fulfilled_at, o.updated_at)
FROM payment_order o
WHERE o.workspace_id = $1 AND o.status IN ('fulfilled', 'refunded')
ON CONFLICT (event_type, source_id) DO NOTHING`, benchWorkspaceID)
	if err != nil {
		b.Fatal(err)
	}
	if err := env.api.Admin.RefreshDailyStats(
		env.ctx, benchWorkspaceID, time.Now().Add(-365*24*time.Hour), time.Now().Add(24*time.Hour),
	); err != nil {
		b.Fatal(err)
	}
}

func benchmarkFulfillmentUpdates(b *testing.B, env paymentBenchmarkEnv) {
	b.Helper()
	tx, err := env.db.BeginTx(env.ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer tx.Rollback()

	product := env.products[0]
	priceID := product.priceIDs["RUB"]
	ids := make([]uint64, b.N)
	statement, err := tx.PrepareContext(env.ctx, `
INSERT INTO payment_order (
    public_id, workspace_id, app_id, platform_id, platform_user_id,
    product_id, quantity, price_id, asset_code, locale,
    list_amount_minor, discount_amount_minor, payable_amount_minor,
    status, paid_at
) VALUES ($1, $2, 1, 1, $3, $4, 1, $5, 'RUB', 'ru', 1000, 0, 1000, 'paid', now())
RETURNING id`)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		err := statement.QueryRowContext(
			env.ctx,
			fmt.Sprintf("s%035d", i+1),
			benchWorkspaceID,
			fmt.Sprintf("stats-user-%d", i),
			product.id,
			priceID,
		).Scan(&ids[i])
		if err != nil {
			statement.Close()
			b.Fatal(err)
		}
	}
	if err := statement.Close(); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for _, id := range ids {
		if _, err := tx.ExecContext(env.ctx, `
UPDATE payment_order
SET status = 'fulfilled', fulfilled_at = now(), updated_at = now()
WHERE id = $1 AND status = 'paid'`, id); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

func BenchmarkVKMAPaymentProcedure(b *testing.B) {
	env := setupVKMAProcedureBenchmark(b)
	env.db.SetMaxOpenConns(320)
	env.db.SetMaxIdleConns(320)

	productID := createBenchmarkVKMAProduct(b, env)

	b.Run("one_time", func(b *testing.B) {
		benchmarkVKMAPaymentProcedure(b, env, productID)
	})
}

func setupVKMAProcedureBenchmark(b *testing.B) paymentBenchmarkEnv {
	b.Helper()
	env := setupPaymentIntegrationTest(b)
	return paymentBenchmarkEnv{
		ctx: env.ctx,
		db:  env.db,
		api: env.api,
	}
}

type vkmaPaymentProcedureTotals struct {
	catalogGetProduct    time.Duration
	platformGetItem      time.Duration
	confirmCreateOrder   time.Duration
	confirmCreateAttempt time.Duration
	confirmCreateEvent   time.Duration
	confirmComplete      time.Duration
	confirmTotal         time.Duration
	procedureTotal       time.Duration
}

func benchmarkVKMAPaymentProcedure(b *testing.B, env paymentBenchmarkEnv, productID string) {
	b.Helper()

	var totals vkmaPaymentProcedureTotals
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		seq := benchNextSeq()
		userID := fmt.Sprintf("vkma_bench_user_%d", seq)
		orderID := 1_500_000_000 + int(seq%400_000_000)
		providerPaymentID := fmt.Sprintf("%d", orderID)
		providerEventID := benchRunValue("vkma_event", seq)
		eventType := string(vkmashop.OrderStatusChange)
		eventStatus := string(vkmashop.Chargeable)
		signatureValid := true

		procedureStart := time.Now()

		start := time.Now()
		_, err := env.api.User.GetProduct(env.ctx, product.GetParams{
			Identity:  paymentTestIdentity(benchWorkspaceID, 7001, paymentvkma.PlatformID, userID),
			ProductID: productID,
			AssetCode: paymentvkma.AssetCode,
			Locale:    "ru",
		})
		totals.catalogGetProduct += time.Since(start)
		benchNoError(b, err)

		start = time.Now()
		_, err = env.api.Adapters.VKMA.GetItemForWorkspace(env.ctx, benchWorkspaceID, vkmashop.Params{
			NotificationType: vkmashop.GetItem,
			AppID:            7001,
			UserID:           int(seq),
			Item:             productID,
			Lang:             "ru",
		})
		totals.platformGetItem += time.Since(start)
		benchNoError(b, err)

		confirmStart := time.Now()

		start = time.Now()
		order, err := env.api.User.CreateOrder(env.ctx, checkout.CreateOrderParams{
			Identity:  paymentTestIdentity(benchWorkspaceID, 7001, paymentvkma.PlatformID, userID),
			ProductID: productID,
			AssetCode: paymentvkma.AssetCode,
			Locale:    "ru",
		})
		totals.confirmCreateOrder += time.Since(start)
		benchNoError(b, err)

		idempotencyKey := fmt.Sprintf("%s:%s", paymentvkma.ProviderCode, providerPaymentID)

		start = time.Now()
		attempt, err := env.api.User.CreateAttempt(env.ctx, checkout.CreateAttemptParams{
			Identity:          paymentAttemptIdentity(order),
			OrderID:           order.ID,
			ProviderCode:      paymentvkma.ProviderCode,
			ProviderPaymentID: &providerPaymentID,
			IdempotencyKey:    &idempotencyKey,
		})
		totals.confirmCreateAttempt += time.Since(start)
		benchNoError(b, err)

		orderIDInt64 := int64(order.ID)
		attemptIDInt64 := int64(attempt.ID)
		payloadHash := benchmarkVKMAPayloadHash(providerEventID, providerPaymentID, productID, userID)

		start = time.Now()
		_, err = env.api.Operational.CreateEvent(env.ctx, checkout.CreateEventParams{
			ProviderCode:      paymentvkma.ProviderCode,
			AttemptID:         &attemptIDInt64,
			OrderID:           &orderIDInt64,
			ProviderEventID:   &providerEventID,
			ProviderPaymentID: &providerPaymentID,
			EventType:         eventType,
			EventStatus:       &eventStatus,
			PayloadHash:       payloadHash,
			SignatureValid:    &signatureValid,
		})
		totals.confirmCreateEvent += time.Since(start)
		benchNoError(b, err)

		start = time.Now()
		_, err = env.api.Operational.CompleteAttempt(env.ctx, checkout.CompleteAttemptParams{
			WorkspaceID:       testWorkspaceID,
			AttemptID:         attempt.ID,
			ProviderCode:      paymentvkma.ProviderCode,
			ProviderPaymentID: &providerPaymentID,
			AmountMinor:       attempt.AmountMinor,
			AssetCode:         paymentvkma.AssetCode,
		})
		totals.confirmComplete += time.Since(start)
		benchNoError(b, err)

		totals.confirmTotal += time.Since(confirmStart)
		totals.procedureTotal += time.Since(procedureStart)
	}

	reportVKMAProcedureMetric(b, "catalog-get-product", totals.catalogGetProduct, b.N)
	reportVKMAProcedureMetric(b, "platform-get-item", totals.platformGetItem, b.N)
	reportVKMAProcedureMetric(b, "confirm-create-order", totals.confirmCreateOrder, b.N)
	reportVKMAProcedureMetric(b, "confirm-create-attempt", totals.confirmCreateAttempt, b.N)
	reportVKMAProcedureMetric(b, "confirm-create-event", totals.confirmCreateEvent, b.N)
	reportVKMAProcedureMetric(b, "confirm-complete-attempt", totals.confirmComplete, b.N)
	reportVKMAProcedureMetric(b, "confirm-total", totals.confirmTotal, b.N)
	reportVKMAProcedureMetric(b, "procedure-total", totals.procedureTotal, b.N)
}

func benchmarkVKMAPayloadHash(providerEventID string, providerPaymentID string, productID string, userID string) string {
	sum := sha256.Sum256([]byte(providerEventID + "|" + providerPaymentID + "|" + productID + "|" + userID))
	return hex.EncodeToString(sum[:])
}

func reportVKMAProcedureMetric(b *testing.B, name string, total time.Duration, iterations int) {
	b.Helper()
	if total == 0 || iterations == 0 {
		return
	}
	b.ReportMetric(float64(total.Nanoseconds())/float64(iterations), name+"-ns/op")
}

func createBenchmarkVKMAProduct(b *testing.B, env paymentBenchmarkEnv) string {
	b.Helper()

	productID := "bench_vkma_product_" + paymentBenchRunID
	groupCode := "bench_vkma_group_" + paymentBenchRunID
	itemID := "bench_vkma_item_" + paymentBenchRunID
	productTitleKey := productID + ".title"
	productDescriptionKey := productID + ".description"
	itemTitleKey := itemID + ".title"
	itemDescriptionKey := itemID + ".description"
	now := time.Now()
	availableFrom := now.Add(-24 * time.Hour)
	availableUntil := now.Add(365 * 24 * time.Hour)
	priceStartsAt := now.Add(-24 * time.Hour)
	priceEndsAt := now.Add(365 * 24 * time.Hour)

	err := env.api.Admin.SaveProductGroup(env.ctx, product.UpsertGroupParams{
		WorkspaceID: benchWorkspaceID,
		Code:        groupCode,
		IsActive:    true,
	})
	benchNoError(b, err)

	err = env.api.Admin.SaveProduct(env.ctx, product.UpsertParams{
		WorkspaceID:    benchWorkspaceID,
		ID:             productID,
		GroupCode:      &groupCode,
		TitleKey:       productTitleKey,
		DescriptionKey: &productDescriptionKey,
		Position:       1,
		GlobalInterval: "UNLIMITED",
		UserInterval:   "UNLIMITED",
		AvailableFrom:  &availableFrom,
		AvailableUntil: &availableUntil,
		IsVisible:      true,
	})
	benchNoError(b, err)

	for _, localization := range []product.UpsertLocalizationParams{
		{WorkspaceID: benchWorkspaceID, Locale: "ru", LocalizationKey: productTitleKey, Value: "Benchmark VKMA product"},
		{WorkspaceID: benchWorkspaceID, Locale: "ru", LocalizationKey: productDescriptionKey, Value: "Benchmark VKMA product description"},
		{WorkspaceID: benchWorkspaceID, Locale: "ru", LocalizationKey: itemTitleKey, Value: "Benchmark VKMA item"},
		{WorkspaceID: benchWorkspaceID, Locale: "ru", LocalizationKey: itemDescriptionKey, Value: "Benchmark VKMA item description"},
	} {
		err = env.api.Admin.SaveLocalization(env.ctx, localization)
		benchNoError(b, err)
	}

	err = env.api.Admin.AttachProductItem(env.ctx, product.AddItemParams{
		WorkspaceID: benchWorkspaceID,
		ProductID:   productID,
		ItemID:      itemID,
		Quantity:    1,
	})
	benchNoError(b, err)

	_, err = env.api.Admin.CreateCatalogPrice(env.ctx, product.CreatePriceParams{
		WorkspaceID:         benchWorkspaceID,
		ProductID:           productID,
		AssetCode:           paymentvkma.AssetCode,
		ListAmountMinor:     35,
		DiscountAmountMinor: 0,
		StartsAt:            &priceStartsAt,
		EndsAt:              &priceEndsAt,
	})
	benchNoError(b, err)

	return productID
}
