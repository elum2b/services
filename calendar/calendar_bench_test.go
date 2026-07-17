package calendar

import (
	"context"
	"errors"
	"github.com/elum2b/services/calendar/repository"
	"github.com/elum2b/services/calendar/service/admin"
	"github.com/elum2b/services/calendar/service/user"
	"github.com/elum2b/services/internal/testsupport"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

var calendarAdminBenchmarkID atomic.Uint64

func BenchmarkCalendarAdminCalendarMethods(b *testing.B) {
	service := newCalendarTestService(b)
	ctx := context.Background()
	base := benchmarkCalendarParams("bench-admin", "base")
	baseID := createCalendar(b, service, base)

	b.ReportAllocs()
	b.Run("CreateCalendar", func(b *testing.B) {
		for range b.N {
			id := calendarAdminBenchmarkID.Add(1)
			params := benchmarkCalendarParams("bench-admin", "create-"+strconv.FormatUint(id, 10))
			_, err := service.Admin.CreateCalendar(ctx, params)
			benchmarkError(b, err)
		}
	})
	b.Run("UpdateCalendar", func(b *testing.B) {
		params := base
		params.ID = baseID
		for i := 0; i < b.N; i++ {
			params.HideFutureRewards = i%2 == 0
			_, err := service.Admin.UpdateCalendar(ctx, params)
			benchmarkError(b, err)
		}
	})
	b.Run("GetCalendar", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.GetCalendar(ctx, testsupport.WorkspaceID("bench-admin"), baseID)
			benchmarkError(b, err)
		}
	})
	b.Run("ListCalendars", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.ListCalendars(ctx, testsupport.WorkspaceID("bench-admin"), admin.Page{Limit: 100})
			benchmarkError(b, err)
		}
	})
	b.Run("SetCalendarActive", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := service.Admin.SetCalendarActive(ctx, testsupport.WorkspaceID("bench-admin"), baseID, i%2 == 0)
			benchmarkError(b, err)
		}
	})
	b.Run("DeleteCalendar", func(b *testing.B) {
		for range b.N {
			b.StopTimer()
			id := calendarAdminBenchmarkID.Add(1)
			calendarID := createCalendar(b, service,
				benchmarkCalendarParams("bench-delete", "delete-"+strconv.FormatUint(id, 10)))
			b.StartTimer()
			_, err := service.Admin.DeleteCalendar(ctx, testsupport.WorkspaceID("bench-delete"), calendarID)
			benchmarkError(b, err)
		}
	})
}

func BenchmarkCalendarAdminLocalizationMethods(b *testing.B) {
	service := newCalendarTestService(b)
	ctx := context.Background()
	calendarID := createCalendar(b, service, benchmarkCalendarParams("bench-loc", "localization"))
	params := admin.SaveLocalizationParams{
		WorkspaceID: testsupport.WorkspaceID("bench-loc"), CalendarID: calendarID,
		Locale: "ru", Title: "Title", Description: "Description",
	}
	if err := service.Admin.UpsertLocalization(ctx, params); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.Run("UpsertLocalization", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			params.Title = "Title " + strconv.Itoa(i%2)
			benchmarkError(b, service.Admin.UpsertLocalization(ctx, params))
		}
	})
	b.Run("GetLocalization", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.GetLocalization(ctx, testsupport.WorkspaceID("bench-loc"), calendarID, "ru")
			benchmarkError(b, err)
		}
	})
	b.Run("ListLocalizations", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.ListLocalizations(ctx, testsupport.WorkspaceID("bench-loc"), calendarID)
			benchmarkError(b, err)
		}
	})
	b.Run("DeleteLocalization", func(b *testing.B) {
		for range b.N {
			b.StopTimer()
			benchmarkError(b, service.Admin.UpsertLocalization(ctx, params))
			b.StartTimer()
			_, err := service.Admin.DeleteLocalization(ctx, testsupport.WorkspaceID("bench-loc"), calendarID, "ru")
			benchmarkError(b, err)
		}
	})
}

func benchmarkCalendarParams(workspaceID, calendarType string) admin.SaveCalendarParams {
	return admin.SaveCalendarParams{
		WorkspaceID: testsupport.WorkspaceID(workspaceID), Type: calendarType, Mode: ModeSequential,
		IntervalType: IntervalFloating, IntervalUnit: "second",
		IntervalCount: 1, EndBehavior: EndRepeatLast,
		Timezone: "UTC", IsActive: true,
	}
}

