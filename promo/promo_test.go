package promo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/elum2b/services/internal/testsupport"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/promo/repository"
	"github.com/elum2b/services/promo/service/admin"
	"github.com/elum2b/services/promo/service/user"
)

func TestIsReady(t *testing.T) {
	var nilService *Promo

	if nilService.IsReady() {
		t.Fatal("nil promo must not be ready")
	}

	service := New()
	if service.IsReady() {
		t.Fatal("uninitialized promo must not be ready")
	}

	if _, err := service.Admin.GetPromo(
		context.Background(),
		"00000000-0000-0000-0000-000000000001",
		1,
	); !errors.Is(
		err,
		sqlwrap.ErrServiceNotReady,
	) {
		t.Fatalf("unready promo admin error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	service.rootCtx, service.client, service.Admin, service.User = ctx, &sqlwrap.Client{}, &admin.Admin{}, &user.User{}

	if !service.IsReady() {
		t.Fatal("initialized promo must be ready")
	}

	cancel()

	if service.IsReady() {
		t.Fatal("closed promo must not be ready")
	}
}

func TestBootstrapIsIdempotent(t *testing.T) {
	_ = newPromoTestService(t)

	db, err := openPromoPostgres(promoTestDB)
	if err != nil {
		t.Fatalf("open bootstrap test database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	client, err := sqlwrap.New(db)
	if err != nil {
		t.Fatalf("create bootstrap client: %v", err)
	}

	t.Cleanup(func() { _ = client.Close() })

	if err := repository.New(client).
		Bootstrap(context.Background()); err != nil {
		t.Fatalf("repeat promo bootstrap: %v", err)
	}
}

func TestBootstrapRejectsIncompatibleEnum(t *testing.T) {
	_ = newPromoTestService(t)

	db, err := openPromoPostgres(promoTestDB)
	if err != nil {
		t.Fatalf("open bootstrap test database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.ExecContext(
		context.Background(),
		"ALTER TYPE promo_reward_type ADD VALUE 'unexpected'",
	); err != nil {
		t.Fatalf("make enum incompatible: %v", err)
	}

	client, err := sqlwrap.New(db)
	if err != nil {
		t.Fatalf("create bootstrap client: %v", err)
	}

	t.Cleanup(func() { _ = client.Close() })

	if err := repository.New(client).
		Bootstrap(context.Background()); err == nil {
		t.Fatal("bootstrap accepted an incompatible promo_reward_type enum")
	}
}

func TestCallbackWorkerDoesNotLeaseFifthRouteEvent(t *testing.T) {
	_ = newPromoTestService(t)

	db, err := openPromoPostgres(promoTestDB)
	if err != nil {
		t.Fatalf("open callback test database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	store := callbackutil.NewWithTable(db, callbackutil.PromoTable)
	workspaceID := testsupport.WorkspaceID("callback-route-capacity")
	payload := []byte(`{"app_id":77,"platform_id":88}`)
	releaseByID := make(map[uint64]chan struct{}, 6)
	firstRouteIDs := make([]uint64, 0, 5)

	for index := range 5 {
		eventKey := fmt.Sprintf("callback.capacity:%d", index)

		id, err := store.CreateEvent(ctx, callbackutil.CreateParams{
			WorkspaceID:    workspaceID,
			SourceService:  "promo",
			EventType:      "promo.capacity_test",
			EventKey:       eventKey,
			IdempotencyKey: eventKey,
			Payload:        payload,
		})
		if err != nil {
			t.Fatalf("create callback event %d: %v", index, err)
		}

		firstRouteIDs = append(firstRouteIDs, id)
		releaseByID[id] = make(chan struct{}, 1)
	}

	otherEventKey := "callback.capacity:other-route"

	otherRouteID, err := store.CreateEvent(ctx, callbackutil.CreateParams{
		WorkspaceID:    workspaceID,
		SourceService:  "promo",
		EventType:      "promo.capacity_test",
		EventKey:       otherEventKey,
		IdempotencyKey: otherEventKey,
		Payload:        []byte(`{"app_id":99,"platform_id":88}`),
	})
	if err != nil {
		t.Fatalf("create other-route callback event: %v", err)
	}

	releaseByID[otherRouteID] = make(chan struct{}, 1)

	fifthRouteID := firstRouteIDs[4]

	workerCtx, cancel := context.WithCancel(ctx)
	started := make(chan uint64, 6)
	workerDone := make(chan error, 1)

	go func() {
		workerDone <- store.On(
			workerCtx,
			func(callbackCtx callbackutil.Context) error {
				started <- callbackCtx.EventID

				select {
				case <-releaseByID[callbackCtx.EventID]:
					return callbackCtx.Successful()
				case <-callbackCtx.Done():
					return callbackCtx.Err()
				}
			},
			callbackutil.WithSourceService("promo"),
			callbackutil.WithIdleDelay(10*time.Millisecond),
		)
	}()

	t.Cleanup(func() {
		for _, release := range releaseByID {
			select {
			case release <- struct{}{}:
			default:
			}
		}

		cancel()
		<-workerDone
	})

	startedIDs := make(map[uint64]struct{}, 5)

	for index := range 5 {
		select {
		case id := <-started:
			startedIDs[id] = struct{}{}
		case <-time.After(2 * time.Second):
			t.Fatalf("callback %d did not start", index+1)
		}
	}

	if _, ok := startedIDs[otherRouteID]; !ok {
		t.Fatal("other route was blocked by the saturated route")
	}

	if _, ok := startedIDs[fifthRouteID]; ok {
		t.Fatal("fifth callback started before a route slot was released")
	}

	select {
	case id := <-started:
		t.Fatalf(
			"fifth callback %d started before a route slot was released",
			id,
		)
	case <-time.After(100 * time.Millisecond):
	}

	var pendingCount, processingCount, pendingAttempts, pendingLocks int

	if err := db.QueryRowContext(ctx, `
SELECT
    COUNT(*) FILTER (WHERE status = 'pending'),
    COUNT(*) FILTER (WHERE status = 'processing'),
    COALESCE(SUM(attempt_count) FILTER (WHERE status = 'pending'), 0),
    COUNT(locked_until) FILTER (WHERE status = 'pending')
FROM promo_clb_event
WHERE workspace_id = $1 AND event_type = 'promo.capacity_test'`,
		workspaceID,
	).Scan(
		&pendingCount,
		&processingCount,
		&pendingAttempts,
		&pendingLocks,
	); err != nil {
		t.Fatalf("read callback lease state: %v", err)
	}

	if pendingCount != 1 || processingCount != 5 ||
		pendingAttempts != 0 || pendingLocks != 0 {
		t.Fatalf(
			"callback state pending=%d processing=%d attempts=%d locks=%d",
			pendingCount,
			processingCount,
			pendingAttempts,
			pendingLocks,
		)
	}

	releaseByID[firstRouteIDs[0]] <- struct{}{}

	select {
	case id := <-started:
		if id != fifthRouteID {
			t.Fatalf("callback %d started instead of fifth route event", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fifth callback did not start after a route slot was released")
	}
}

func TestCallbackBootstrapBackfillsRoutingKey(t *testing.T) {
	_ = newPromoTestService(t)

	db, err := openPromoPostgres(promoTestDB)
	if err != nil {
		t.Fatalf("open callback migration database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(
		ctx,
		"DROP INDEX IF EXISTS promo_clb_event_route_due_idx",
	); err != nil {
		t.Fatalf("drop routing index: %v", err)
	}

	if _, err := db.ExecContext(
		ctx,
		"ALTER TABLE promo_clb_event DROP COLUMN routing_key",
	); err != nil {
		t.Fatalf("drop routing key: %v", err)
	}

	workspaceID := testsupport.WorkspaceID("callback-routing-migration")
	if _, err := db.ExecContext(ctx, `
INSERT INTO promo_clb_event (
    workspace_id,
    source_service,
    event_type,
    event_key,
    idempotency_key,
    payload
) VALUES ($1, 'promo', 'promo.migration_test', $2, $2, $3)`,
		workspaceID,
		"promo.migration_test:1",
		[]byte(`{"app_id":17,"platform_id":19}`),
	); err != nil {
		t.Fatalf("insert legacy callback: %v", err)
	}

	if err := callbackutil.BootstrapTable(
		ctx,
		db,
		callbackutil.PromoTable,
	); err != nil {
		t.Fatalf("migrate callback routing key: %v", err)
	}

	var routingKey, nullable string

	if err := db.QueryRowContext(ctx, `
SELECT routing_key
FROM promo_clb_event
WHERE event_key = 'promo.migration_test:1'`).Scan(&routingKey); err != nil {
		t.Fatalf("read migrated routing key: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
SELECT is_nullable
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND table_name = 'promo_clb_event'
  AND column_name = 'routing_key'`).Scan(&nullable); err != nil {
		t.Fatalf("read routing key nullability: %v", err)
	}

	want := workspaceID + ":17:19"
	if routingKey != want || nullable != "NO" {
		t.Fatalf("routing key = %q, nullable = %q", routingKey, nullable)
	}
}

func TestPromoRunBlocksUntilContextCanceled(t *testing.T) {
	newPromoTestService(t)

	params := DatabaseParams{
		User:     promoTestPGUser,
		Password: promoTestPGPassword,
		Database: promoTestDB,
		Host:     promoTestPGHost,
		Port:     promoTestPGPort,
		Options:  promoTestOptions(),
	}
	service := New()
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- service.Run(runCtx, params)
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

	if err := service.Run(
		context.Background(),
		params,
	); !errors.Is(
		err,
		ErrServiceRunning,
	) {
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

	promoID, err := nodeA.Admin.CreatePromo(
		context.Background(),
		admin.SavePromoParams{
			WorkspaceID: testsupport.WorkspaceID("cache-workspace"),
			Code:        "CACHE",
			Payload:     json.RawMessage(`{"version":1}`),
			IsActive:    true,
		},
	)
	if err != nil {
		t.Fatalf("create cached promo: %v", err)
	}

	if err := nodeA.Admin.UpsertLocalization(
		context.Background(),
		admin.SaveLocalizationParams{
			WorkspaceID: testsupport.WorkspaceID("cache-workspace"),
			PromoID:     promoID,
			Locale:      "ru",
			Title:       "Old title",
		},
	); err != nil {
		t.Fatalf("create cached promo localization: %v", err)
	}

	if err := nodeA.Admin.UpsertReward(
		context.Background(),
		admin.SaveRewardParams{
			WorkspaceID: testsupport.WorkspaceID("cache-workspace"),
			PromoID:     promoID,
			Key:         "stars",
			Quantity:    1,
		},
	); err != nil {
		t.Fatalf("create cached promo reward: %v", err)
	}

	assertPromoCacheRead(t, nodeB, promoID, "Old title", 1)

	if _, err := nodeA.Admin.UpdatePromo(
		context.Background(),
		admin.SavePromoParams{
			ID:          promoID,
			WorkspaceID: testsupport.WorkspaceID("cache-workspace"),
			Code:        "CACHE",
			Payload:     json.RawMessage(`{"version":2}`),
			IsActive:    true,
		},
	); err != nil {
		t.Fatalf("update cached promo: %v", err)
	}

	if err := nodeA.Admin.UpsertLocalization(
		context.Background(),
		admin.SaveLocalizationParams{
			WorkspaceID: testsupport.WorkspaceID("cache-workspace"),
			PromoID:     promoID,
			Locale:      "ru",
			Title:       "New title",
		},
	); err != nil {
		t.Fatalf("update cached promo localization: %v", err)
	}

	if err := nodeA.Admin.UpsertReward(
		context.Background(),
		admin.SaveRewardParams{
			WorkspaceID: testsupport.WorkspaceID("cache-workspace"),
			PromoID:     promoID,
			Key:         "stars",
			Quantity:    2,
		},
	); err != nil {
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

	result, err := repository.New(service.client).Import(
		context.Background(),
		testsupport.WorkspaceID("large-workspace"),
		admin.ImportRequest{
			Package: admin.ExportPackage{
				Format:  repository.ExportFormat,
				Service: "promo",
				Promos:  promos,
			},
			ConflictStrategy: repository.ImportConflictUpdate,
		},
	)
	if err != nil {
		t.Fatalf("import large promo package: %v", err)
	}

	if result.Imported.Promos != promoCount {
		t.Fatalf(
			"imported promos = %d, want %d",
			result.Imported.Promos,
			promoCount,
		)
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
		_, err := repository.New(service.client).
			Import(ctx, workspaceID, admin.ImportRequest{
				Package: admin.ExportPackage{
					Format:  repository.ExportFormat,
					Service: "promo",
					Promos: []repository.ExportPromo{
						{
							Code:     "IMPORT",
							Payload:  json.RawMessage(`{}`),
							IsActive: true,
						},
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

	values, err := service.Admin.ListPromos(
		ctx,
		workspaceID,
		admin.Page{Limit: 10},
	)
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
			t.Fatalf(
				"promo lock waiters = %d, want at least %d",
				waiting,
				minimum,
			)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func assertPromoCacheRead(
	t *testing.T,
	service *Promo,
	promoID uint64,
	title string,
	quantity int64,
) {
	t.Helper()

	value, err := service.Admin.GetPromo(
		context.Background(),
		testsupport.WorkspaceID("cache-workspace"),
		promoID,
	)
	if err != nil || len(value.Localizations) != 1 ||
		value.Localizations[0].Title != title ||
		len(value.Rewards) != 1 ||
		value.Rewards[0].Quantity != quantity {
		t.Fatalf(
			"promo node returned stale catalog: promo=%+v err=%v",
			value,
			err,
		)
	}
}

const (
	promoTestPGHost     = "localhost"
	promoTestPGPort     = 5432
	promoTestPGUser     = "postgres"
	promoTestPGPassword = "RBTX0DXKbagvCy2XCAi4qHt0cjeSD6bU"
	promoTestDB         = "promo_test"
)

func TestPromoIdentityCollisionIsolation(t *testing.T) {
	service := newPromoTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("promo-identity-collision")

	promoID, err := service.Admin.CreatePromo(ctx, admin.SavePromoParams{
		WorkspaceID:    workspaceID,
		Code:           "IDENTITY-COLLISION",
		Payload:        json.RawMessage(`{}`),
		MaxActivations: 10,
		IsActive:       true,
	})
	if err != nil {
		t.Fatalf("create collision promo: %v", err)
	}

	identities := []user.Identity{
		{
			WorkspaceID:    workspaceID,
			AppID:          100,
			PlatformID:     200,
			PlatformUserID: "shared-user",
		},
		{
			WorkspaceID:    workspaceID,
			AppID:          101,
			PlatformID:     200,
			PlatformUserID: "shared-user",
		},
		{
			WorkspaceID:    workspaceID,
			AppID:          100,
			PlatformID:     201,
			PlatformUserID: "shared-user",
		},
		{
			WorkspaceID:    workspaceID,
			AppID:          100,
			PlatformID:     200,
			PlatformUserID: "other-user",
		},
	}
	redemptions := make(map[uint64]struct{}, len(identities))

	for _, identity := range identities {
		applied, err := service.User.Apply(ctx, user.ApplyParams{
			Identity: identity,
			Code:     "IDENTITY-COLLISION",
			Locale:   "ru",
		})
		if err != nil {
			t.Fatalf("apply promo for %#v: %v", identity, err)
		}

		if applied.Status != repository.StatusSuccess ||
			applied.Redemption == nil {
			t.Fatalf(
				"identity %#v collided during apply: %+v",
				identity,
				applied,
			)
		}

		if _, exists := redemptions[applied.Redemption.ID]; exists {
			t.Fatalf(
				"redemption %d reused across identities",
				applied.Redemption.ID,
			)
		}

		redemptions[applied.Redemption.ID] = struct{}{}

		repeated, err := service.User.Apply(ctx, user.ApplyParams{
			Identity: identity,
			Code:     "IDENTITY-COLLISION",
			Locale:   "ru",
		})
		if err != nil {
			t.Fatalf("repeat promo for %#v: %v", identity, err)
		}

		if repeated.Status != repository.StatusAlreadyApplied ||
			repeated.Redemption == nil ||
			repeated.Redemption.ID != applied.Redemption.ID {
			t.Fatalf(
				"identity %#v did not retain redemption: %+v",
				identity,
				repeated,
			)
		}

		redemption, err := service.Admin.GetUserRedemption(
			ctx,
			identity,
			promoID,
		)
		if err != nil || redemption == nil ||
			redemption.ID != applied.Redemption.ID {
			t.Fatalf(
				"identity %#v redemption=%+v err=%v",
				identity,
				redemption,
				err,
			)
		}
	}
}

func TestPromoApplyLifecycleAndCallback(t *testing.T) {
	service := newPromoTestService(t)
	ctx := context.Background()

	promoID, err := service.Admin.CreatePromo(ctx, admin.SavePromoParams{
		WorkspaceID: testsupport.WorkspaceID("workspace-a"),
		Code:        "SUMMER2026",
		Payload: json.RawMessage(
			`{"image":"summer.png"}`,
		),
		MaxActivations: 10,
		IsActive:       true,
	})
	if err != nil {
		t.Fatalf("create promo: %v", err)
	}

	if err := service.Admin.UpsertLocalization(
		ctx,
		admin.SaveLocalizationParams{
			WorkspaceID: testsupport.WorkspaceID(
				"workspace-a",
			),
			PromoID:     promoID,
			Locale:      "ru",
			Title:       "Летний промо",
			Description: "Описание",
		},
	); err != nil {
		t.Fatalf("upsert localization: %v", err)
	}

	if err := service.Admin.UpsertReward(ctx, admin.SaveRewardParams{
		WorkspaceID: testsupport.WorkspaceID(
			"workspace-a",
		),
		PromoID:  promoID,
		Key:      "coin",
		Quantity: 100,
	}); err != nil {
		t.Fatalf("upsert reward: %v", err)
	}

	reward, err := service.Admin.GetReward(
		ctx,
		testsupport.WorkspaceID("workspace-a"),
		promoID,
		"coin",
	)
	if err != nil || reward.Key != "coin" || reward.Quantity != 100 {
		t.Fatalf("get reward: %+v, err=%v", reward, err)
	}

	identity := user.Identity{
		WorkspaceID: testsupport.WorkspaceID(
			"workspace-a",
		),
		AppID:          1,
		PlatformID:     2,
		PlatformUserID: "player",
	}

	first, err := service.User.Apply(
		ctx,
		user.ApplyParams{
			Identity: identity,
			Code:     " summer2026 ",
			Locale:   "ru",
		},
	)
	if err != nil {
		t.Fatalf("apply promo: %v", err)
	}

	if first.Status != repository.StatusSuccess || first.Promo.ID != promoID ||
		first.Promo.Title != "Летний промо" || len(first.Promo.Rewards) != 1 {
		t.Fatalf("unexpected successful result: %+v", first)
	}

	second, err := service.User.Apply(
		ctx,
		user.ApplyParams{Identity: identity, Code: "SUMMER2026", Locale: "ru"},
	)
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
		WorkspaceID: testsupport.WorkspaceID(
			"workspace-a",
		),
		PromoID:  promoID,
		Key:      "coin",
		Quantity: 999,
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

	refreshAt := time.Now()
	if err := service.Admin.RefreshDailyStats(
		ctx,
		testsupport.WorkspaceID("workspace-a"),
		refreshAt,
		refreshAt,
	); err != nil {
		t.Fatalf("refresh daily stats: %v", err)
	}

	daily, err := service.Admin.ListDailyStats(
		ctx,
		testsupport.WorkspaceID("workspace-a"),
		promoID,
		time.Now().Add(-24*time.Hour),
		time.Now().Add(24*time.Hour),
	)
	if err != nil || len(daily) != 1 || daily[0].RedemptionCount != 1 ||
		daily[0].UniqueUsers != 1 {
		t.Fatalf("daily stats: %+v, err=%v", daily, err)
	}

	events, err := service.Admin.ListCallbackEvents(
		ctx,
		admin.CallbackEventListParams{
			WorkspaceID: testsupport.WorkspaceID("workspace-a"),
			Page:        admin.Page{Limit: 10},
		},
	)
	if err != nil {
		t.Fatalf("list callback events: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("callback count = %d, want 1", len(events))
	}

	workerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err = service.OnCallback(workerCtx, func(callbackCtx Context) error {
		if callbackCtx.Applied == nil ||
			callbackCtx.Applied.PromoID != promoID ||
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
		WorkspaceID: testsupport.WorkspaceID(
			"workspace-export",
		),
		Code: "EXPORT2026",
		Payload: json.RawMessage(
			`{"source":"export"}`,
		),
		MaxActivations: 5,
		IsActive:       true,
	})
	if err != nil {
		t.Fatalf("create promo: %v", err)
	}

	if err := service.Admin.UpsertLocalization(
		ctx,
		admin.SaveLocalizationParams{
			WorkspaceID: testsupport.WorkspaceID(
				"workspace-export",
			),
			PromoID:     promoID,
			Locale:      "ru",
			Title:       "Промо",
			Description: "Описание",
		},
	); err != nil {
		t.Fatalf("upsert localization: %v", err)
	}

	if err := service.Admin.UpsertReward(ctx, admin.SaveRewardParams{
		WorkspaceID: testsupport.WorkspaceID(
			"workspace-export",
		),
		PromoID:  promoID,
		Key:      "stars",
		Quantity: 25,
		Scale:    2,
	}); err != nil {
		t.Fatalf("upsert reward: %v", err)
	}

	pkg, err := repository.New(service.client).Export(
		ctx,
		testsupport.WorkspaceID("workspace-export"),
		admin.ExportRequest{},
	)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	preview, err := repository.New(service.client).PreviewImport(
		ctx,
		testsupport.WorkspaceID("workspace-import"),
		pkg,
	)
	if err != nil || preview.Counts.Promos != 1 || len(preview.Conflicts) != 0 {
		t.Fatalf("preview import: value=%+v err=%v", preview, err)
	}

	if _, err := repository.New(service.client).Import(
		ctx,
		testsupport.WorkspaceID("workspace-import"),
		admin.ImportRequest{
			Package: pkg, ConflictStrategy: repository.ImportConflictUpdate,
		},
	); err != nil {
		t.Fatalf("import: %v", err)
	}

	imported, err := repository.New(service.client).Export(
		ctx,
		testsupport.WorkspaceID("workspace-import"),
		admin.ExportRequest{},
	)
	if err != nil {
		t.Fatalf("export imported: %v", err)
	}

	if len(imported.Promos) != 1 || len(imported.Promos[0].Localization) != 1 ||
		len(
			imported.Promos[0].Rewards,
		) != 1 || imported.Promos[0].Rewards[0].Scale != 2 {
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
		t.Fatalf(
			"apply imported promo before replacement: result=%+v err=%v",
			beforeReplace,
			err,
		)
	}

	pkg.Promos[0].Localization = nil
	pkg.Promos[0].Rewards = nil

	if _, err := repository.New(service.client).Import(
		ctx,
		testsupport.WorkspaceID("workspace-import"),
		admin.ImportRequest{
			Package:          pkg,
			ConflictStrategy: repository.ImportConflictUpdate,
		},
	); err != nil {
		t.Fatalf("replace imported promo: %v", err)
	}

	replaced, err := repository.New(service.client).Export(
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
		t.Fatalf(
			"update_existing kept removed promo children: %+v",
			replaced.Promos,
		)
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
		t.Fatalf(
			"replace changed redemption snapshot: result=%+v err=%v",
			afterReplace,
			err,
		)
	}
}

func TestPromoStatusesAndAdminCRUD(t *testing.T) {
	service := newPromoTestService(t)
	ctx := context.Background()
	identity := user.Identity{
		WorkspaceID:    testsupport.WorkspaceID("workspace-s"),
		AppID:          1,
		PlatformID:     1,
		PlatformUserID: "user",
	}

	assertStatus := func(code string, expected string) {
		t.Helper()

		result, err := service.User.Apply(
			ctx,
			user.ApplyParams{Identity: identity, Code: code, Locale: "ru"},
		)
		if err != nil {
			t.Fatalf("apply %s: %v", code, err)
		}

		if result.Status != expected {
			t.Fatalf(
				"status for %s = %s, want %s",
				code,
				result.Status,
				expected,
			)
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
		{
			"future",
			true,
			timePtr(now.Add(time.Hour)),
			nil,
			repository.StatusNotStarted,
		},
		{
			"expired",
			true,
			nil,
			timePtr(now.Add(-time.Hour)),
			repository.StatusExpired,
		},
	}

	for _, item := range cases {
		_, err := service.Admin.CreatePromo(ctx, admin.SavePromoParams{
			WorkspaceID: identity.WorkspaceID,
			Code:        item.code,
			Payload:     json.RawMessage(`{}`),
			IsActive:    item.active,
			StartAt:     item.start,
			EndAt:       item.end,
		})
		if err != nil {
			t.Fatalf("create %s: %v", item.code, err)
		}

		assertStatus(item.code, item.expected)
	}

	promoID, err := service.Admin.CreatePromo(ctx, admin.SavePromoParams{
		WorkspaceID:    identity.WorkspaceID,
		Code:           "crud",
		Payload:        json.RawMessage(`{"v":1}`),
		MaxActivations: 1,
		IsActive:       true,
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

	if err := service.Admin.UpsertLocalization(
		ctx,
		admin.SaveLocalizationParams{
			WorkspaceID: identity.WorkspaceID,
			PromoID:     promoID,
			Locale:      "ru",
			Title:       "Title",
		},
	); err != nil {
		t.Fatalf("upsert localization: %v", err)
	}

	if _, err := service.Admin.GetLocalization(
		ctx,
		identity.WorkspaceID,
		promoID,
		"ru",
	); err != nil {
		t.Fatalf("get localization: %v", err)
	}

	localizations, err := service.Admin.ListLocalizations(
		ctx,
		identity.WorkspaceID,
		promoID,
	)
	if err != nil || len(localizations) != 1 ||
		localizations[0].Title != "Title" {
		t.Fatalf("list localizations: values=%+v err=%v", localizations, err)
	}

	if err := service.Admin.UpsertReward(ctx, admin.SaveRewardParams{
		WorkspaceID: identity.WorkspaceID,
		PromoID:     promoID,
		Key:         "gem",
		Quantity:    5,
	}); err != nil {
		t.Fatalf("upsert reward: %v", err)
	}

	rewards, err := service.Admin.ListRewards(
		ctx,
		identity.WorkspaceID,
		promoID,
	)
	if err != nil || len(rewards) != 1 || rewards[0].Key != "gem" {
		t.Fatalf("list rewards: values=%+v err=%v", rewards, err)
	}

	if _, err := service.Admin.GetPromo(
		ctx,
		identity.WorkspaceID,
		promoID,
	); err != nil {
		t.Fatalf("get promo: %v", err)
	}

	stats, err := service.Admin.GetStats(ctx, identity.WorkspaceID, promoID)
	if err != nil || stats.RemainingActivations == nil ||
		*stats.RemainingActivations != 1 {
		t.Fatalf("get stats before activation: %+v, err=%v", stats, err)
	}

	if _, err := service.Admin.ListPromos(
		ctx,
		identity.WorkspaceID,
		admin.Page{Limit: 10},
	); err != nil {
		t.Fatalf("list promos: %v", err)
	}

	if _, err := service.Admin.DeleteReward(
		ctx,
		identity.WorkspaceID,
		promoID,
		"gem",
	); err != nil {
		t.Fatalf("delete reward: %v", err)
	}

	if _, err := service.Admin.DeleteLocalization(
		ctx,
		identity.WorkspaceID,
		promoID,
		"ru",
	); err != nil {
		t.Fatalf("delete localization: %v", err)
	}

	if _, err := service.Admin.DeletePromo(
		ctx,
		identity.WorkspaceID,
		promoID,
	); err != nil {
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

	events, err := service.Admin.ListCallbackEvents(
		ctx,
		admin.CallbackEventListParams{
			WorkspaceID: workspaceID,
			Page:        admin.Page{Limit: 10},
		},
	)
	if err != nil || len(events) != 3 {
		t.Fatalf("list callback events: values=%+v err=%v", events, err)
	}

	loaded, err := service.Admin.GetCallbackEvent(
		ctx,
		workspaceID,
		events[0].ID,
	)
	if err != nil || loaded.ID != events[0].ID {
		t.Fatalf("get callback event: value=%+v err=%v", loaded, err)
	}

	if changed, err := service.Admin.RetryCallbackEventNow(
		ctx,
		workspaceID,
		events[0].ID,
	); err != nil ||
		changed != 1 {
		t.Fatalf("retry callback: changed=%d err=%v", changed, err)
	}

	if changed, err := service.Admin.MarkCallbackEventOK(
		ctx,
		workspaceID,
		events[0].ID,
	); err != nil ||
		changed != 1 {
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

	if changed, err := service.Admin.ResetExpiredCallbackProcessing(
		ctx,
		workspaceID,
	); err != nil ||
		changed != 1 {
		t.Fatalf("reset callback processing: changed=%d err=%v", changed, err)
	}
}

func TestPromoConcurrentLifetimeLimit(t *testing.T) {
	service := newPromoTestService(t)
	ctx := context.Background()

	promoID, err := service.Admin.CreatePromo(ctx, admin.SavePromoParams{
		WorkspaceID: testsupport.WorkspaceID(
			"workspace-limit",
		),
		Code:           "LIMITED",
		Payload:        json.RawMessage(`{}`),
		MaxActivations: 3,
		IsActive:       true,
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
					WorkspaceID: testsupport.WorkspaceID(
						"workspace-limit",
					),
					AppID:          1,
					PlatformID:     1,
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

	stats, err := service.Admin.GetStats(
		ctx,
		testsupport.WorkspaceID("workspace-limit"),
		promoID,
	)
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}

	if stats.ActivationCount != 3 || stats.RemainingActivations == nil ||
		*stats.RemainingActivations != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	redemptions, err := service.Admin.ListRedemptions(
		ctx,
		testsupport.WorkspaceID("workspace-limit"),
		promoID,
		admin.Page{Limit: 20},
	)
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

	_, _ = adminDB.ExecContext(
		ctx,
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
		promoTestDB,
	)
	if _, err := adminDB.ExecContext(
		ctx,
		"DROP DATABASE IF EXISTS "+promoTestDB,
	); err != nil {
		t.Fatalf("drop database: %v", err)
	}

	if _, err := adminDB.ExecContext(
		ctx,
		"CREATE DATABASE "+promoTestDB,
	); err != nil {
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
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		promoTestPGHost,
		promoTestPGPort,
		promoTestPGUser,
		promoTestPGPassword,
		database,
	)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func timePtr(value time.Time) *time.Time { return &value }

func TestPromoConcurrentSameIdentity(t *testing.T) {
	service := newPromoTestService(t)
	ctx := context.Background()
	workspace := testsupport.WorkspaceID("same-identity-audit")
	_, err := service.Admin.CreatePromo(ctx, admin.SavePromoParams{WorkspaceID: workspace, Code: "ONCE", Payload: json.RawMessage(`{}`), IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	params := user.ApplyParams{Identity: user.Identity{WorkspaceID: workspace, AppID: 1, PlatformID: 1, PlatformUserID: "same"}, Code: "ONCE"}
	db, err := openPromoPostgres(promoTestDB)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT id FROM promo_offer WHERE workspace_id=$1 FOR UPDATE", workspace); err != nil {
		t.Fatal(err)
	}
	const n = 4
	start := make(chan struct{})
	results := make(chan string, n)
	errs := make(chan error, n)
	for range n {
		go func() { <-start; result, err := service.User.Apply(ctx, params); results <- result.Status; errs <- err }()
	}
	close(start)
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%promo_offer%'").Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= n {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d blocked apply calls", waiting)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	success := 0
	for range n {
		if err := <-errs; err != nil {
			t.Errorf("concurrent replay: %v", err)
		}
		switch status := <-results; status {
		case repository.StatusSuccess:
			success++
		case repository.StatusAlreadyApplied:
		default:
			t.Errorf("unexpected status %s", status)
		}
	}
	if success != 1 {
		t.Fatalf("successful redemptions = %d, want 1", success)
	}
}

func TestPromoDefaultImportMergesCatalog(t *testing.T) {
	service := newPromoTestService(t)
	repo := repository.New(service.client)
	ctx := context.Background()
	workspace := testsupport.WorkspaceID("audit-catalog-merge")
	makePackage := func(keys []string, active bool) repository.ExportPackage {
		pkg := repository.ExportPackage{Format: repository.ExportFormat, Service: "promo"}
		for _, key := range keys {
			pkg.Promos = append(pkg.Promos, repository.ExportPromo{Code: key, IsActive: active, Payload: json.RawMessage(`{}`)})
		}
		return pkg
	}
	for _, pkg := range []repository.ExportPackage{makePackage([]string{"A", "B", "C"}, true), makePackage([]string{"B", "C", "D"}, false)} {
		if _, err := repo.Import(ctx, workspace, repository.ImportRequest{Package: pkg}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.Export(ctx, workspace, repository.ExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Promos) != 4 {
		t.Fatalf("catalog size = %d, want 4", len(got.Promos))
	}
	seen := map[string]bool{}
	for _, item := range got.Promos {
		seen[item.Code] = true
		if item.IsActive != (item.Code == "A") {
			t.Fatalf("import priority wrong for %s", item.Code)
		}
	}
	for _, key := range []string{"A", "B", "C", "D"} {
		if !seen[key] {
			t.Fatalf("missing catalog key %s", key)
		}
	}
}

func TestPromoArchiveJobsRoundTrip(t *testing.T) {
	options := promoTestOptions()

	options.ArchiveDirectory = t.TempDir()

	service := newPromoTestServiceWithOptions(t, options)
	ctx := context.Background()
	source := testsupport.WorkspaceID("archive-source")
	target := testsupport.WorkspaceID("archive-target")

	if _, err := service.Admin.CreatePromo(
		ctx,
		admin.SavePromoParams{
			WorkspaceID:    source,
			Code:           "ARCHIVE",
			Payload:        []byte(`{}`),
			MaxActivations: 1,
			IsActive:       true,
		},
	); err != nil {
		t.Fatal(err)
	}

	applied, err := service.User.Apply(ctx, user.ApplyParams{
		Identity: user.Identity{
			WorkspaceID:    source,
			AppID:          1,
			PlatformID:     1,
			PlatformUserID: "archive-user",
		},
		Code: "ARCHIVE",
	})
	if err != nil || applied.Status != repository.StatusSuccess {
		t.Fatalf("apply source promo: result=%+v err=%v", applied, err)
	}

	export, err := service.Admin.QueueArchiveExport(
		ctx,
		admin.QueueArchiveExportParams{
			WorkspaceID: source,
			FileName:    "promo.zip",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if export.Status != jobs.StatusQueued {
		t.Fatalf("export status = %s, want queued", export.Status)
	}

	worker := startPromoArchiveWorker(t, options)

	waitPromoArchiveJob(t, worker, source, export.ID)

	dump, _, err := service.Admin.DownloadArchive(ctx, source, export.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer dump.Close()

	importJob, err := service.Admin.QueueArchiveImport(
		ctx,
		admin.QueueArchiveImportParams{
			WorkspaceID: target,
			FileName:    "promo.zip",
			ImportRequest: admin.ImportRequest{
				ConflictStrategy: "update_existing",
			},
			Archive: dump,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	waitPromoArchiveJob(t, worker, target, importJob.ID)

	pkg, err := repository.New(service.client).
		Export(ctx, target, admin.ExportRequest{})
	if err != nil || len(pkg.Promos) != 1 ||
		pkg.Promos[0].ActivationCount != 1 {
		t.Fatalf("imported package = %+v, err = %v", pkg, err)
	}
}

func startPromoArchiveWorker(t *testing.T, options Options) *Promo {
	t.Helper()

	service := New()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- service.Run(ctx, DatabaseParams{
			User:     promoTestPGUser,
			Password: promoTestPGPassword,
			Database: promoTestDB,
			Host:     promoTestPGHost,
			Port:     promoTestPGPort,
			Options:  options,
		})
	}()

	deadline := time.Now().Add(5 * time.Second)

	for !service.IsReady() {
		select {
		case err := <-done:
			cancel()
			t.Fatalf("start promo archive worker: %v", err)
		default:
		}

		if time.Now().After(deadline) {
			cancel()
			t.Fatal("promo archive worker did not become ready")
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Cleanup(func() {
		cancel()

		if err := <-done; err != nil {
			t.Errorf("stop promo archive worker: %v", err)
		}
	})

	return service
}

func waitPromoArchiveJob(
	t *testing.T,
	service *Promo,
	workspaceID string,
	id int64,
) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, err := service.Admin.ArchiveJob(
			context.Background(),
			workspaceID,
			id,
		)
		if err == nil && job.Status == jobs.StatusCompleted {
			return
		}

		if err == nil && job.Status == jobs.StatusFailed {
			t.Fatalf("archive job failed: %s", job.Error)
		}

		time.Sleep(25 * time.Millisecond)
	}

	t.Fatal("archive job did not complete")
}

func TestPromoImportNeverDecreasesActivationCount(t *testing.T) {
	service := newPromoTestService(t)
	ctx := context.Background()
	repo := repository.New(service.client)
	workspace := testsupport.WorkspaceID("import-activation-floor")
	pkg := repository.ExportPackage{Format: repository.ExportFormat, Service: "promo", Promos: []repository.ExportPromo{{Code: "KEEP", Payload: json.RawMessage(`{}`), MaxActivations: 3, IsActive: true}}}
	apply := func(w, who string) user.ApplyResult {
		t.Helper()
		result, err := service.User.Apply(ctx, user.ApplyParams{Identity: user.Identity{WorkspaceID: w, AppID: 1, PlatformID: 1, PlatformUserID: who}, Code: "KEEP"})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	importPackage := func(w string) {
		t.Helper()
		if _, err := repo.Import(ctx, w, repository.ImportRequest{Package: pkg}); err != nil {
			t.Fatal(err)
		}
	}
	importPackage(workspace)
	for i := range 3 {
		if got := apply(workspace, fmt.Sprint(i)); got.Status != repository.StatusSuccess {
			t.Fatalf("seed activation: %s", got.Status)
		}
	}
	for _, count := range []uint64{0, 1, 3} {
		pkg.Promos[0].ActivationCount = count
		pkg.Promos[0].Payload = json.RawMessage(`{"imported":true}`)
		importPackage(workspace)
		exported, err := repo.Export(ctx, workspace, repository.ExportRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if len(exported.Promos) != 1 || exported.Promos[0].ActivationCount != 3 {
			t.Fatalf("counter changed incorrectly: %+v", exported.Promos)
		}
		var settings struct {
			Imported bool `json:"imported"`
		}
		if err := json.Unmarshal(exported.Promos[0].Payload, &settings); err != nil || !settings.Imported {
			t.Fatalf("imported settings not applied: %+v err=%v", settings, err)
		}
		if got := apply(workspace, "new"); got.Status != repository.StatusLimitReached {
			t.Fatalf("import restored spent limit: %s", got.Status)
		}
		if got := apply(workspace, "0"); got.Status != repository.StatusAlreadyApplied {
			t.Fatalf("import lost redemption: %s", got.Status)
		}
	}
	// A higher archive value must still win, including on a fresh target.
	pkg.Promos[0].ActivationCount = 5
	pkg.Promos[0].MaxActivations = 6
	for _, target := range []string{workspace, testsupport.WorkspaceID("fresh-activation-floor")} {
		importPackage(target)
		got := apply(target, "last")
		if got.Status != repository.StatusSuccess || got.Promo.ActivationCount != 6 {
			t.Fatalf("higher imported counter was not preserved: %+v", got.Promo)
		}
		if got := apply(target, "overflow"); got.Status != repository.StatusLimitReached {
			t.Fatalf("higher imported limit: %s", got.Status)
		}
	}
}
