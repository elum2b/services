package tasks

import (
	"context"
	"testing"
	"time"

	"github.com/elum2b/services/internal/testsupport"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
	"github.com/elum2b/services/tasks/repository"
	"github.com/elum2b/services/tasks/service/admin"
)

func TestTasksArchiveJobsRoundTrip(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	source := testsupport.WorkspaceID("tasks-archive-source")
	target := testsupport.WorkspaceID("tasks-archive-target")

	if err := service.Admin.UpsertGroup(
		ctx,
		source,
		"archive",
		1,
		true,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Admin.SaveTask(ctx, admin.SaveTaskParams{
		WorkspaceID: source,
		Key:         "archive_task",
		GroupKey:    "archive",
		TaskKind:    repository.TaskKindInternal,
		ActionKey:   "archive.task",
		ActionKind:  repository.ActionKindAppAction,
		ClaimMode:   repository.ClaimModeManual,
		TargetCount: 1,
		ResetUnit:   repository.ResetNever,
		ResetEvery:  1,
		Position:    1,
		IsVisible:   true,
		IsActive:    true,
	}); err != nil {
		t.Fatal(err)
	}

	export, err := service.Admin.QueueArchiveExport(
		ctx,
		admin.QueueArchiveExportParams{
			WorkspaceID: source,
			FileName:    "tasks.zip",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if export.Status != jobs.StatusQueued {
		t.Fatalf("export status = %s, want queued", export.Status)
	}

	worker := startTasksArchiveWorker(t)

	waitTasksArchiveJob(t, worker, source, export.ID)

	dump, _, err := service.Admin.DownloadArchive(ctx, source, export.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer dump.Close()

	importJob, err := service.Admin.QueueArchiveImport(
		ctx,
		admin.QueueArchiveImportParams{
			WorkspaceID: target,
			FileName:    "tasks.zip",
			ImportRequest: admin.ImportRequest{
				ConflictStrategy: repository.ImportConflictUpdate,
			},
			Archive: dump,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	waitTasksArchiveJob(t, worker, target, importJob.ID)

	pkg, err := repository.New(service.client).
		Export(ctx, target, admin.ExportRequest{})
	if err != nil || len(pkg.Groups) != 1 || len(pkg.Groups[0].Tasks) != 1 {
		t.Fatalf("imported package = %+v, err = %v", pkg, err)
	}
}

func startTasksArchiveWorker(t *testing.T) *Tasks {
	t.Helper()

	service := New()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- service.Run(ctx, DatabaseParams{
			User:     pgUser,
			Password: pgPassword,
			Database: tasksTestDB,
			Host:     pgHost,
			Port:     pgPort,
			Options:  tasksTestOptions(Options{}),
		})
	}()

	t.Cleanup(func() {
		cancel()

		if err := <-done; err != nil {
			t.Errorf("stop tasks archive worker: %v", err)
		}
	})

	return service
}

func waitTasksArchiveJob(
	t *testing.T,
	service *Tasks,
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
