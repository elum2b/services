package promo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/elum2b/services/internal/testsupport"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/promo/repository"
	"github.com/elum2b/services/promo/service/admin"
	"github.com/elum2b/services/promo/service/user"
	_ "github.com/jackc/pgx/v5/stdlib"
	"sync"
	"testing"
	"time"
)

func TestIsReady(t *testing.T) {
	var nilService *Promo
	if nilService.IsReady() {
		t.Fatal("nil promo must not be ready")
	}
	service := New(DatabaseParams{})
	if service.IsReady() {
		t.Fatal("uninitialized promo must not be ready")
	}
	ctx, cancel := context.WithCancel(context.Background())
	service.rootCtx, service.Admin, service.User = ctx, &admin.Admin{}, &user.User{}
	if !service.IsReady() {
		t.Fatal("initialized promo must be ready")
	}
	cancel()
	if service.IsReady() {
		t.Fatal("closed promo must not be ready")
	}
}

func TestPromoRunBlocksUntilContextCanceled(t *testing.T) {
	newPromoTestService(t)
	service := New(DatabaseParams{
		User:     promoTestPGUser,
		Password: promoTestPGPassword,
		Database: promoTestDB,
		Host:     promoTestPGHost,
		Port:     promoTestPGPort,
		Options:  promoTestOptions(),
	})
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.Run(runCtx)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for !service.IsReady() {
		select {
		case err := <-done:
			cancel()
			t.Fatalf("Run returned before readiness: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("promo service did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := service.Run(context.Background()); !errors.Is(err, ErrServiceRunning) {
		cancel()
		t.Fatalf("second Run error = %v, want ErrServiceRunning", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("promo Run did not stop after cancellation")
	}
}

func TestPromoCacheVersionInvalidatesOtherNode(t *testing.T) {
	cache := testsupport.NewCache()
	options := promoTestOptions()
	options.Cache = cache
	options.CacheL2Delay = time.Minute
	nodeA := newPromoTestServiceWithOptions(t, options)
	db, err := openPromoPostgres(promoTestDB)
	if err != nil {
		t.Fatalf("open second promo node database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	nodeB, err := NewWithDatabase(context.Background(), db, options)
	if err != nil {
		t.Fatalf("create second promo node: %v", err)
	}
	t.Cleanup(func() { _ = nodeB.Close() })

	promoID, err := nodeA.Admin.CreatePromo(context.Background(), admin.SavePromoParams{
		WorkspaceID: testsupport.WorkspaceID("cache-workspace"),
		Code:        "CACHE",
		Payload:     json.RawMessage(`{"version":1}`),
		IsActive:    true,
	})
	if err != nil {
		t.Fatalf("create cached promo: %v", err)
	}
	if err := nodeA.Admin.UpsertLocalization(context.Background(), admin.SaveLocalizationParams{
		WorkspaceID: testsupport.WorkspaceID("cache-workspace"),
		PromoID:     promoID,
		Locale:      "ru",
		Title:       "Old title",
	}); err != nil {
		t.Fatalf("create cached promo localization: %v", err)
	}
	if err := nodeA.Admin.UpsertReward(context.Background(), admin.SaveRewardParams{
		WorkspaceID: testsupport.WorkspaceID("cache-workspace"),
		PromoID:     promoID,
		Key:         "stars",
		Quantity:    1,
	}); err != nil {
		t.Fatalf("create cached promo reward: %v", err)
	}
	assertPromoCacheRead(t, nodeB, promoID, "Old title", 1)

	if _, err := nodeA.Admin.UpdatePromo(context.Background(), admin.SavePromoParams{
		ID:          promoID,
		WorkspaceID: testsupport.WorkspaceID("cache-workspace"),
		Code:        "CACHE",
		Payload:     json.RawMessage(`{"version":2}`),
		IsActive:    true,
	}); err != nil {
		t.Fatalf("update cached promo: %v", err)
	}
	if err := nodeA.Admin.UpsertLocalization(context.Background(), admin.SaveLocalizationParams{
		WorkspaceID: testsupport.WorkspaceID("cache-workspace"),
		PromoID:     promoID,
		Locale:      "ru",
		Title:       "New title",
	}); err != nil {
		t.Fatalf("update cached promo localization: %v", err)
	}
	if err := nodeA.Admin.UpsertReward(context.Background(), admin.SaveRewardParams{
		WorkspaceID: testsupport.WorkspaceID("cache-workspace"),
		PromoID:     promoID,
		Key:         "stars",
		Quantity:    2,
	}); err != nil {
		t.Fatalf("update cached promo reward: %v", err)
	}
	assertPromoCacheRead(t, nodeB, promoID, "New title", 2)
}

func TestPromoImportBatchesMoreThanPostgresParameterLimit(t *testing.T) {
	options := promoTestOptions()
	options.QueryTimeout = 30 * time.Second
	service := newPromoTestServiceWithOptions(t, options)
	const promoCount = 6667
	promos := make([]repository.ExportPromo, 0, promoCount)
	for index := range promoCount {
		promos = append(promos, repository.ExportPromo{
			Code:     fmt.Sprintf("LARGE%05d", index),
			Payload:  json.RawMessage(`{}`),
			IsActive: true,
		})
	}

	result, err := service.Admin.Import(context.Background(), testsupport.WorkspaceID("large-workspace"), admin.ImportRequest{
		Package: admin.ExportPackage{
			Format:  repository.ExportFormat,
			Service: "promo",
			Promos:  promos,
		},
		ConflictStrategy: repository.ImportConflictUpdate,
	})
	if err != nil {
		t.Fatalf("import large promo package: %v", err)
	}
	if result.Imported.Promos != promoCount {
		t.Fatalf("imported promos = %d, want %d", result.Imported.Promos, promoCount)
	}
}

func TestPromoImportSerializesWithAdminWrite(t *testing.T) {
	service := newPromoTestService(t)
	db, err := openPromoPostgres(promoTestDB)
	if err != nil {
		t.Fatalf("open promo lock database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("concurrent-workspace")

	transaction, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin promo lock transaction: %v", err)
	}
	t.Cleanup(func() { _ = transaction.Rollback() })
	if _, err := transaction.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		"promo:"+workspaceID,
	); err != nil {
		t.Fatalf("lock promo workspace: %v", err)
	}

	importResult := make(chan error, 1)
	go func() {
		_, err := service.Admin.Import(ctx, workspaceID, admin.ImportRequest{
			Package: admin.ExportPackage{
				Format:  repository.ExportFormat,
				Service: "promo",
				Promos: []repository.ExportPromo{
					{Code: "IMPORT", Payload: json.RawMessage(`{}`), IsActive: true},
				},
			},
			ConflictStrategy: repository.ImportConflictUpdate,
		})
		importResult <- err
	}()
	waitForPromoWorkspaceLock(t, db, 1)

	adminResult := make(chan error, 1)
	go func() {
		_, err := service.Admin.CreatePromo(ctx, admin.SavePromoParams{
			WorkspaceID: workspaceID,
			Code:        "ADMIN",
			Payload:     json.RawMessage(`{}`),
			IsActive:    true,
		})
		adminResult <- err
	}()
	waitForPromoWorkspaceLock(t, db, 2)

	if err := transaction.Commit(); err != nil {
		t.Fatalf("release promo workspace lock: %v", err)
	}
	if err := <-importResult; err != nil {
		t.Fatalf("concurrent promo import: %v", err)
	}
	if err := <-adminResult; err != nil {
		t.Fatalf("concurrent promo admin write: %v", err)
	}

	values, err := service.Admin.ListPromos(ctx, workspaceID, admin.Page{Limit: 10})
	if err != nil || len(values) != 2 {
		t.Fatalf("concurrent promo result: promos=%+v err=%v", values, err)
	}
}

func waitForPromoWorkspaceLock(t *testing.T, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, minimum int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting int
		if err := db.QueryRowContext(context.Background(), `
SELECT COUNT(*)
FROM pg_stat_activity
WHERE datname = current_database()
  AND wait_event_type = 'Lock'
  AND query LIKE '%pg_advisory_xact_lock%'`).Scan(&waiting); err != nil {
			t.Fatalf("inspect promo lock waiters: %v", err)
		}
		if waiting >= minimum {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("promo lock waiters = %d, want at least %d", waiting, minimum)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertPromoCacheRead(t *testing.T, service *Promo, promoID uint64, title string, quantity int64) {
	t.Helper()
	value, err := service.Admin.GetPromo(context.Background(), testsupport.WorkspaceID("cache-workspace"), promoID)
	if err != nil || len(value.Localizations) != 1 || value.Localizations[0].Title != title ||
		len(value.Rewards) != 1 || value.Rewards[0].Quantity != quantity {
		t.Fatalf("promo node returned stale catalog: promo=%+v err=%v", value, err)
	}
}

const (
	promoTestPGHost     = "localhost"
	promoTestPGPort     = 5432
	promoTestPGUser     = "postgres"
	promoTestPGPassword = "RBTX0DXKbagvCy2XCAi4qHt0cjeSD6bU"
	promoTestDB         = "promo_test"
)

func TestPromoApplyLifecycleAndCallback(t *testing.T) {
	service := newPromoTestService(t)
	ctx := context.Background()
	promoID, err := service.Admin.CreatePromo(ctx, admin.SavePromoParams{
		WorkspaceID: testsupport.WorkspaceID("workspace-a"), Code: "SUMMER2026",
		Payload: json.RawMessage(`{"image":"summer.png"}`), MaxActivations: 10, IsActive: true,
	})
	if err != nil {
		t.Fatalf("create promo: %v", err)
	}
	if err := service.Admin.UpsertLocalization(ctx, admin.SaveLocalizationParams{
		WorkspaceID: testsupport.WorkspaceID("workspace-a"), PromoID: promoID, Locale: "ru",
		Title: "Летний промо", Description: "Описание",
	}); err != nil {
		t.Fatalf("upsert localization: %v", err)
	}
	if err := service.Admin.UpsertReward(ctx, admin.SaveRewardParams{
		WorkspaceID: testsupport.WorkspaceID("workspace-a"), PromoID: promoID, Key: "coin", Quantity: 100,
	}); err != nil {
		t.Fatalf("upsert reward: %v", err)
	}
	reward, err := service.Admin.GetReward(ctx, testsupport.WorkspaceID("workspace-a"), promoID, "coin")
	if err != nil || reward.Key != "coin" || reward.Quantity != 100 {
		t.Fatalf("get reward: %+v, err=%v", reward, err)
	}
	identity := user.Identity{
		WorkspaceID: testsupport.WorkspaceID("workspace-a"), AppID: 1, PlatformID: 2, PlatformUserID: "player",
	}
	first, err := service.User.Apply(ctx, user.ApplyParams{Identity: identity, Code: " summer2026 ", Locale: "ru"})
	if err != nil {
		t.Fatalf("apply promo: %v", err)
	}
	if first.Status != repository.StatusSuccess || first.Promo.ID != promoID ||
		first.Promo.Title != "Летний промо" || len(first.Promo.Rewards) != 1 {
		t.Fatalf("unexpected successful result: %+v", first)
	}
	second, err := service.User.Apply(ctx, user.ApplyParams{Identity: identity, Code: "SUMMER2026", Locale: "ru"})
	if err != nil {
		t.Fatalf("apply promo again: %v", err)
	}
	if second.Status != repository.StatusAlreadyApplied ||
		second.Redemption == nil || first.Redemption.ID != second.Redemption.ID {
		t.Fatalf("unexpected repeated result: %+v", second)
	}
	if !first.Redemption.RedeemedAt.Equal(second.Redemption.RedeemedAt) {
		t.Fatalf(
			"redemption timestamp changed between apply calls: first=%s second=%s",
			first.Redemption.RedeemedAt,
			second.Redemption.RedeemedAt,
		)
	}
	if err := service.Admin.UpsertReward(ctx, admin.SaveRewardParams{
		WorkspaceID: testsupport.WorkspaceID("workspace-a"), PromoID: promoID, Key: "coin", Quantity: 999,
	}); err != nil {
		t.Fatalf("update reward after redemption: %v", err)
	}
	afterRewardUpdate, err := service.User.Apply(ctx, user.ApplyParams{
		Identity: identity,
		Code:     "SUMMER2026",
		Locale:   "ru",
	})
	if err != nil {
		t.Fatalf("apply promo after reward update: %v", err)
	}
	if afterRewardUpdate.Status != repository.StatusAlreadyApplied ||
		len(afterRewardUpdate.Promo.Rewards) != 1 ||
		afterRewardUpdate.Promo.Rewards[0].Key != "coin" ||
		afterRewardUpdate.Promo.Rewards[0].Quantity != 100 {
		t.Fatalf("redemption reward snapshot changed: %+v", afterRewardUpdate)
	}
	redemption, err := service.Admin.GetUserRedemption(ctx, identity, promoID)
	if err != nil || redemption == nil || redemption.ID != first.Redemption.ID {
		t.Fatalf("get user redemption: %+v, err=%v", redemption, err)
	}
	if err := service.Admin.RefreshDailyStats(ctx, testsupport.WorkspaceID("workspace-a"), time.Now().Add(-time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("refresh daily stats: %v", err)
	}
	daily, err := service.Admin.ListDailyStats(
		ctx, testsupport.WorkspaceID("workspace-a"), promoID, time.Now().Add(-24*time.Hour), time.Now().Add(24*time.Hour),
	)
	if err != nil || len(daily) != 1 || daily[0].RedemptionCount != 1 || daily[0].UniqueUsers != 1 {
		t.Fatalf("daily stats: %+v, err=%v", daily, err)
	}
	events, err := service.Admin.ListCallbackEvents(ctx, admin.CallbackEventListParams{
		WorkspaceID: testsupport.WorkspaceID("workspace-a"),
		Page:        admin.Page{Limit: 10},
	})
	if err != nil {
		t.Fatalf("list callback events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("callback count = %d, want 1", len(events))
	}

	workerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err = service.OnCallback(workerCtx, func(callbackCtx Context) error {
		if callbackCtx.Applied == nil || callbackCtx.Applied.PromoID != promoID ||
			len(callbackCtx.Applied.Rewards) != 1 ||
			callbackCtx.Applied.Rewards[0].Key != "coin" ||
			callbackCtx.Applied.Rewards[0].Quantity != 100 {
			return errors.New("callback payload is incomplete")
		}
		if err := callbackCtx.Successful(); err != nil {
			return err
		}
		cancel()
		return nil
	},
		WithCallbackWorkerID("promo-test-worker"),
		WithCallbackBatchSize(10),
		WithCallbackLeaseTimeout(time.Second),
		WithCallbackIdleDelay(10*time.Millisecond),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("OnCallback error = %v", err)
	}
}

func TestPromoImportExportCycle(t *testing.T) {
	service := newPromoTestService(t)
	ctx := context.Background()
	promoID, err := service.Admin.CreatePromo(ctx, admin.SavePromoParams{
		WorkspaceID: testsupport.WorkspaceID("workspace-export"), Code: "EXPORT2026",
		Payload: json.RawMessage(`{"source":"export"}`), MaxActivations: 5, IsActive: true,
	})
	if err != nil {
		t.Fatalf("create promo: %v", err)
	}
	if err := service.Admin.UpsertLocalization(ctx, admin.SaveLocalizationParams{
		WorkspaceID: testsupport.WorkspaceID("workspace-export"), PromoID: promoID, Locale: "ru",
		Title: "Промо", Description: "Описание",
	}); err != nil {
		t.Fatalf("upsert localization: %v", err)
	}
	if err := service.Admin.UpsertReward(ctx, admin.SaveRewardParams{
		WorkspaceID: testsupport.WorkspaceID("workspace-export"), PromoID: promoID, Key: "stars", Quantity: 25, Scale: 2,
	}); err != nil {
		t.Fatalf("upsert reward: %v", err)
	}
	pkg, err := service.Admin.Export(ctx, testsupport.WorkspaceID("workspace-export"), admin.ExportRequest{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	preview, err := service.Admin.PreviewImport(
		ctx,
		testsupport.WorkspaceID("workspace-import"),
		pkg,
	)
	if err != nil || preview.Counts.Promos != 1 || len(preview.Conflicts) != 0 {
		t.Fatalf("preview import: value=%+v err=%v", preview, err)
	}
	if _, err := service.Admin.Import(ctx, testsupport.WorkspaceID("workspace-import"), admin.ImportRequest{
		Package: pkg, ConflictStrategy: repository.ImportConflictUpdate,
	}); err != nil {
		t.Fatalf("import: %v", err)
	}
	imported, err := service.Admin.Export(ctx, testsupport.WorkspaceID("workspace-import"), admin.ExportRequest{})
	if err != nil {
		t.Fatalf("export imported: %v", err)
	}
	if len(imported.Promos) != 1 || len(imported.Promos[0].Localization) != 1 ||
		len(imported.Promos[0].Rewards) != 1 || imported.Promos[0].Rewards[0].Scale != 2 {
		t.Fatalf("unexpected imported package: %+v", imported)
	}
	importIdentity := user.Identity{
		WorkspaceID:    testsupport.WorkspaceID("workspace-import"),
		AppID:          7,
		PlatformID:     8,
		PlatformUserID: "import-history",
	}
	beforeReplace, err := service.User.Apply(ctx, user.ApplyParams{
		Identity: importIdentity,
		Code:     "EXPORT2026",
		Locale:   "ru",
	})
	if err != nil || beforeReplace.Status != repository.StatusSuccess {
		t.Fatalf("apply imported promo before replacement: result=%+v err=%v", beforeReplace, err)
	}

	pkg.Promos[0].Localization = nil
	pkg.Promos[0].Rewards = nil
	if _, err := service.Admin.Import(ctx, testsupport.WorkspaceID("workspace-import"), admin.ImportRequest{
		Package:          pkg,
		ConflictStrategy: repository.ImportConflictUpdate,
	}); err != nil {
		t.Fatalf("replace imported promo: %v", err)
	}
	replaced, err := service.Admin.Export(
		ctx,
		testsupport.WorkspaceID("workspace-import"),
		admin.ExportRequest{},
	)
	if err != nil {
		t.Fatalf("export replaced promo: %v", err)
	}
	if len(replaced.Promos) != 1 ||
		len(replaced.Promos[0].Localization) != 0 ||
		len(replaced.Promos[0].Rewards) != 0 {
		t.Fatalf("update_existing kept removed promo children: %+v", replaced.Promos)
	}
	afterReplace, err := service.User.Apply(ctx, user.ApplyParams{
		Identity: importIdentity,
		Code:     "EXPORT2026",
		Locale:   "ru",
	})
	if err != nil ||
		afterReplace.Status != repository.StatusAlreadyApplied ||
		len(afterReplace.Promo.Rewards) != 1 ||
		afterReplace.Promo.Rewards[0].Key != "stars" {
		t.Fatalf("replace changed redemption snapshot: result=%+v err=%v", afterReplace, err)
	}
}

func TestPromoStatusesAndAdminCRUD(t *testing.T) {
	service := newPromoTestService(t)
	ctx := context.Background()
	identity := user.Identity{WorkspaceID: testsupport.WorkspaceID("workspace-s"), AppID: 1, PlatformID: 1, PlatformUserID: "user"}

	assertStatus := func(code string, expected string) {
		t.Helper()
		result, err := service.User.Apply(ctx, user.ApplyParams{Identity: identity, Code: code, Locale: "ru"})
		if err != nil {
			t.Fatalf("apply %s: %v", code, err)
		}
		if result.Status != expected {
			t.Fatalf("status for %s = %s, want %s", code, result.Status, expected)
		}
	}
	assertStatus("missing", repository.StatusNotFound)

	now := time.Now()
	cases := []struct {
		code     string
		active   bool
		start    *time.Time
		end      *time.Time
		expected string
	}{
		{"inactive", false, nil, nil, repository.StatusInactive},
		{"future", true, timePtr(now.Add(time.Hour)), nil, repository.StatusNotStarted},
		{"expired", true, nil, timePtr(now.Add(-time.Hour)), repository.StatusExpired},
	}
	for _, item := range cases {
		_, err := service.Admin.CreatePromo(ctx, admin.SavePromoParams{
			WorkspaceID: identity.WorkspaceID, Code: item.code, Payload: json.RawMessage(`{}`),
			IsActive: item.active, StartAt: item.start, EndAt: item.end,
		})
		if err != nil {
			t.Fatalf("create %s: %v", item.code, err)
		}
		assertStatus(item.code, item.expected)
	}

	promoID, err := service.Admin.CreatePromo(ctx, admin.SavePromoParams{
		WorkspaceID: identity.WorkspaceID, Code: "crud", Payload: json.RawMessage(`{"v":1}`),
		MaxActivations: 1, IsActive: true,
	})
	if err != nil {
		t.Fatalf("create CRUD promo: %v", err)
	}
	if _, err := service.Admin.UpdatePromo(ctx, admin.SavePromoParams{
		ID: promoID, WorkspaceID: identity.WorkspaceID, Code: "CRUD",
		Payload: json.RawMessage(`{"v":2}`), MaxActivations: 1, IsActive: true,
	}); err != nil {
		t.Fatalf("update promo: %v", err)
	}
	if err := service.Admin.UpsertLocalization(ctx, admin.SaveLocalizationParams{
		WorkspaceID: identity.WorkspaceID, PromoID: promoID, Locale: "ru", Title: "Title",
	}); err != nil {
		t.Fatalf("upsert localization: %v", err)
	}
	if _, err := service.Admin.GetLocalization(ctx, identity.WorkspaceID, promoID, "ru"); err != nil {
		t.Fatalf("get localization: %v", err)
	}
	localizations, err := service.Admin.ListLocalizations(ctx, identity.WorkspaceID, promoID)
	if err != nil || len(localizations) != 1 || localizations[0].Title != "Title" {
		t.Fatalf("list localizations: values=%+v err=%v", localizations, err)
	}
	if err := service.Admin.UpsertReward(ctx, admin.SaveRewardParams{
		WorkspaceID: identity.WorkspaceID, PromoID: promoID, Key: "gem", Quantity: 5,
	}); err != nil {
		t.Fatalf("upsert reward: %v", err)
	}
	rewards, err := service.Admin.ListRewards(ctx, identity.WorkspaceID, promoID)
	if err != nil || len(rewards) != 1 || rewards[0].Key != "gem" {
		t.Fatalf("list rewards: values=%+v err=%v", rewards, err)
	}
	if _, err := service.Admin.GetPromo(ctx, identity.WorkspaceID, promoID); err != nil {
		t.Fatalf("get promo: %v", err)
	}
	stats, err := service.Admin.GetStats(ctx, identity.WorkspaceID, promoID)
	if err != nil || stats.RemainingActivations == nil || *stats.RemainingActivations != 1 {
		t.Fatalf("get stats before activation: %+v, err=%v", stats, err)
	}
	if _, err := service.Admin.ListPromos(ctx, identity.WorkspaceID, admin.Page{Limit: 10}); err != nil {
		t.Fatalf("list promos: %v", err)
	}
	if _, err := service.Admin.DeleteReward(ctx, identity.WorkspaceID, promoID, "gem"); err != nil {
		t.Fatalf("delete reward: %v", err)
	}
	if _, err := service.Admin.DeleteLocalization(ctx, identity.WorkspaceID, promoID, "ru"); err != nil {
		t.Fatalf("delete localization: %v", err)
	}
	if _, err := service.Admin.DeletePromo(ctx, identity.WorkspaceID, promoID); err != nil {
		t.Fatalf("soft delete promo: %v", err)
	}
	assertStatus("crud", repository.StatusNotFound)
}

func TestPromoAdminCallbackControls(t *testing.T) {

	service := newPromoTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("callback-controls")
	promoID, err := service.Admin.CreatePromo(ctx, admin.SavePromoParams{
		WorkspaceID:    workspaceID,
		Code:           "CALLBACK-CONTROLS",
		Payload:        json.RawMessage(`{}`),
		MaxActivations: 10,
		IsActive:       true,
	})
	if err != nil {
		t.Fatalf("create promo: %v", err)
	}
	if err := service.Admin.UpsertReward(ctx, admin.SaveRewardParams{
		WorkspaceID: workspaceID,
		PromoID:     promoID,
		Key:         "coin",
		Quantity:    1,
	}); err != nil {
		t.Fatalf("create promo reward: %v", err)
	}

	for index := 0; index < 3; index++ {
		result, err := service.User.Apply(ctx, user.ApplyParams{
			Identity: user.Identity{
				WorkspaceID:    workspaceID,
				AppID:          1,
				PlatformID:     1,
				PlatformUserID: fmt.Sprintf("callback-user-%d", index),
			},
			Code: "CALLBACK-CONTROLS",
		})
		if err != nil || result.Status != repository.StatusSuccess {
			t.Fatalf("apply promo %d: result=%+v err=%v", index, result, err)
		}
	}

	events, err := service.Admin.ListCallbackEvents(ctx, admin.CallbackEventListParams{
		WorkspaceID: workspaceID,
		Page:        admin.Page{Limit: 10},
	})
	if err != nil || len(events) != 3 {
		t.Fatalf("list callback events: values=%+v err=%v", events, err)
	}
	loaded, err := service.Admin.GetCallbackEvent(ctx, workspaceID, events[0].ID)
	if err != nil || loaded.ID != events[0].ID {
		t.Fatalf("get callback event: value=%+v err=%v", loaded, err)
	}
	if changed, err := service.Admin.RetryCallbackEventNow(ctx, workspaceID, events[0].ID); err != nil || changed != 1 {
		t.Fatalf("retry callback: changed=%d err=%v", changed, err)
	}
	if changed, err := service.Admin.MarkCallbackEventOK(ctx, workspaceID, events[0].ID); err != nil || changed != 1 {
		t.Fatalf("mark callback ok: changed=%d err=%v", changed, err)
	}
	if changed, err := service.Admin.MarkCallbackEventReject(
		ctx,
		workspaceID,
		events[1].ID,
		"manual reject",
	); err != nil || changed != 1 {
		t.Fatalf("mark callback reject: changed=%d err=%v", changed, err)
	}

	db, err := openPromoPostgres(promoTestDB)
	if err != nil {
		t.Fatalf("open promo callback database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `
		UPDATE promo_clb_event
		SET status = 'processing', locked_until = now() - interval '1 minute'
		WHERE id = $1
	`, events[2].ID); err != nil {
		t.Fatalf("expire callback lease: %v", err)
	}
	if changed, err := service.Admin.ResetExpiredCallbackProcessing(ctx, workspaceID); err != nil || changed != 1 {
		t.Fatalf("reset callback processing: changed=%d err=%v", changed, err)
	}

}

func TestPromoConcurrentLifetimeLimit(t *testing.T) {
	service := newPromoTestService(t)
	ctx := context.Background()
	promoID, err := service.Admin.CreatePromo(ctx, admin.SavePromoParams{
		WorkspaceID: testsupport.WorkspaceID("workspace-limit"), Code: "LIMITED", Payload: json.RawMessage(`{}`),
		MaxActivations: 3, IsActive: true,
	})
	if err != nil {
		t.Fatalf("create limited promo: %v", err)
	}

	const workers = 12
	statuses := make(chan string, workers)
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			result, err := service.User.Apply(ctx, user.ApplyParams{
				Identity: user.Identity{
					WorkspaceID: testsupport.WorkspaceID("workspace-limit"), AppID: 1, PlatformID: 1,
					PlatformUserID: fmt.Sprintf("user-%d", index),
				},
				Code: "limited",
			})
			statuses <- result.Status
			errs <- err
		}(i)
	}
	wait.Wait()
	close(statuses)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent apply: %v", err)
		}
	}
	success := 0
	for status := range statuses {
		if status == repository.StatusSuccess {
			success++
		} else if status != repository.StatusLimitReached {
			t.Fatalf("unexpected status: %s", status)
		}
	}
	if success != 3 {
		t.Fatalf("successful activations = %d, want 3", success)
	}
	stats, err := service.Admin.GetStats(ctx, testsupport.WorkspaceID("workspace-limit"), promoID)
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.ActivationCount != 3 || stats.RemainingActivations == nil || *stats.RemainingActivations != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	redemptions, err := service.Admin.ListRedemptions(ctx, testsupport.WorkspaceID("workspace-limit"), promoID, admin.Page{Limit: 20})
	if err != nil || len(redemptions) != 3 {
		t.Fatalf("redemptions = %d, err=%v", len(redemptions), err)
	}
}

func newPromoTestService(t testing.TB) *Promo {
	return newPromoTestServiceWithOptions(t, promoTestOptions())
}

func newPromoTestServiceWithOptions(t testing.TB, options Options) *Promo {
	t.Helper()
	ctx := context.Background()
	adminDB, err := openPromoPostgres("postgres")
	if err != nil {
		t.Fatalf("open postgres admin: %v", err)
	}
	_, _ = adminDB.ExecContext(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()", promoTestDB)
	if _, err := adminDB.ExecContext(ctx, "DROP DATABASE IF EXISTS "+promoTestDB); err != nil {
		t.Fatalf("drop database: %v", err)
	}
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+promoTestDB); err != nil {
		t.Fatalf("create database: %v", err)
	}
	_ = adminDB.Close()
	db, err := openPromoPostgres(promoTestDB)
	if err != nil {
		t.Fatalf("open app postgres: %v", err)
	}
	client, err := sqlwrap.New(db, sqlwrap.Options{
		CacheEnabled:  true,
		CacheSize:     10000,
		CacheTTLCheck: time.Minute,
	})
	if err != nil {
		t.Fatalf("create sql client: %v", err)
	}
	repo := repository.New(client)
	if err := repo.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap promo: %v", err)
	}
	service, err := NewWithDatabase(ctx, db, options)
	if err != nil {
		t.Fatalf("create promo service: %v", err)
	}
	t.Cleanup(func() {
		_ = service.Close()
		_ = repo.Close()
		_ = client.Close()
	})
	return service
}

func promoTestOptions() Options {
	return Options{
		CacheEnabled:  true,
		CacheSize:     10000,
		CacheTTLCheck: time.Minute,
		CacheL1Delay:  time.Minute,
	}
}

func openPromoPostgres(database string) (*sql.DB, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", promoTestPGHost, promoTestPGPort, promoTestPGUser, promoTestPGPassword, database)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func timePtr(value time.Time) *time.Time { return &value }