var calendarBenchmarkUserID atomic.Uint64

func BenchmarkCalendarServiceMethods(b *testing.B) {
	service := newCalendarTestService(b)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	calendarID := createCalendar(b, service, admin.SaveCalendarParams{
		WorkspaceID: testsupport.WorkspaceID("bench"), Type: "bench", Mode: repository.ModeSequential,
		IntervalType: repository.IntervalFloating, IntervalUnit: "second",
		IntervalCount: 1, EndBehavior: repository.EndRepeatLast,
		Timezone: "UTC", IsActive: true,
	})
	createStepReward(b, service, testsupport.WorkspaceID("bench"), calendarID, 1, "coin", 1)
	idempotent := user.Identity{
		WorkspaceID: testsupport.WorkspaceID("bench"), AppID: 1, PlatformID: 1, PlatformUserID: "same",
	}
	if _, err := service.User.Record(ctx, user.RecordParams{
		Identity: idempotent, CalendarRef: calendarID, OperationID: "same-op", Now: start,
	}); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.Run("User.Record/idempotent", func(b *testing.B) {
		for range b.N {
			_, err := service.User.Record(ctx, user.RecordParams{
				Identity: idempotent, CalendarRef: calendarID,
				OperationID: "same-op", Now: start.Add(time.Hour),
			})
			benchmarkError(b, err)
		}
	})
	b.Run("User.Record/success", func(b *testing.B) {
		for range b.N {
			id := calendarBenchmarkUserID.Add(1)
			_, err := service.User.Record(ctx, user.RecordParams{
				Identity: user.Identity{
					WorkspaceID: testsupport.WorkspaceID("bench"), AppID: 1, PlatformID: 1,
					PlatformUserID: "user-" + strconv.FormatUint(id, 10),
				},
				CalendarRef: calendarID, OperationID: "op-" + strconv.FormatUint(id, 10), Now: start,
			})
			benchmarkError(b, err)
		}
	})
	b.Run("User.Next", func(b *testing.B) {
		for range b.N {
			_, err := service.User.Next(ctx, user.NextParams{
				Identity: idempotent, CalendarRef: calendarID, Now: start.Add(time.Hour),
			})
			benchmarkError(b, err)
		}
	})
	b.Run("User.ListActive", func(b *testing.B) {
		for range b.N {
			_, err := service.User.ListActive(ctx, user.ListActiveParams{WorkspaceID: testsupport.WorkspaceID("bench"), Locale: "ru", Now: start})
			benchmarkError(b, err)
		}
	})
	b.Run("User.GetCalendar", func(b *testing.B) {
		for range b.N {
			_, err := service.User.GetCalendar(ctx, user.GetCalendarParams{Identity: idempotent, Ref: calendarID, Locale: "ru"})
			benchmarkError(b, err)
		}
	})
	b.Run("User.GetProgress", func(b *testing.B) {
		for range b.N {
			_, err := service.User.GetProgress(ctx, user.GetProgressParams{Identity: idempotent, CalendarID: calendarID})
			benchmarkError(b, err)
		}
	})
}

func BenchmarkCalendarImportExport(b *testing.B) {
	service := newCalendarTestService(b)
	ctx := context.Background()
	calendarID := createCalendar(b, service, admin.SaveCalendarParams{
		WorkspaceID: testsupport.WorkspaceID("bench-import"), Type: "bench_import", Mode: repository.ModeSequential,
		IntervalType: repository.IntervalFloating, IntervalUnit: "day",
		IntervalCount: 1, EndBehavior: repository.EndStop, Timezone: "UTC", IsActive: true,
	})
	createStepReward(b, service, testsupport.WorkspaceID("bench-import"), calendarID, 1, "coin", 1)
	if err := service.Admin.UpsertLocalization(ctx, admin.SaveLocalizationParams{
		WorkspaceID: testsupport.WorkspaceID("bench-import"), CalendarID: calendarID, Locale: "ru", Title: "Benchmark",
	}); err != nil {
		b.Fatal(err)
	}
	pkg, err := service.Admin.Export(ctx, testsupport.WorkspaceID("bench-import"), admin.ExportRequest{})
	benchmarkError(b, err)
	b.ReportAllocs()
	b.Run("Export", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.Export(ctx, testsupport.WorkspaceID("bench-import"), admin.ExportRequest{})
			benchmarkError(b, err)
		}
	})
	b.Run("Import/update", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.Import(ctx, testsupport.WorkspaceID("bench-import"), admin.ImportRequest{
				Package: pkg, ConflictStrategy: repository.ImportConflictUpdate,
			})
			benchmarkError(b, err)
		}
	})
}

