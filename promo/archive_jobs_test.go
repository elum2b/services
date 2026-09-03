package promo

import (
	"context"
	"testing"
	"time"

	"github.com/elum2b/services/internal/testsupport"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
	"github.com/elum2b/services/promo/repository"
	"github.com/elum2b/services/promo/service/admin"
	"github.com/elum2b/services/promo/service/user"
)

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
