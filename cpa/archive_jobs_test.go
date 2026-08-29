package cpa_test

import (
	"context"
	"testing"
	"time"

	"github.com/elum2b/services/cpa"
	"github.com/elum2b/services/cpa/service/admin"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
)

func TestCPAArchiveJobsRoundTrip(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertSharedOffer(t, env, "archive-offer", true)

	export, err := env.Service.Admin.QueueArchiveExport(
		env.Context,
		admin.QueueArchiveExportParams{
			WorkspaceID: cpaTestWorkspaceID,
			FileName:    "cpa.zip",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	waitCPAArchiveJob(t, env.Service, cpaTestWorkspaceID, export.ID)

	dump, _, err := env.Service.Admin.DownloadArchive(
		env.Context,
		cpaTestWorkspaceID,
		export.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer dump.Close()

	importJob, err := env.Service.Admin.QueueArchiveImport(
		env.Context,
		admin.QueueArchiveImportParams{
			WorkspaceID: cpaImportWorkspaceID,
			FileName:    "cpa.zip",
			ImportRequest: admin.ImportRequest{
				ConflictStrategy: "update_existing",
			},
			Archive: dump,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	waitCPAArchiveJob(t, env.Service, cpaImportWorkspaceID, importJob.ID)

	pkg, err := env.Repository.Export(
		context.Background(),
		cpaImportWorkspaceID,
		admin.ExportRequest{},
	)
	if err != nil || len(pkg.Offers) != 1 {
		t.Fatalf("imported package = %+v, err = %v", pkg, err)
	}
}

func waitCPAArchiveJob(
	t *testing.T,
	service *cpa.CPA,
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