func benchmarkError(b *testing.B, err error) {
	b.Helper()
	if err != nil {
		b.Fatal(err)
	}
}

func BenchmarkCalendarAdminCallbackMethods(b *testing.B) {
	service := newCalendarTestService(b)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	calendarID := createCalendar(
		b, service, benchmarkCalendarParams("bench-callback", "callback"),
	)
	createStepReward(b, service, testsupport.WorkspaceID("bench-callback"), calendarID, 1, "coin", 1)
	baseEventID := createBenchmarkCallbackEvent(b, service, "bench-callback", calendarID, start)

	b.ReportAllocs()
	b.Run("ListCallbackEvents", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.ListCallbackEvents(ctx, admin.CallbackEventListParams{
				WorkspaceID: testsupport.WorkspaceID("bench-callback"),
				Page:        admin.Page{Limit: 100},
			})
			benchmarkError(b, err)
		}
	})
	b.Run("GetCallbackEvent", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.GetCallbackEvent(ctx, testsupport.WorkspaceID("bench-callback"), baseEventID)
			benchmarkError(b, err)
		}
	})
	b.Run("RetryCallbackEventNow", func(b *testing.B) {
		for range b.N {
			b.StopTimer()
			eventID := createBenchmarkCallbackEvent(b, service, "bench-callback", calendarID, start)
			b.StartTimer()
			_, err := service.Admin.RetryCallbackEventNow(ctx, testsupport.WorkspaceID("bench-callback"), eventID)
			benchmarkError(b, err)
		}
	})
	b.Run("MarkCallbackEventOK", func(b *testing.B) {
		for range b.N {
			b.StopTimer()
			eventID := createBenchmarkCallbackEvent(b, service, "bench-callback", calendarID, start)
			b.StartTimer()
			_, err := service.Admin.MarkCallbackEventOK(ctx, testsupport.WorkspaceID("bench-callback"), eventID)
			benchmarkError(b, err)
		}
	})
	b.Run("MarkCallbackEventReject", func(b *testing.B) {
		for range b.N {
			b.StopTimer()
			eventID := createBenchmarkCallbackEvent(b, service, "bench-callback", calendarID, start)
			b.StartTimer()
			_, err := service.Admin.MarkCallbackEventReject(ctx, testsupport.WorkspaceID("bench-callback"), eventID, "benchmark")
			benchmarkError(b, err)
		}
	})
	b.Run("ResetExpiredCallbackProcessing", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.ResetExpiredCallbackProcessing(ctx, testsupport.WorkspaceID("bench-callback"))
			benchmarkError(b, err)
		}
	})
}

