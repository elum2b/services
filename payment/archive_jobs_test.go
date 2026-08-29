package payment

import (
	"context"
	"testing"
	"time"

	"github.com/elum2b/services/internal/testsupport"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
	"github.com/elum2b/services/payment/repository"
	"github.com/elum2b/services/payment/service/admin"
)

func TestPaymentArchiveJobsRoundTrip(t *testing.T) {
	env := setupPaymentIntegrationTest(t)
	source := testsupport.WorkspaceID("payment-archive-source")
	target := testsupport.WorkspaceID("payment-archive-target")
	createPaymentProduct(t, env, testProductOptions{WorkspaceID: source})

	export, err := env.api.Admin.QueueArchiveExport(
		context.Background(),
		admin.QueueArchiveExportParams{
			WorkspaceID: source,
			FileName:    "payment.zip",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	waitPaymentArchiveJob(t, env.api, source, export.ID)

	dump, _, err := env.api.Admin.DownloadArchive(
		context.Background(),
		source,
		export.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer dump.Close()

	importJob, err := env.api.Admin.QueueArchiveImport(
		context.Background(),
		admin.QueueArchiveImportParams{
			WorkspaceID: target,
			FileName:    "payment.zip",
			ImportRequest: admin.ImportRequest{
				ConflictStrategy: repository.ImportConflictUpdate,
			},
			Archive: dump,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	waitPaymentArchiveJob(t, env.api, target, importJob.ID)

	pkg, err := repository.NewPaymentRepository(env.client).
		Export(context.Background(), target, admin.ExportRequest{})
	if err != nil || len(pkg.Groups) != 1 || len(pkg.Groups[0].Products) != 1 {
		t.Fatalf("imported package = %+v, err = %v", pkg, err)
	}
}

func waitPaymentArchiveJob(
	t *testing.T,
	service *Payment,
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
