package jobs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elum2b/services/internal/utils/goroutine"
)

func TestNormalizeTableName(t *testing.T) {
	if got := normalizeTableName("Jobs_Events"); got != "jobs_events" {
		t.Fatalf("table = %q", got)
	}

	if got := normalizeTableName("jobs; DROP TABLE"); got != DefaultTable {
		t.Fatalf("unsafe table = %q", got)
	}

	if got := normalizeTableName(
		"abcdefghijklmnopqrstuvwxyzabcdefghijklmno",
	); got != DefaultTable {
		t.Fatalf("too-long table = %q", got)
	}
}

func TestStoreQueryTemplatesAreInitializedOnce(t *testing.T) {
	s := &store{table: "jobs", history: "jobs_history"}

	const callers = 16

	templates := make(chan *storeQueryTemplates, callers)

	var wait sync.WaitGroup

	for range callers {
		wait.Add(1)

		go func() {
			defer wait.Done()

			templates <- s.queries()
		}()
	}

	wait.Wait()
	close(templates)

	first := <-templates
	for template := range templates {
		if first != template {
			t.Fatal("query templates were not reused")
		}
	}

	if !strings.Contains(first.queue, `INSERT INTO "jobs"`) ||
		!strings.Contains(first.queue, `INSERT INTO "jobs_history"`) {
		t.Fatalf(
			"queue template does not contain quoted tables: %q",
			first.queue,
		)
	}

	s.table = "other_jobs"
	if got := s.queries().get; strings.Contains(got, `"other_jobs"`) {
		t.Fatalf("query templates changed after initialization: %q", got)
	}
}

func TestValidateIdentity(t *testing.T) {
	if err := validateIdentity(
		"service",
		"c2b604c6-6960-41a7-b330-5083ca633434",
	); err != nil {
		t.Fatal(err)
	}

	if err := validateIdentity(
		"",
		"c2b604c6-6960-41a7-b330-5083ca633434",
	); err == nil {
		t.Fatal("missing service accepted")
	}

	if err := validateIdentity("service", ""); err == nil {
		t.Fatal("missing workspace accepted")
	}
}

func TestJobStatesAreDistinct(t *testing.T) {
	states := map[string]bool{
		StatusQueued:     true,
		StatusProcessing: true,
		StatusCompleted:  true,
		StatusFailed:     true,
	}
	if len(states) != 4 {
		t.Fatalf("states = %v", states)
	}

	if !errors.Is(ErrActiveJob, ErrActiveJob) {
		t.Fatal("active-job sentinel is not comparable")
	}
}

func TestNewTokenIsUnique(t *testing.T) {
	if first, second := newToken(), newToken(); first == second ||
		first == "" ||
		second == "" {
		t.Fatalf("tokens = %q, %q", first, second)
	}
}

func TestDiskArchiveStoresOpaqueArchive(t *testing.T) {
	archive, err := NewDiskArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	key, err := archive.Store(
		t.Context(),
		ArchiveObject{FileName: "../unsafe.zip"},
		strings.NewReader("archive"),
	)
	if err != nil {
		t.Fatal(err)
	}

	if filepath.Base(key) != key || key == "../unsafe.zip" {
		t.Fatalf("archive key = %q", key)
	}

	reader, err := archive.Open(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}

	data, err := io.ReadAll(reader)

	_ = reader.Close()

	if err != nil || string(data) != "archive" {
		t.Fatalf("archive data = %q, err = %v", data, err)
	}

	if err := archive.Delete(t.Context(), key); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(
		filepath.Join(archive.directory, key),
	); !os.IsNotExist(
		err,
	) {
		t.Fatalf("archive remains after delete: %v", err)
	}

	if _, err := archive.Open(t.Context(), "../archive.zip"); err == nil {
		t.Fatal("path traversal key accepted")
	}
}

func TestStartIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	manager := &Manager{
		workers:       goroutine.New(),
		rootCtx:       t.Context(),
		cancel:        func() {},
		idleDelay:     time.Millisecond,
		cleanupPeriod: time.Hour,
	}
	if !manager.Start(ctx) {
		t.Fatal("idempotent start failed")
	}

	manager.Close()
}