func BenchmarkCalendarOnCallback(b *testing.B) {
	service := newCalendarTestService(b)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	calendarID := createCalendar(
		b, service, benchmarkCalendarParams("bench-worker", "worker"),
	)
	createStepReward(b, service, testsupport.WorkspaceID("bench-worker"), calendarID, 1, "coin", 1)

	b.StopTimer()
	for range b.N {
		createBenchmarkCallbackEvent(b, service, "bench-worker", calendarID, start)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	var processed atomic.Int64
	b.ResetTimer()
	b.ReportAllocs()
	b.StartTimer()
	err := service.OnCallback(runCtx, func(callbackCtx Context) error {
		if err := callbackCtx.Successful(); err != nil {
			return err
		}
		if processed.Add(1) == int64(b.N) {
			cancel()
		}
		return nil
	}, WithCallbackBatchSize(100), WithCallbackIdleDelay(time.Millisecond))
	if !errors.Is(err, context.Canceled) {
		b.Fatalf("OnCallback: %v", err)
	}
}

func createBenchmarkCallbackEvent(
	b *testing.B,
	service *Calendar,
	workspaceID string,
	calendarID string,
	now time.Time,
) uint64 {
	b.Helper()
	workspaceID = testsupport.WorkspaceID(workspaceID)
	id := calendarBenchmarkUserID.Add(1)
	_, err := service.User.Record(context.Background(), user.RecordParams{
		Identity: user.Identity{
			WorkspaceID: workspaceID, AppID: 1, PlatformID: 1,
			PlatformUserID: "callback-" + strconv.FormatUint(id, 10),
		},
		CalendarRef: calendarID, OperationID: "callback-op-" + strconv.FormatUint(id, 10),
		Now: now,
	})
	if err != nil {
		b.Fatal(err)
	}
	events, err := service.Admin.ListCallbackEvents(
		context.Background(), admin.CallbackEventListParams{
			WorkspaceID: workspaceID,
			Page:        admin.Page{Limit: 1},
		},
	)
	if err != nil {
		b.Fatal(err)
	}
	if len(events) == 0 {
		b.Fatal("callback event was not created")
	}
	return events[0].ID
}

func BenchmarkCalendarAdminStepMethods(b *testing.B) {
	service := newCalendarTestService(b)
	ctx := context.Background()
	calendarID := createCalendar(b, service, benchmarkCalendarParams("bench-step", "steps"))
	baseStep, err := service.Admin.CreateStep(ctx, admin.SaveStepParams{
		WorkspaceID: testsupport.WorkspaceID("bench-step"), CalendarID: calendarID, Position: 1,
	})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.Run("CreateStep", func(b *testing.B) {
		for range b.N {
			id := calendarAdminBenchmarkID.Add(1)
			_, err := service.Admin.CreateStep(ctx, admin.SaveStepParams{
				WorkspaceID: testsupport.WorkspaceID("bench-step"), CalendarID: calendarID,
				Position: uint32(id + 10),
			})
			benchmarkError(b, err)
		}
	})
	b.Run("UpdateStep", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := service.Admin.UpdateStep(ctx, admin.SaveStepParams{
				WorkspaceID: testsupport.WorkspaceID("bench-step"), CalendarID: calendarID,
				ID: baseStep, Position: uint32(2 + i%2),
			})
			benchmarkError(b, err)
		}
	})
	b.Run("DeleteStep", func(b *testing.B) {
		for range b.N {
			b.StopTimer()
			id := calendarAdminBenchmarkID.Add(1)
			stepID, err := service.Admin.CreateStep(ctx, admin.SaveStepParams{
				WorkspaceID: testsupport.WorkspaceID("bench-step"), CalendarID: calendarID,
				Position: uint32(id + 100000),
			})
			benchmarkError(b, err)
			b.StartTimer()
			_, err = service.Admin.DeleteStep(ctx, testsupport.WorkspaceID("bench-step"), calendarID, stepID)
			benchmarkError(b, err)
		}
	})
}

func BenchmarkCalendarAdminRewardMethods(b *testing.B) {
	service := newCalendarTestService(b)
	ctx := context.Background()
	calendarID := createCalendar(b, service, benchmarkCalendarParams("bench-reward", "rewards"))
	stepID, err := service.Admin.CreateStep(ctx, admin.SaveStepParams{
		WorkspaceID: testsupport.WorkspaceID("bench-reward"), CalendarID: calendarID, Position: 1,
	})
	if err != nil {
		b.Fatal(err)
	}
	baseReward, err := service.Admin.CreateReward(ctx, admin.SaveRewardParams{
		WorkspaceID: testsupport.WorkspaceID("bench-reward"), CalendarID: calendarID, StepID: stepID,
		Key: "base", Quantity: 1, Position: 1,
	})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.Run("CreateReward", func(b *testing.B) {
		for range b.N {
			id := calendarAdminBenchmarkID.Add(1)
			_, err := service.Admin.CreateReward(ctx, admin.SaveRewardParams{
				WorkspaceID: testsupport.WorkspaceID("bench-reward"), CalendarID: calendarID, StepID: stepID,
				Key: "reward-" + strconv.FormatUint(id, 10), Quantity: 1, Position: uint32(id + 10),
			})
			benchmarkError(b, err)
		}
	})
	b.Run("UpdateReward", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := service.Admin.UpdateReward(ctx, admin.SaveRewardParams{
				ID: baseReward, WorkspaceID: testsupport.WorkspaceID("bench-reward"), CalendarID: calendarID,
				StepID: stepID, Key: "base", Quantity: int64(i%2 + 1), Position: 1,
			})
			benchmarkError(b, err)
		}
	})
	b.Run("GetReward", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.GetReward(ctx, testsupport.WorkspaceID("bench-reward"), calendarID, baseReward)
			benchmarkError(b, err)
		}
	})
	b.Run("DeleteReward", func(b *testing.B) {
		for range b.N {
			b.StopTimer()
			id := calendarAdminBenchmarkID.Add(1)
			rewardID, err := service.Admin.CreateReward(ctx, admin.SaveRewardParams{
				WorkspaceID: testsupport.WorkspaceID("bench-reward"), CalendarID: calendarID, StepID: stepID,
				Key: "delete-" + strconv.FormatUint(id, 10), Quantity: 1, Position: uint32(id + 100000),
			})
			benchmarkError(b, err)
			b.StartTimer()
			_, err = service.Admin.DeleteReward(ctx, testsupport.WorkspaceID("bench-reward"), calendarID, rewardID)
			benchmarkError(b, err)
		}
	})
}

