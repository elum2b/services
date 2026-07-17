package calendar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	services "github.com/elum2b/services"
	"github.com/elum2b/services/calendar/repository"
	"github.com/elum2b/services/calendar/service/admin"
	"github.com/elum2b/services/calendar/service/user"
	"github.com/elum2b/services/internal/testsupport"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"sync"
	"testing"
	"time"
)

func TestIsReady(t *testing.T) {
	var nilService *Calendar
	if nilService.IsReady() {
		t.Fatal("nil calendar must not be ready")
	}
	service := New(DatabaseParams{})
	if service.IsReady() {
		t.Fatal("uninitialized calendar must not be ready")
	}
	ctx, cancel := context.WithCancel(context.Background())
	service.rootCtx, service.Admin, service.User = ctx, &admin.Admin{}, &user.User{}
	if !service.IsReady() {
		t.Fatal("initialized calendar must be ready")
	}
	cancel()
	if service.IsReady() {
		t.Fatal("closed calendar must not be ready")
	}
}

func TestCalendarRunBlocksUntilContextCanceled(t *testing.T) {
	newCalendarTestService(t)
	service := New(DatabaseParams{
		User:     calendarTestPGUser,
		Password: calendarTestPGPassword,
		Database: calendarTestDB,
		Host:     calendarTestPGHost,
		Port:     calendarTestPGPort,
		Options:  calendarTestOptions(),
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
			t.Fatal("calendar service did not become ready")
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
		t.Fatal("calendar Run did not stop after cancellation")
	}
}

func TestCalendarCacheVersionInvalidatesOtherNode(t *testing.T) {
	cache := testsupport.NewCache()
	options := calendarTestOptions()
	options.Cache = cache
	options.CacheL2Delay = time.Minute
	nodeA := newCalendarTestServiceWithOptions(t, options)
	db, err := openCalendarPostgres(calendarTestDB)
	if err != nil {
		t.Fatalf("open second calendar node database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	nodeB, err := NewWithDatabase(context.Background(), db, options)
	if err != nil {
		t.Fatalf("create second calendar node: %v", err)
	}
	t.Cleanup(func() { _ = nodeB.Close() })

	calendarID, err := nodeA.Admin.CreateCalendar(context.Background(), admin.SaveCalendarParams{
		WorkspaceID:   testsupport.WorkspaceID("cache-workspace"),
		Type:          "daily",
		Mode:          repository.ModeSequential,
		IntervalType:  repository.IntervalFloating,
		IntervalUnit:  "day",
		IntervalCount: 1,
		EndBehavior:   repository.EndStop,
		Timezone:      "UTC",
		IsActive:      true,
	})
	if err != nil {
		t.Fatalf("create cached calendar: %v", err)
	}
	if err := nodeA.Admin.UpsertLocalization(context.Background(), admin.SaveLocalizationParams{
		WorkspaceID: testsupport.WorkspaceID("cache-workspace"),
		CalendarID:  calendarID,
		Locale:      "ru",
		Title:       "Old title",
	}); err != nil {
		t.Fatalf("create cached calendar localization: %v", err)
	}
	assertCalendarCacheRead(t, nodeB, "Old title")

	if err := nodeA.Admin.UpsertLocalization(context.Background(), admin.SaveLocalizationParams{
		WorkspaceID: testsupport.WorkspaceID("cache-workspace"),
		CalendarID:  calendarID,
		Locale:      "ru",
		Title:       "New title",
	}); err != nil {
		t.Fatalf("update cached calendar localization: %v", err)
	}
	assertCalendarCacheRead(t, nodeB, "New title")
}

func TestCalendarImportBatchesMoreThanPostgresParameterLimit(t *testing.T) {
	service := newCalendarTestService(t)
	const calendarCount = 5001
	values := make([]repository.ExportCalendar, 0, calendarCount)
	for index := 0; index < calendarCount; index++ {
		values = append(values, repository.ExportCalendar{
			Type:          fmt.Sprintf("large.%05d", index),
			Mode:          repository.ModeSequential,
			IntervalType:  repository.IntervalFloating,
			IntervalUnit:  "day",
			IntervalCount: 1,
			EndBehavior:   repository.EndStop,
			Timezone:      "UTC",
			IsActive:      true,
		})
	}

	result, err := service.Admin.Import(context.Background(), testsupport.WorkspaceID("large-workspace"), admin.ImportRequest{
		Package: admin.ExportPackage{
			Format:    repository.ExportFormat,
			Service:   "calendar",
			Calendars: values,
		},
		ConflictStrategy: repository.ImportConflictUpdate,
	})
	if err != nil {
		t.Fatalf("import large calendar package: %v", err)
	}
	if result.Imported.Calendars != calendarCount {
		t.Fatalf("imported calendars = %d, want %d", result.Imported.Calendars, calendarCount)
	}
}

func TestCalendarImportSerializesWithAdminWrite(t *testing.T) {
	service := newCalendarTestService(t)
	db, err := openCalendarPostgres(calendarTestDB)
	if err != nil {
		t.Fatalf("open calendar lock database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("concurrent-workspace")
	transaction, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin calendar lock transaction: %v", err)
	}
	t.Cleanup(func() { _ = transaction.Rollback() })
	if _, err := transaction.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "calendar:"+workspaceID); err != nil {
		t.Fatalf("lock calendar workspace: %v", err)
	}

	importResult := make(chan error, 1)
	go func() {
		_, err := service.Admin.Import(ctx, workspaceID, admin.ImportRequest{
			Package: admin.ExportPackage{
				Format:  repository.ExportFormat,
				Service: "calendar",
				Calendars: []repository.ExportCalendar{
					calendarImportTestValue("import"),
				},
			},
			ConflictStrategy: repository.ImportConflictUpdate,
		})
		importResult <- err
	}()
	waitForCalendarWorkspaceLock(t, db, 1)

	adminResult := make(chan error, 1)
	go func() {
		_, err := service.Admin.CreateCalendar(ctx, admin.SaveCalendarParams{
			WorkspaceID:   workspaceID,
			Type:          "admin",
			Mode:          repository.ModeSequential,
			IntervalType:  repository.IntervalFloating,
			IntervalUnit:  "day",
			IntervalCount: 1,
			EndBehavior:   repository.EndStop,
			Timezone:      "UTC",
			IsActive:      true,
		})
		adminResult <- err
	}()
	waitForCalendarWorkspaceLock(t, db, 2)

	if err := transaction.Commit(); err != nil {
		t.Fatalf("release calendar workspace lock: %v", err)
	}
	if err := <-importResult; err != nil {
		t.Fatalf("concurrent calendar import: %v", err)
	}
	if err := <-adminResult; err != nil {
		t.Fatalf("concurrent calendar admin write: %v", err)
	}
	values, err := service.Admin.ListCalendars(ctx, workspaceID, admin.Page{Limit: 10})
	if err != nil || len(values) != 2 {
		t.Fatalf("concurrent calendar result: values=%+v err=%v", values, err)
	}
}

func calendarImportTestValue(calendarType string) repository.ExportCalendar {
	return repository.ExportCalendar{
		Type:          calendarType,
		Mode:          repository.ModeSequential,
		IntervalType:  repository.IntervalFloating,
		IntervalUnit:  "day",
		IntervalCount: 1,
		EndBehavior:   repository.EndStop,
		Timezone:      "UTC",
		IsActive:      true,
	}
}

func waitForCalendarWorkspaceLock(t *testing.T, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, minimum int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting int
		if err := db.QueryRowContext(context.Background(), `
SELECT COUNT(*) FROM pg_stat_activity
WHERE datname = current_database()
  AND wait_event_type = 'Lock'
  AND query LIKE '%pg_advisory_xact_lock%'`).Scan(&waiting); err != nil {
			t.Fatalf("inspect calendar lock waiters: %v", err)
		}
		if waiting >= minimum {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("calendar lock waiters = %d, want at least %d", waiting, minimum)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertCalendarCacheRead(t *testing.T, service *Calendar, title string) {
	t.Helper()
	ctx := context.Background()
	active, err := service.User.ListActive(ctx, user.ListActiveParams{
		WorkspaceID: testsupport.WorkspaceID("cache-workspace"),
		Locale:      "ru",
	})
	if err != nil || len(active) != 1 || active[0].Title != title {
		t.Fatalf("calendar ListActive returned stale data: values=%+v err=%v", active, err)
	}
	value, err := service.User.GetCalendar(ctx, user.GetCalendarParams{
		Identity: services.Identity{
			WorkspaceID:    testsupport.WorkspaceID("cache-workspace"),
			AppID:          1,
			PlatformID:     1,
			PlatformUserID: "cache-user",
		},
		Ref:    "daily",
		Locale: "ru",
	})
	if err != nil || value.Title != title {
		t.Fatalf("calendar GetCalendar returned stale data: value=%+v err=%v", value, err)
	}
}

const (
	calendarTestPGHost     = "localhost"
	calendarTestPGPort     = 5432
	calendarTestPGUser     = "postgres"
	calendarTestPGPassword = "RBTX0DXKbagvCy2XCAi4qHt0cjeSD6bU"
	calendarTestDB         = "calendar_test"
)

func TestCalendarSequentialLifecycleAndCallback(t *testing.T) {
	service := newCalendarTestService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	calendarID := createCalendar(t, service, admin.SaveCalendarParams{
		WorkspaceID: testsupport.WorkspaceID("workspace-a"), Type: "daily", Mode: repository.ModeSequential,
		IntervalType: repository.IntervalFloating, IntervalUnit: "hour", IntervalCount: 1,
		EndBehavior: repository.EndStop, Timezone: "UTC", IsActive: true,
	})
	step1 := createStepReward(t, service, testsupport.WorkspaceID("workspace-a"), calendarID, 1, "coin", 100)
	createStepReward(t, service, testsupport.WorkspaceID("workspace-a"), calendarID, 2, "gem", 2)
	if err := service.Admin.UpsertLocalization(ctx, admin.SaveLocalizationParams{
		WorkspaceID: testsupport.WorkspaceID("workspace-a"), CalendarID: calendarID, Locale: "ru",
		Title: "Ежедневные награды", Description: "Описание",
	}); err != nil {
		t.Fatalf("upsert localization: %v", err)
	}
	identity := user.Identity{
		WorkspaceID: testsupport.WorkspaceID("workspace-a"), AppID: 1, PlatformID: 2, PlatformUserID: "player",
	}
	first := record(t, service, identity, "daily", "op-1", start)
	if !first.Granted || first.Status != repository.StatusGranted ||
		first.Position == nil || *first.Position != 1 || len(first.Rewards) != 1 {
		t.Fatalf("unexpected first result: %+v", first)
	}
	repeated := record(t, service, identity, calendarID, "op-1", start.Add(3*time.Hour))
	if !repeated.Granted || repeated.OperationRowID != first.OperationRowID ||
		repeated.Position == nil || *repeated.Position != 1 {
		t.Fatalf("unexpected repeated result: %+v", repeated)
	}
	blocked := record(t, service, identity, calendarID, "op-blocked", start.Add(10*time.Minute))
	if blocked.Granted || blocked.Status != repository.StatusNotAvailable {
		t.Fatalf("unexpected blocked result: %+v", blocked)
	}
	sameBlocked := record(t, service, identity, calendarID, "op-blocked", start.Add(2*time.Hour))
	if sameBlocked.Status != repository.StatusNotAvailable ||
		sameBlocked.OperationRowID != blocked.OperationRowID {
		t.Fatalf("denied idempotency changed: %+v", sameBlocked)
	}
	second := record(t, service, identity, calendarID, "op-2", start.Add(2*time.Hour))
	if !second.Granted || second.Position == nil || *second.Position != 2 ||
		!second.Progress.IsCompleted {
		t.Fatalf("unexpected second result: %+v", second)
	}
	if _, err := service.Admin.UpdateReward(ctx, admin.SaveRewardParams{
		ID: step1.rewardID, WorkspaceID: testsupport.WorkspaceID("workspace-a"), CalendarID: calendarID,
		StepID: step1.stepID, Key: "coin", Quantity: 999, Position: 1,
	}); err != nil {
		t.Fatalf("update reward: %v", err)
	}

	workerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := service.OnCallback(workerCtx, func(callbackCtx Context) error {
		if callbackCtx.RewardGranted == nil ||
			callbackCtx.RewardGranted.OperationID != "op-1" ||
			len(callbackCtx.RewardGranted.Rewards) != 1 ||
			callbackCtx.RewardGranted.Rewards[0].Quantity != 100 {
			return errors.New("unexpected callback snapshot")
		}
		if err := callbackCtx.Successful(); err != nil {
			return err
		}
		cancel()
		return nil
	},
		WithCallbackWorkerID("calendar-test-worker"),
		WithCallbackBatchSize(10),
		WithCallbackLeaseTimeout(time.Second),
		WithCallbackIdleDelay(10*time.Millisecond),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("OnCallback error = %v", err)
	}

	progress, err := service.User.GetProgress(ctx, user.GetProgressParams{Identity: identity, CalendarID: calendarID})
	if err != nil || progress == nil || progress.ClaimCount != 2 || !progress.IsCompleted {
		t.Fatalf("progress: %+v, err=%v", progress, err)
	}
	stats, err := service.Admin.GetStats(ctx, testsupport.WorkspaceID("workspace-a"), calendarID)
	if err != nil || stats.OperationCount != 3 || stats.GrantCount != 2 || stats.UniqueUsers != 1 {
		t.Fatalf("stats: %+v, err=%v", stats, err)
	}
	if err := service.Admin.RefreshDailyStats(ctx, testsupport.WorkspaceID("workspace-a"), start.Add(-time.Hour), start.Add(4*time.Hour)); err != nil {
		t.Fatalf("refresh daily stats: %v", err)
	}
	daily, err := service.Admin.ListDailyStats(
		ctx, testsupport.WorkspaceID("workspace-a"), calendarID, start.Add(-24*time.Hour), start.Add(24*time.Hour),
	)
	if err != nil || len(daily) != 1 || daily[0].GrantCount != 2 {
		t.Fatalf("daily stats: %+v, err=%v", daily, err)
	}
}

func TestCalendarSequentialSupportsSparsePositions(t *testing.T) {
	service := newCalendarTestService(t)
	workspaceID := testsupport.WorkspaceID("sparse-steps")
	calendarID := createCalendar(t, service, admin.SaveCalendarParams{
		WorkspaceID:   workspaceID,
		Type:          "sparse",
		Mode:          repository.ModeSequential,
		IntervalType:  repository.IntervalFloating,
		IntervalUnit:  "hour",
		IntervalCount: 1,
		EndBehavior:   repository.EndStop,
		Timezone:      "UTC",
		IsActive:      true,
	})
	createStepReward(t, service, workspaceID, calendarID, 1, "coin", 1)
	createStepReward(t, service, workspaceID, calendarID, 3, "gem", 1)

	identity := user.Identity{
		WorkspaceID:    workspaceID,
		AppID:          1,
		PlatformID:     1,
		PlatformUserID: "player",
	}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	first := record(t, service, identity, calendarID, "sparse-1", start)
	second := record(t, service, identity, calendarID, "sparse-2", start.Add(2*time.Hour))

	if !first.Granted || first.Position == nil || *first.Position != 1 {
		t.Fatalf("first sparse grant: %+v", first)
	}
	if !second.Granted || second.Position == nil || *second.Position != 3 {
		t.Fatalf("second sparse grant: %+v", second)
	}
}

func TestCalendarImportExportCycle(t *testing.T) {
	service := newCalendarTestService(t)
	ctx := context.Background()
	calendarID := createCalendar(t, service, admin.SaveCalendarParams{
		WorkspaceID: testsupport.WorkspaceID("workspace-export"), Type: "daily_export", Mode: repository.ModeSequential,
		IntervalType: repository.IntervalFloating, IntervalUnit: "day", IntervalCount: 1,
		EndBehavior: repository.EndStop, Timezone: "UTC", IsActive: true,
	})
	createStepReward(t, service, testsupport.WorkspaceID("workspace-export"), calendarID, 1, "stars", 25)
	if err := service.Admin.UpsertLocalization(ctx, admin.SaveLocalizationParams{
		WorkspaceID: testsupport.WorkspaceID("workspace-export"), CalendarID: calendarID, Locale: "ru",
		Title: "Календарь", Description: "Описание",
	}); err != nil {
		t.Fatalf("upsert localization: %v", err)
	}
	pkg, err := service.Admin.Export(ctx, testsupport.WorkspaceID("workspace-export"), admin.ExportRequest{})
	if err != nil {
		t.Fatalf("export: %v", err)
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
	if len(imported.Calendars) != 1 || len(imported.Calendars[0].Localization) != 1 ||
		len(imported.Calendars[0].Steps) != 1 || len(imported.Calendars[0].Steps[0].Rewards) != 1 {
		t.Fatalf("unexpected imported package: %+v", imported)
	}

	pkg.Calendars[0].Localization = nil
	pkg.Calendars[0].Steps = nil
	if _, err := service.Admin.Import(ctx, testsupport.WorkspaceID("workspace-import"), admin.ImportRequest{
		Package:          pkg,
		ConflictStrategy: repository.ImportConflictUpdate,
	}); err != nil {
		t.Fatalf("replace imported calendar: %v", err)
	}
	replaced, err := service.Admin.Export(
		ctx,
		testsupport.WorkspaceID("workspace-import"),
		admin.ExportRequest{},
	)
	if err != nil {
		t.Fatalf("export replaced calendar: %v", err)
	}
	if len(replaced.Calendars) != 1 ||
		len(replaced.Calendars[0].Localization) != 0 ||
		len(replaced.Calendars[0].Steps) != 0 {
		t.Fatalf("update_existing kept removed calendar children: %+v", replaced.Calendars)
	}
}

func TestCalendarIntervalAndResetModes(t *testing.T) {
	service := newCalendarTestService(t)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	identity := user.Identity{
		WorkspaceID: testsupport.WorkspaceID("workspace-modes"), AppID: 1, PlatformID: 1, PlatformUserID: "user",
	}

	intervalID := createCalendar(t, service, admin.SaveCalendarParams{
		WorkspaceID: identity.WorkspaceID, Type: "hourly", Mode: repository.ModeInterval,
		IntervalType: repository.IntervalCalendar, IntervalUnit: "hour", IntervalCount: 1,
		EndBehavior: repository.EndStop, Timezone: "UTC", IsActive: true, StartAt: &start,
	})
	createStepReward(t, service, identity.WorkspaceID, intervalID, 1, "coin", 1)
	createStepReward(t, service, identity.WorkspaceID, intervalID, 2, "coin", 2)
	first := record(t, service, identity, "hourly", "interval-1", start.Add(10*time.Minute))
	blocked := record(t, service, identity, "hourly", "interval-2", start.Add(20*time.Minute))
	second := record(t, service, identity, "hourly", "interval-3", start.Add(70*time.Minute))
	if !first.Granted || blocked.Status != repository.StatusNotAvailable ||
		!second.Granted || second.Position == nil || *second.Position != 2 {
		t.Fatalf("interval results: first=%+v blocked=%+v second=%+v", first, blocked, second)
	}

	resetID := createCalendar(t, service, admin.SaveCalendarParams{
		WorkspaceID: identity.WorkspaceID, Type: "reset", Mode: repository.ModeSequentialReset,
		IntervalType: repository.IntervalFloating, IntervalUnit: "hour", IntervalCount: 1,
		ResetAfterIntervals: 1, EndBehavior: repository.EndStop, Timezone: "UTC", IsActive: true,
	})
	createStepReward(t, service, identity.WorkspaceID, resetID, 1, "gem", 1)
	createStepReward(t, service, identity.WorkspaceID, resetID, 2, "gem", 2)
	resetFirst := record(t, service, identity, "reset", "reset-1", start)
	resetSecond := record(t, service, identity, "reset", "reset-2", start.Add(3*time.Hour))
	if !resetFirst.Granted || !resetSecond.Granted || !resetSecond.Progress.LastWasReset ||
		resetSecond.Position == nil || *resetSecond.Position != 1 ||
		resetSecond.Progress.ResetCount != 1 {
		t.Fatalf("reset results: first=%+v second=%+v", resetFirst, resetSecond)
	}
}

func TestCalendarConcurrentSingleIntervalGrant(t *testing.T) {
	service := newCalendarTestService(t)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	calendarID := createCalendar(t, service, admin.SaveCalendarParams{
		WorkspaceID: testsupport.WorkspaceID("workspace-concurrent"), Type: "concurrent",
		Mode: repository.ModeSequential, IntervalType: repository.IntervalFloating,
		IntervalUnit: "hour", IntervalCount: 1, EndBehavior: repository.EndRepeatLast,
		Timezone: "UTC", IsActive: true,
	})
	createStepReward(t, service, testsupport.WorkspaceID("workspace-concurrent"), calendarID, 1, "coin", 1)
	identity := user.Identity{
		WorkspaceID: testsupport.WorkspaceID("workspace-concurrent"), AppID: 1, PlatformID: 1, PlatformUserID: "same",
	}
	const workers = 8
	results := make(chan user.RecordResult, workers)
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for index := range workers {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			value, err := service.User.Record(context.Background(), user.RecordParams{
				Identity: identity, CalendarRef: calendarID,
				OperationID: fmt.Sprintf("concurrent-%d", index), Now: start,
			})
			results <- value
			errs <- err
		}(index)
	}
	wait.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent record: %v", err)
		}
	}
	granted := 0
	for result := range results {
		if result.Granted {
			granted++
		} else if result.Status != repository.StatusNotAvailable {
			t.Fatalf("unexpected concurrent status: %+v", result)
		}
	}
	if granted != 1 {
		t.Fatalf("granted = %d, want 1", granted)
	}
}

func TestCalendarStatusesVisibilityAndAdminCRUD(t *testing.T) {
	service := newCalendarTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	identity := user.Identity{
		WorkspaceID: testsupport.WorkspaceID("workspace-admin"), AppID: 9, PlatformID: 3, PlatformUserID: "player",
	}

	inactiveID := createCalendar(t, service, admin.SaveCalendarParams{
		WorkspaceID: identity.WorkspaceID, Type: "inactive", Mode: repository.ModeSequential,
		IntervalType: repository.IntervalFloating, IntervalUnit: "day",
		EndBehavior: repository.EndStop, Timezone: "UTC", IsActive: false,
	})
	createStepReward(t, service, identity.WorkspaceID, inactiveID, 1, "coin", 1)
	if value := record(t, service, identity, "inactive", "inactive-op", now); value.Status != repository.StatusInactive {
		t.Fatalf("inactive status: %+v", value)
	}

	future := now.Add(time.Hour)
	futureID := createCalendar(t, service, admin.SaveCalendarParams{
		WorkspaceID: identity.WorkspaceID, Type: "future", Mode: repository.ModeSequential,
		IntervalType: repository.IntervalFloating, IntervalUnit: "day",
		EndBehavior: repository.EndStop, Timezone: "UTC", IsActive: true, StartAt: &future,
	})
	createStepReward(t, service, identity.WorkspaceID, futureID, 1, "coin", 1)
	if value := record(t, service, identity, "future", "future-op", now); value.Status != repository.StatusNotStarted {
		t.Fatalf("future status: %+v", value)
	}

	past := now.Add(-time.Hour)
	expiredID := createCalendar(t, service, admin.SaveCalendarParams{
		WorkspaceID: identity.WorkspaceID, Type: "expired", Mode: repository.ModeSequential,
		IntervalType: repository.IntervalFloating, IntervalUnit: "day",
		EndBehavior: repository.EndStop, Timezone: "UTC", IsActive: true, EndAt: &past,
	})
	createStepReward(t, service, identity.WorkspaceID, expiredID, 1, "coin", 1)
	if value := record(t, service, identity, "expired", "expired-op", now); value.Status != repository.StatusExpired {
		t.Fatalf("expired status: %+v", value)
	}

	hiddenID := createCalendar(t, service, admin.SaveCalendarParams{
		WorkspaceID: identity.WorkspaceID, Type: "hidden", Mode: repository.ModeSequential,
		IntervalType: repository.IntervalFloating, IntervalUnit: "hour",
		EndBehavior: repository.EndRestart, Timezone: "UTC", IsActive: true,
		HideFutureRewards: true,
	})
	firstStep := createStepReward(t, service, identity.WorkspaceID, hiddenID, 1, "coin", 10)
	createStepReward(t, service, identity.WorkspaceID, hiddenID, 2, "gem", 20)
	createStepReward(t, service, identity.WorkspaceID, hiddenID, 3, "ticket", 30)
	if err := service.Admin.UpsertLocalization(ctx, admin.SaveLocalizationParams{
		WorkspaceID: identity.WorkspaceID, CalendarID: hiddenID,
		Locale: "ru", Title: "Скрытый", Description: "Описание",
	}); err != nil {
		t.Fatalf("localization: %v", err)
	}
	before, err := service.User.GetCalendar(ctx, user.GetCalendarParams{Identity: identity, Ref: hiddenID, Locale: "ru"})
	if err != nil || len(before.Steps) != 1 || before.Title != "Скрытый" {
		t.Fatalf("hidden before progress: %+v, err=%v", before, err)
	}
	record(t, service, identity, hiddenID, "hidden-1", now)
	after, err := service.User.GetCalendar(ctx, user.GetCalendarParams{Identity: identity, Ref: hiddenID, Locale: "ru"})
	if err != nil || len(after.Steps) != 2 {
		t.Fatalf("hidden after progress: %+v, err=%v", after, err)
	}
	next, err := service.User.Next(ctx, user.NextParams{
		Identity: identity, CalendarRef: hiddenID, Locale: "ru", Now: now.Add(2 * time.Hour),
	})
	if err != nil || !next.Granted || next.Position == nil || *next.Position != 2 {
		t.Fatalf("next: %+v, err=%v", next, err)
	}
	if _, err := service.Admin.UpdateStep(ctx, admin.SaveStepParams{
		WorkspaceID: identity.WorkspaceID, CalendarID: hiddenID,
		ID: firstStep.stepID, Position: 2,
	}); err == nil {
		t.Fatal("expected duplicate position conflict")
	}
	reward, err := service.Admin.GetReward(ctx, identity.WorkspaceID, hiddenID, firstStep.rewardID)
	if err != nil || reward.Key != "coin" {
		t.Fatalf("get reward: %+v, err=%v", reward, err)
	}
	if _, err := service.Admin.SetCalendarActive(ctx, identity.WorkspaceID, hiddenID, false); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if _, err := service.Admin.DeleteCalendar(ctx, identity.WorkspaceID, hiddenID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	deleted := record(t, service, identity, hiddenID, "deleted-op", now.Add(3*time.Hour))
	if deleted.Status != repository.StatusNotFound {
		t.Fatalf("deleted status: %+v", deleted)
	}
}

func TestCalendarAdminSurfaceAndCallbackControls(t *testing.T) {

	service := newCalendarTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("admin-surface")
	calendarID := createCalendar(t, service, admin.SaveCalendarParams{
		WorkspaceID:   workspaceID,
		Type:          "admin-surface",
		Mode:          repository.ModeSequential,
		IntervalType:  repository.IntervalFloating,
		IntervalUnit:  "hour",
		IntervalCount: 1,
		EndBehavior:   repository.EndStop,
		Timezone:      "UTC",
		IsActive:      true,
	})
	if changed, err := service.Admin.UpdateCalendar(ctx, admin.SaveCalendarParams{
		ID:                  calendarID,
		WorkspaceID:         workspaceID,
		Type:                "admin-surface-updated",
		Mode:                repository.ModeSequential,
		IntervalType:        repository.IntervalFloating,
		IntervalUnit:        "hour",
		IntervalCount:       1,
		ResetAfterIntervals: 1,
		EndBehavior:         repository.EndStop,
		Timezone:            "UTC",
		IsActive:            true,
	}); err != nil || changed != 1 {
		t.Fatalf("update calendar: changed=%d err=%v", changed, err)
	}

	for _, localization := range []admin.SaveLocalizationParams{
		{
			WorkspaceID: workspaceID,
			CalendarID:  calendarID,
			Locale:      "ru",
			Title:       "Календарь",
			Description: "Описание",
		},
		{
			WorkspaceID: workspaceID,
			CalendarID:  calendarID,
			Locale:      "en",
			Title:       "Calendar",
			Description: "Description",
		},
	} {
		if err := service.Admin.UpsertLocalization(ctx, localization); err != nil {
			t.Fatalf("upsert localization %s: %v", localization.Locale, err)
		}
	}

	localization, err := service.Admin.GetLocalization(ctx, workspaceID, calendarID, "ru")
	if err != nil || localization.Title != "Календарь" {
		t.Fatalf("get localization: value=%+v err=%v", localization, err)
	}
	localizations, err := service.Admin.ListLocalizations(ctx, workspaceID, calendarID)
	if err != nil || len(localizations) != 2 {
		t.Fatalf("list localizations: values=%+v err=%v", localizations, err)
	}

	mainStep := createStepReward(t, service, workspaceID, calendarID, 1, "coin", 10)
	deleteStepID, err := service.Admin.CreateStep(ctx, admin.SaveStepParams{
		WorkspaceID: workspaceID,
		CalendarID:  calendarID,
		Position:    2,
	})
	if err != nil {
		t.Fatalf("create removable step: %v", err)
	}
	day := "day"
	deleteRewardID, err := service.Admin.CreateReward(ctx, admin.SaveRewardParams{
		WorkspaceID: workspaceID,
		CalendarID:  calendarID,
		StepID:      deleteStepID,
		Key:         "premium",
		Type:        "duration",
		Quantity:    1,
		Unit:        &day,
		Position:    1,
	})
	if err != nil {
		t.Fatalf("create removable duration reward: %v", err)
	}

	calendarValue, err := service.Admin.GetCalendar(ctx, workspaceID, calendarID)
	if err != nil || calendarValue.Type != "admin-surface-updated" ||
		len(calendarValue.Localizations) != 2 || len(calendarValue.Steps) != 2 {
		t.Fatalf("get calendar: value=%+v err=%v", calendarValue, err)
	}

	now := time.Now().UTC()
	for index := 0; index < 4; index++ {
		identity := user.Identity{
			WorkspaceID:    workspaceID,
			AppID:          1,
			PlatformID:     1,
			PlatformUserID: fmt.Sprintf("admin-user-%d", index),
		}
		result := record(
			t,
			service,
			identity,
			calendarID,
			fmt.Sprintf("admin-operation-%d", index),
			now,
		)
		if !result.Granted {
			t.Fatalf("operation %d was not granted: %+v", index, result)
		}
	}

	operations, err := service.Admin.ListOperations(
		ctx,
		workspaceID,
		calendarID,
		admin.Page{Limit: 10},
	)
	if err != nil || len(operations) != 4 {
		t.Fatalf("list operations: values=%+v err=%v", operations, err)
	}

	pkg, err := service.Admin.Export(ctx, workspaceID, admin.ExportRequest{})
	if err != nil {
		t.Fatalf("export calendar: %v", err)
	}
	preview, err := service.Admin.PreviewImport(ctx, workspaceID, pkg)
	if err != nil || preview.Counts.Calendars != 1 || len(preview.Conflicts) != 1 {
		t.Fatalf("preview import: value=%+v err=%v", preview, err)
	}

	events, err := service.Admin.ListCallbackEvents(ctx, admin.CallbackEventListParams{
		WorkspaceID: workspaceID,
		Page:        admin.Page{Limit: 10},
	})
	if err != nil || len(events) != 4 {
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

	db, err := openCalendarPostgres(calendarTestDB)
	if err != nil {
		t.Fatalf("open callback database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `
		UPDATE calendar_clb_event
		SET status = 'processing', locked_until = now() - interval '1 minute'
		WHERE id = $1
	`, events[2].ID); err != nil {
		t.Fatalf("expire callback lease: %v", err)
	}
	if changed, err := service.Admin.ResetExpiredCallbackProcessing(ctx, workspaceID); err != nil || changed != 1 {
		t.Fatalf("reset callback processing: changed=%d err=%v", changed, err)
	}

	if changed, err := service.Admin.DeleteReward(ctx, workspaceID, calendarID, deleteRewardID); err != nil || changed != 1 {
		t.Fatalf("delete reward: changed=%d err=%v", changed, err)
	}
	if changed, err := service.Admin.DeleteStep(ctx, workspaceID, calendarID, deleteStepID); err != nil || changed != 1 {
		t.Fatalf("delete step: changed=%d err=%v", changed, err)
	}
	if changed, err := service.Admin.DeleteLocalization(ctx, workspaceID, calendarID, "en"); err != nil || changed != 1 {
		t.Fatalf("delete localization: changed=%d err=%v", changed, err)
	}
	if _, err := service.Admin.DeleteReward(ctx, "invalid", calendarID, mainStep.rewardID); !errors.Is(err, services.ErrIdentityWorkspaceInvalid) {
		t.Fatalf("invalid workspace delete reward error = %v", err)
	}

}

type stepReward struct {
	stepID   uint64
	rewardID uint64
}

func createCalendar(t testing.TB, service *Calendar, params admin.SaveCalendarParams) string {
	t.Helper()
	id, err := service.Admin.CreateCalendar(context.Background(), params)
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	return id
}

func createStepReward(t testing.TB, service *Calendar, workspaceID, calendarID string, position uint32, key string, quantity int64) stepReward {
	t.Helper()
	stepID, err := service.Admin.CreateStep(context.Background(), admin.SaveStepParams{
		WorkspaceID: workspaceID, CalendarID: calendarID, Position: position,
	})
	if err != nil {
		t.Fatalf("create step: %v", err)
	}
	rewardID, err := service.Admin.CreateReward(context.Background(), admin.SaveRewardParams{
		WorkspaceID: workspaceID, CalendarID: calendarID, StepID: stepID,
		Key: key, Quantity: quantity, Position: 1,
	})
	if err != nil {
		t.Fatalf("create reward: %v", err)
	}
	return stepReward{stepID: stepID, rewardID: rewardID}
}

func record(t testing.TB, service *Calendar, identity user.Identity, ref, operation string, now time.Time) user.RecordResult {
	t.Helper()
	value, err := service.User.Record(context.Background(), user.RecordParams{
		Identity: identity, CalendarRef: ref, OperationID: operation, Now: now,
	})
	if err != nil {
		t.Fatalf("record %s: %v", operation, err)
	}
	return value
}

func newCalendarTestService(t testing.TB) *Calendar {
	return newCalendarTestServiceWithOptions(t, calendarTestOptions())
}

func newCalendarTestServiceWithOptions(t testing.TB, options Options) *Calendar {
	t.Helper()
	ctx := context.Background()
	adminDB, err := openCalendarPostgres("")
	if err != nil {
		t.Fatalf("open admin postgres: %v", err)
	}
	terminateCalendarConnections(ctx, t, adminDB, calendarTestDB)
	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", calendarTestDB)); err != nil {
		t.Fatalf("drop database: %v", err)
	}
	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", calendarTestDB)); err != nil {
		t.Fatalf("create database: %v", err)
	}
	_ = adminDB.Close()
	db, err := openCalendarPostgres(calendarTestDB)
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
		t.Fatalf("bootstrap calendar: %v", err)
	}
	service, err := NewWithDatabase(ctx, db, options)
	if err != nil {
		t.Fatalf("create calendar service: %v", err)
	}
	t.Cleanup(func() {
		_ = service.Close()
		_ = repo.Close()
		_ = client.Close()
	})
	return service
}

func calendarTestOptions() Options {
	return Options{
		CacheEnabled:  true,
		CacheSize:     10000,
		CacheTTLCheck: time.Minute,
		CacheL1Delay:  time.Minute,
	}
}

func terminateCalendarConnections(ctx context.Context, t testing.TB, db *sql.DB, database string) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = $1 AND pid <> pg_backend_pid()`, database)
	if err != nil {
		t.Fatalf("terminate postgres connections: %v", err)
	}
}

func TestCalendarHideFutureRewardsLimitsMutationResponses(t *testing.T) {
	service := newCalendarTestService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	workspaceID := testsupport.WorkspaceID("hidden-mutation-rewards")
	calendarID := createCalendar(t, service, admin.SaveCalendarParams{
		WorkspaceID:       workspaceID,
		Type:              "hidden-mutation",
		Mode:              repository.ModeSequential,
		IntervalType:      repository.IntervalFloating,
		IntervalUnit:      "hour",
		EndBehavior:       repository.EndStop,
		Timezone:          "UTC",
		IsActive:          true,
		HideFutureRewards: true,
	})
	createStepReward(t, service, workspaceID, calendarID, 1, "current", 1)
	createStepReward(t, service, workspaceID, calendarID, 2, "next", 1)
	createStepReward(t, service, workspaceID, calendarID, 3, "future", 1)

	assertVisibleSteps := func(t *testing.T, value user.RecordResult) {
		t.Helper()
		if len(value.Calendar.Steps) != 2 ||
			value.Calendar.Steps[0].Position != 1 ||
			value.Calendar.Steps[1].Position != 2 {
			t.Fatalf("future rewards leaked: %+v", value.Calendar.Steps)
		}
	}

	t.Run("Next", func(t *testing.T) {
		identity := user.Identity{
			WorkspaceID:    workspaceID,
			AppID:          1,
			PlatformID:     1,
			PlatformUserID: "next-user",
		}
		result, err := service.User.Next(ctx, user.NextParams{
			Identity:    identity,
			CalendarRef: calendarID,
			Locale:      "ru",
			Now:         now,
		})
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		if !result.Granted {
			t.Fatalf("current step must be granted: %+v", result)
		}
		assertVisibleSteps(t, result)
	})

	t.Run("Record", func(t *testing.T) {
		identity := user.Identity{
			WorkspaceID:    workspaceID,
			AppID:          1,
			PlatformID:     1,
			PlatformUserID: "record-user",
		}
		result := record(t, service, identity, calendarID, "hidden-record", now)
		assertVisibleSteps(t, result)
	})
}

func openCalendarPostgres(database string) (*sql.DB, error) {
	if database == "" {
		database = "postgres"
	}
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		calendarTestPGHost,
		calendarTestPGPort,
		calendarTestPGUser,
		calendarTestPGPassword,
		database,
	)
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
