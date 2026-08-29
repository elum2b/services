package promo

import (
	"context"
	"testing"
	"time"

	"github.com/elum2b/services/internal/testsupport"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
	"github.com/elum2b/services/promo/repository"
	"github.com/elum2b/services/promo/service/admin"
)

func TestPromoArchiveJobsRoundTrip(t *testing.T) {
	service := newPromoTestService(t)
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

	waitPromoArchiveJob(t, service, source, export.ID)

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

	waitPromoArchiveJob(t, service, target, importJob.ID)

	pkg, err := repository.New(service.client).
		Export(ctx, target, admin.ExportRequest{})
	if err != nil || len(pkg.Promos) != 1 {
		t.Fatalf("imported package = %+v, err = %v", pkg, err)
	}
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