func BenchmarkCalendarLifecycle(b *testing.B) {
	ctx := context.Background()
	adminDB, err := openCalendarPostgres("")
	if err != nil {
		b.Fatal(err)
	}
	terminateCalendarConnections(ctx, b, adminDB, "calendar_bench_lifecycle")
	if _, err := adminDB.ExecContext(ctx, "DROP DATABASE IF EXISTS calendar_bench_lifecycle"); err != nil {
		b.Fatal(err)
	}
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE calendar_bench_lifecycle"); err != nil {
		b.Fatal(err)
	}
	_ = adminDB.Close()

	db, err := openCalendarPostgres("calendar_bench_lifecycle")
	if err != nil {
		b.Fatal(err)
	}
	client, err := sqlwrap.New(db)
	if err != nil {
		b.Fatal(err)
	}
	repo := repository.New(client)
	if err := repo.Bootstrap(ctx); err != nil {
		b.Fatal(err)
	}
	_ = repo.Close()
	b.Cleanup(func() { _ = client.Close() })

	b.ReportAllocs()
	b.Run("NewClose", func(b *testing.B) {
		for range b.N {
			service := New(DatabaseParams{})
			benchmarkError(b, service.Close())
		}
	})
	b.Run("RunClose", func(b *testing.B) {
		params := DatabaseParams{
			User:     calendarTestPGUser,
			Password: calendarTestPGPassword,
			Database: "calendar_bench_lifecycle",
			Host:     calendarTestPGHost,
			Port:     calendarTestPGPort,
		}
		for range b.N {
			runCtx, cancel := context.WithCancel(ctx)
			cancel()
			benchmarkError(b, New(params).Run(runCtx))
		}
	})
}

func BenchmarkCalendarAdminStatsMethods(b *testing.B) {
	service := newCalendarTestService(b)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	calendarID := createCalendar(b, service, benchmarkCalendarParams("bench-stats", "stats"))
	createStepReward(b, service, testsupport.WorkspaceID("bench-stats"), calendarID, 1, "coin", 1)
	identity := user.Identity{
		WorkspaceID: testsupport.WorkspaceID("bench-stats"), AppID: 1, PlatformID: 1, PlatformUserID: "user",
	}
	record(b, service, identity, calendarID, "stats-op", start)
	if err := service.Admin.RefreshDailyStats(ctx, testsupport.WorkspaceID("bench-stats"), start.Add(-time.Hour), start.Add(time.Hour)); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.Run("ListOperations", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.ListOperations(
				ctx, testsupport.WorkspaceID("bench-stats"), calendarID, admin.Page{Limit: 100},
			)
			benchmarkError(b, err)
		}
	})
	b.Run("GetStats", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.GetStats(ctx, testsupport.WorkspaceID("bench-stats"), calendarID)
			benchmarkError(b, err)
		}
	})
	b.Run("ListDailyStats", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.ListDailyStats(
				ctx, testsupport.WorkspaceID("bench-stats"), calendarID, start.Add(-24*time.Hour), start.Add(24*time.Hour),
			)
			benchmarkError(b, err)
		}
	})
	b.Run("RefreshDailyStats", func(b *testing.B) {
		for range b.N {
			benchmarkError(
				b,
				service.Admin.RefreshDailyStats(ctx, testsupport.WorkspaceID("bench-stats"), start.Add(-time.Hour), start.Add(time.Hour)),
			)
		}
	})
}
