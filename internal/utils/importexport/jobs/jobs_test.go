package jobs

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/elum2b/services/internal/utils/goroutine"
	"github.com/jackc/pgx/v5/pgconn"
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

func TestManifestSpoolingLimitsAndRoundTrip(t *testing.T) {
	payload := strings.Repeat("a", 4096)
	archive, err := WriteManifestZIP(t.Context(), "manifest.json", 5000, func(w io.Writer) error { _, err := io.WriteString(w, payload); return err })
	if err != nil {
		t.Fatal(err)
	}
	name := archive.(*temporaryArchive).Name()
	if err := ReadManifestZIP(t.Context(), archive, "manifest.json", ZIPLimits{MaxUncompressedBytes: 5000}, func(r io.Reader) error {
		data, err := io.ReadAll(r)
		if string(data) != payload {
			t.Fatal("manifest changed")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_ = archive.Close()
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatal("spooled archive survived Close")
	}
	if _, err := WriteManifestZIP(t.Context(), "manifest.json", 10, func(w io.Writer) error { _, err := io.WriteString(w, payload); return err }); !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("limit: %v", err)
	}
}

func TestManifestShapeRejectsAllocationAmplification(t *testing.T) {
	if err := ValidateJSONShape(t.Context(), strings.NewReader(`[`+strings.Repeat(`null,`, 100)+`null]`), 10); !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("array allocation budget: %v", err)
	}
	if err := ValidateJSONShape(t.Context(), strings.NewReader(strings.Repeat(`[`, 200)+`0`+strings.Repeat(`]`, 200)), 1000); !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("nesting budget: %v", err)
	}
	if err := ValidateJSONShape(t.Context(), strings.NewReader(`{"offers":[{"id":"test"}]}`), 10); err != nil {
		t.Fatal(err)
	}
}

func TestStagingSweepKeepsLiveFiles(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	live, err := NewStagingFile("importexport-live-*")
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	stale, err := NewStagingFile("importexport-stale-*")
	if err != nil {
		t.Fatal(err)
	}
	_ = stale.Close()
	if err := sweepStaging(t.Context(), time.Now().Add(time.Hour), 100); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(live.Name()); err != nil {
		t.Fatal("live file deleted")
	}
	if _, err := os.Stat(stale.Name()); !os.IsNotExist(err) {
		t.Fatal("stale spool survived cleanup")
	}
}

func TestDiskArchiveCleansAbandonedStagingButNotActiveWrite(t *testing.T) {
	a, err := NewDiskArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(a.directory, ".archive-")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	key := filepath.Base(f.Name())
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH); err != nil {
		t.Fatal(err)
	}
	entries, err := a.List(t.Context())
	if err != nil || len(entries) != 1 {
		t.Fatalf("staging not listed: %v %v", entries, err)
	}
	if err := a.Delete(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.Name()); err != nil {
		t.Fatal("active staging was deleted")
	}
	_ = f.Close()
	if err := a.Delete(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.Name()); !os.IsNotExist(err) {
		t.Fatal("abandoned staging survived cleanup")
	}
}

func TestPostgresRetryClassification(t *testing.T) {
	for _, code := range []string{"40001", "40P01", "55P03", "53300", "57P01", "08006"} {
		if !isTransient(&pgconn.PgError{Code: code}) {
			t.Fatalf("retryable SQLSTATE %s rejected", code)
		}
	}
	if isTransient(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("unique constraint is not a transient failure")
	}
}

func TestHandlerPanicDoesNotHangWorker(t *testing.T) {
	manager := &Manager{workers: goroutine.New(), store: &store{leaseTime: time.Minute}, handlerTimeout: time.Second}
	defer manager.Close()
	if err := manager.withLease(t.Context(), Job{}, func(context.Context) error { panic("test panic") }); err == nil {
		t.Fatal("handler panic was not converted to an error")
	}
}

