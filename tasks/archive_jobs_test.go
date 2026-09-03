package tasks

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/elum2b/services/internal/testsupport"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
	"github.com/elum2b/services/tasks/repository"
	"github.com/elum2b/services/tasks/service/admin"
)

func TestTasksArchiveJobsRoundTrip(t *testing.T) {
	options := tasksTestOptions(Options{})

	options.ArchiveDirectory = t.TempDir()

	service := newTasksTestService(t, options)
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

	sourceSecret := "archive-source-secret"
	sourceWebhookSecret := "archive-source-webhook-secret"

	if err := service.Admin.SavePartnerConfig(ctx, admin.PartnerConfigModel{
		WorkspaceID:   source,
		Provider:      "archive-provider",
		GroupKey:      "archive",
		Platform:      "telegram",
		Secret:        &sourceSecret,
		WebhookSecret: &sourceWebhookSecret,
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.Admin.UpsertGroup(
		ctx,
		target,
		"archive",
		1,
		true,
	); err != nil {
		t.Fatal(err)
	}

	previousSecret := "archive-previous-secret"
	previousWebhookSecret := "archive-previous-webhook-secret"

	if err := service.Admin.SavePartnerConfig(ctx, admin.PartnerConfigModel{
		WorkspaceID:   target,
		Provider:      "archive-provider",
		GroupKey:      "archive",
		Platform:      "telegram",
		Secret:        &previousSecret,
		WebhookSecret: &previousWebhookSecret,
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

	worker := startTasksArchiveWorker(t, options)

	waitTasksArchiveJob(t, worker, source, export.ID)

	dump, _, err := service.Admin.DownloadArchive(ctx, source, export.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer dump.Close()

	importSecret := "archive-import-secret"
	importWebhookSecret := "archive-import-webhook-secret"

	importJob, err := service.Admin.QueueArchiveImport(
		ctx,
		admin.QueueArchiveImportParams{
			WorkspaceID: target,
			FileName:    "tasks.zip",
			ImportRequest: admin.ImportRequest{
				ConflictStrategy: repository.ImportConflictUpdate,
				Secrets: map[string]string{
					"tasks.partner.archive-provider.archive.telegram.secret":         importSecret,
					"tasks.partner.archive-provider.archive.telegram.webhook_secret": importWebhookSecret,
				},
			},
			Archive: dump,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(importJob.Options), importSecret) ||
		strings.Contains(string(importJob.Options), importWebhookSecret) ||
		strings.Contains(string(importJob.Options), sourceSecret) {
		t.Fatalf("archive job options expose a secret: %s", importJob.Options)
	}

	waitTasksArchiveJob(t, worker, target, importJob.ID)

	status, err := service.Admin.ArchiveJob(ctx, target, importJob.ID)
	if err != nil || strings.Contains(string(status.Options), importSecret) {
		t.Fatalf(
			"archive job status exposed a secret: %#v, err=%v",
			status,
			err,
		)
	}

	history, err := service.Admin.ArchiveJobHistory(
		ctx,
		target,
		importJob.ID,
		admin.Page{},
	)
	if err != nil || strings.Contains(fmt.Sprint(history), importSecret) {
		t.Fatalf(
			"archive job history exposed a secret: %#v, err=%v",
			history,
			err,
		)
	}

	repo := repository.NewWithOptions(service.client, repository.Options{
		SecretEncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
	})
	pkg, err := repo.Export(ctx, target, admin.ExportRequest{})

	if err != nil || len(pkg.Groups) != 1 || len(pkg.Groups[0].Tasks) != 1 {
		t.Fatalf("imported package = %+v, err = %v", pkg, err)
	}

	var stored, storedWebhookSecret string

	if err := service.client.DB().QueryRowContext(ctx, `
		SELECT secret, webhook_secret FROM task_partner_config
WHERE workspace_id = $1 AND provider = $2 AND group_key = $3 AND platform = $4`,
		target, "archive-provider", "archive", "telegram").
		Scan(&stored, &storedWebhookSecret); err != nil {
		t.Fatal(err)
	}

	if stored == importSecret || !strings.HasPrefix(stored, "v1:") {
		t.Fatalf(
			"archive import did not store the supplied secret securely: %q",
			stored,
		)
	}

	if storedWebhookSecret == importWebhookSecret ||
		!strings.HasPrefix(storedWebhookSecret, "v1:") {
		t.Fatalf(
			"archive import did not store the supplied webhook secret securely: %q",
			storedWebhookSecret,
		)
	}

	config, found, err := repo.GetPartnerConfig(
		ctx,
		target,
		"archive-provider",
		"archive",
		"telegram",
	)
	if err != nil || !found || config.Secret == nil ||
		*config.Secret != importSecret || config.WebhookSecret == nil ||
		*config.WebhookSecret != importWebhookSecret {
		t.Fatalf(
			"archive import did not overwrite the secret: %#v, found=%v, err=%v",
			config,
			found,
			err,
		)
	}

	webhookConfig, found, err := repo.GetPartnerConfigByWebhookSecret(
		ctx,
		target,
		importWebhookSecret,
	)
	if err != nil || !found || webhookConfig.Provider != "archive-provider" {
		t.Fatalf(
			"encrypted webhook lookup = %#v, found=%v, err=%v",
			webhookConfig,
			found,
			err,
		)
	}

	adminConfig, found, err := service.Admin.GetPartnerConfig(
		ctx,
		target,
		"archive-provider",
		"archive",
		"telegram",
	)
	if err != nil || !found || adminConfig.Secret != nil ||
		adminConfig.WebhookSecret != nil {
		t.Fatalf(
			"admin partner config exposes write-only secrets: %#v, found=%v, err=%v",
			adminConfig,
			found,
			err,
		)
	}

	if _, err := service.client.DB().ExecContext(ctx, `
UPDATE task_partner_config SET webhook_secret = $1
WHERE workspace_id = $2 AND provider = $3 AND group_key = $4 AND platform = $5`,
		"legacy-webhook-secret",
		target,
		"archive-provider",
		"archive",
		"telegram",
	); err != nil {
		t.Fatal(err)
	}

	if err := repo.Bootstrap(ctx); err != nil {
		t.Fatalf("migrate legacy webhook secret: %v", err)
	}

	if err := service.client.DB().QueryRowContext(ctx, `
SELECT webhook_secret FROM task_partner_config
WHERE workspace_id = $1 AND provider = $2 AND group_key = $3 AND platform = $4`,
		target,
		"archive-provider",
		"archive",
		"telegram",
	).Scan(&storedWebhookSecret); err != nil {
		t.Fatal(err)
	}

	if storedWebhookSecret == "legacy-webhook-secret" ||
		!strings.HasPrefix(storedWebhookSecret, "v1:") {
		t.Fatalf(
			"legacy webhook secret was not migrated: %q",
			storedWebhookSecret,
		)
	}

	if _, _, err := service.Admin.DownloadArchive(
		ctx,
		target,
		importJob.ID,
	); err == nil {
		t.Fatal("task import archive must not be downloadable")
	}
}

func startTasksArchiveWorker(t *testing.T, options Options) *Tasks {
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
			Options:  options,
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
	for !service.IsReady() {
		if time.Now().After(deadline) {
			t.Fatal("tasks archive worker did not become ready")
		}

		time.Sleep(25 * time.Millisecond)
	}

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
