package calendar

import (
	"context"
	"testing"
	"time"

	"github.com/elum2b/services/calendar/repository"
	"github.com/elum2b/services/calendar/service/admin"
	"github.com/elum2b/services/internal/testsupport"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
)

func TestCalendarArchiveJobsRoundTrip(t *testing.T) {
	options := calendarTestOptions()

	options.ArchiveDirectory = t.TempDir()

	service := newCalendarTestServiceWithOptions(t, options)
	ctx := context.Background()
	source := testsupport.WorkspaceID("archive-source")
	target := testsupport.WorkspaceID("archive-target")
	createCalendar(
		t,
		service,
		admin.SaveCalendarParams{
			WorkspaceID:   source,
			Type:          "archive",
			Mode:          repository.ModeSequential,
			IntervalType:  repository.IntervalFloating,
			IntervalUnit:  "day",
			IntervalCount: 1,
			EndBehavior:   repository.EndStop,
			Timezone:      "UTC",
			IsActive:      true,
		},
	)

	export, err := service.Admin.QueueArchiveExport(
		ctx,
		admin.QueueArchiveExportParams{
			WorkspaceID: source,
			FileName:    "calendar.zip",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if export.Status != jobs.StatusQueued {
		t.Fatalf("export status = %s, want queued", export.Status)
	}

	worker := startCalendarArchiveWorker(t, options)

	waitCalendarArchiveJob(t, worker, source, export.ID)

	dump, _, err := service.Admin.DownloadArchive(ctx, source, export.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer dump.Close()

	importJob, err := service.Admin.QueueArchiveImport(
		ctx,
		admin.QueueArchiveImportParams{
			WorkspaceID: target,
			FileName:    "calendar.zip",
			ImportRequest: admin.ImportRequest{
				ConflictStrategy: "update_existing",
			},
			Archive: dump,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	waitCalendarArchiveJob(t, worker, target, importJob.ID)

	pkg, err := repository.New(service.client).
		Export(ctx, target, admin.ExportRequest{})
	if err != nil || len(pkg.Calendars) != 1 {
		t.Fatalf("imported package = %+v, err = %v", pkg, err)
	}
}

func startCalendarArchiveWorker(t *testing.T, options Options) *Calendar {
	t.Helper()

	service := New()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- service.Run(ctx, DatabaseParams{
			User:     calendarTestPGUser,
			Password: calendarTestPGPassword,
			Database: calendarTestDB,
			Host:     calendarTestPGHost,
			Port:     calendarTestPGPort,
			Options:  options,
		})
	}()

	deadline := time.Now().Add(5 * time.Second)

	for !service.IsReady() {
		select {
		case err := <-done:
			cancel()
			t.Fatalf("start calendar archive worker: %v", err)
		default:
		}

		if time.Now().After(deadline) {
			cancel()
			t.Fatal("calendar archive worker did not become ready")
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Cleanup(func() {
		cancel()

		if err := <-done; err != nil {
			t.Errorf("stop calendar archive worker: %v", err)
		}
	})

	return service
}

func waitCalendarArchiveJob(
	t *testing.T,
	service *Calendar,
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