func TestS3ArchiveSharesObjectsAcrossInstances(t *testing.T) {
	var mu sync.Mutex
	objects := map[string][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(http.StatusOK)
		case http.MethodPut:
			data, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "read", 500)
				return
			}
			objects[r.URL.Path] = data
			w.Header().Set("ETag", `"test"`)
		case http.MethodGet:
			if r.URL.Query().Get("list-type") == "2" {
				w.Header().Set("Content-Type", "application/xml")
				_, _ = io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated>`)
				for key := range objects {
					_, _ = io.WriteString(w, `<Contents><Key>`+strings.TrimPrefix(key, "/bucket/")+`</Key><LastModified>2026-01-01T00:00:00Z</LastModified></Contents>`)
				}
				_, _ = io.WriteString(w, `</ListBucketResult>`)
				return
			}
			data, ok := objects[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(data)
		case http.MethodDelete:
			delete(objects, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	options := S3Options{Bucket: "bucket", Endpoint: server.URL, Prefix: "test/", AccessKeyID: "test", SecretAccessKey: "test", UsePathStyle: true}
	first, err := NewS3Archive(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewS3Archive(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	// This fixture emulates object APIs, not AWS's optional trailer checksums.
	first.client = s3.New(first.client.Options(), func(o *s3.Options) { o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired })
	key, err := first.Store(t.Context(), ArchiveObject{}, strings.NewReader("shared archive"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := second.Open(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(data) != "shared archive" {
		t.Fatalf("shared object: %q %v", data, err)
	}
	listed, err := second.List(t.Context())
	if err != nil || len(listed) != 1 || listed[0].Key != key {
		t.Fatalf("list: %v %v", listed, err)
	}
	if err := second.Delete(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Open(t.Context(), key); err == nil {
		t.Fatal("deleted object remains")
	}
	if _, err := second.Open(t.Context(), "../unsafe.zip"); err == nil {
		t.Fatal("unsafe key accepted")
	}
}

func TestValidateManifestZIPLimits(t *testing.T) {
	makeZIP := func(names ...string) []byte {
		var data bytes.Buffer

		writer := zip.NewWriter(&data)

		for _, name := range names {
			entry, err := writer.Create(name)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := entry.Write([]byte("manifest")); err != nil {
				t.Fatal(err)
			}
		}

		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}

		return data.Bytes()
	}

	limits := ZIPLimits{
		MaxEntries:           1,
		MaxCompressedBytes:   1024,
		MaxUncompressedBytes: 1024,
		MaxCompressionRatio:  100,
	}
	if err := ValidateManifestZIP(
		t.Context(),
		bytes.NewReader(makeZIP("manifest.json")),
		limits,
		"manifest.json",
	); err != nil {
		t.Fatal(err)
	}

	if err := ValidateManifestZIP(
		t.Context(),
		bytes.NewReader(makeZIP("manifest.json", "other.json")),
		ZIPLimits{},
		"manifest.json",
	); !errors.Is(err, ErrInvalidZIP) {
		t.Fatalf("extra manifest entry error = %v", err)
	}

	if err := ValidateManifestZIP(
		t.Context(),
		bytes.NewReader(makeZIP("manifest.json", "other")),
		limits,
		"manifest.json",
	); !errors.Is(
		err,
		ErrInvalidZIP,
	) {
		t.Fatalf("entry limit error = %v", err)
	}

	if err := ValidateManifestZIP(
		t.Context(),
		bytes.NewReader(makeZIP("manifest.json", "manifest.json")),
		ZIPLimits{},
		"manifest.json",
	); !errors.Is(
		err,
		ErrInvalidZIP,
	) {
		t.Fatalf("duplicate manifest error = %v", err)
	}

	if err := ValidateManifestZIP(
		t.Context(),
		bytes.NewReader(makeZIP("other")),
		ZIPLimits{},
		"manifest.json",
	); !errors.Is(
		err,
		ErrInvalidZIP,
	) {
		t.Fatalf("missing manifest error = %v", err)
	}
}

func TestValidateManifestZIPRejectsCompressionBomb(t *testing.T) {
	var data bytes.Buffer

	writer := zip.NewWriter(&data)

	entry, err := writer.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := entry.Write(bytes.Repeat([]byte("a"), 4096)); err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	err = ValidateManifestZIP(
		t.Context(),
		bytes.NewReader(data.Bytes()),
		ZIPLimits{MaxCompressionRatio: 2},
		"manifest.json",
	)
	if !errors.Is(err, ErrInvalidZIP) {
		t.Fatalf("compression bomb error = %v", err)
	}
}

func TestCopyUploadLimit(t *testing.T) {
	var destination bytes.Buffer

	if err := copyUpload(
		t.Context(),
		&destination,
		strings.NewReader("1234"),
		3,
	); !errors.Is(
		err,
		ErrArchiveTooLarge,
	) {
		t.Fatalf("oversized upload error = %v", err)
	}

	if err := copyUpload(
		t.Context(),
		&destination,
		strings.NewReader("123"),
		3,
	); err != nil {
		t.Fatal(err)
	}
}

type temporaryError struct{}

func (temporaryError) Error() string   { return "temporary" }
func (temporaryError) Temporary() bool { return true }

func TestRetryHelpers(t *testing.T) {
	if !isTransient(temporaryError{}) || isTransient(context.DeadlineExceeded) {
		t.Fatal("transient classification is incorrect")
	}

	if got := retryDelay(time.Second, 2); got != 4*time.Second {
		t.Fatalf("retry delay = %s", got)
	}
}

func TestDiskArchiveListsZIPObjects(t *testing.T) {
	archive, err := NewDiskArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := archive.Store(
		t.Context(),
		ArchiveObject{},
		strings.NewReader("data"),
	); err != nil {
		t.Fatal(err)
	}

	archives, err := archive.List(t.Context())
	if err != nil || len(archives) != 1 || archives[0].Key == "" ||
		archives[0].CreatedAt.IsZero() {
		t.Fatalf("archives = %#v, err = %v", archives, err)
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

func TestWriteManifestZIPRejectsUnimportableShape(t *testing.T) {
	dump, err := WriteManifestZIP(context.Background(), "manifest.json", 1024, func(w io.Writer) error {
		_, err := io.WriteString(w, `{"rows":[{},{},{},{},{}]}`)
		return err
	}, func(r io.Reader) error { return ValidateJSONShape(context.Background(), r, 1) })
	if dump != nil {
		dump.Close()
		t.Fatal("oversized shape returned an archive")
	}
	if err == nil {
		t.Fatal("export bypassed import shape budget")
	}
}
