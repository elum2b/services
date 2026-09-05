package admin

import (
	"archive/zip"
	"bytes"
	"context"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/reference/repository"
	json "github.com/goccy/go-json"
	"io"
	"strings"
	"testing"

	"github.com/elum2b/services/reference/storage"
)

func TestArchiveOriginalNames(t *testing.T) {
	want := map[string]string{
		"jpeg":   "image.jpeg",
		"png":    "image.png",
		"webp":   "image.webp",
		"gif":    "image.gif",
		"lottie": "lottie.json",
		"tgs":    "animation.tgs",
		"svg":    "image.svg",
	}
	for format, name := range want {
		got, ok := storage.OriginalName(format)
		if !ok || got != name {
			t.Errorf(
				"OriginalName(%q) = %q, %t; want %q, true",
				format,
				got,
				ok,
				name,
			)
		}
	}
}

type cleanupProbeStore struct {
	storage.Store
	deleted int
}

func (s *cleanupProbeStore) DeleteVersion(context.Context, string, string, string) error {
	s.deleted++
	return nil
}
func TestCleanupPreservesMediaWhenDurabilityUnknown(t *testing.T) {
	client := sqlwrap.NewUnavailable()
	defer client.Close()
	store := &cleanupProbeStore{}
	a := &Admin{repository: repository.New(client), store: store}
	a.cleanupArchiveVersions("00000000-0000-0000-0000-000000000001", []*repository.Resource{{Key: "media", MediaVersion: "abcdefgh"}})
	if store.deleted != 0 {
		t.Fatal("unknown durability must never delete media")
	}
}
func TestArchiveRejectsInvalidLinksBeforeUpload(t *testing.T) {
	pkg := repository.ExportPackage{Items: []repository.ExportItem{{Key: "item"}}}
	resource := repository.ExportResource{Key: "media", Type: "image", Payload: []byte(`{}`), Format: "png", ContentType: "image/png", SHA256: strings.Repeat("a", 64), Size: 1, Width: 1, Height: 1}
	for _, links := range [][]repository.ExportResourceLink{
		{{ItemKey: "item", ResourceKey: "media", Position: -1}},
		{{ItemKey: "item", ResourceKey: "media", Position: 0}, {ItemKey: "item", ResourceKey: "media", Position: 1}},
	} {
		if err := validateArchiveResources(pkg, []repository.ExportResource{resource}, links); err == nil {
			t.Fatal("invalid links accepted")
		}
	}
}

func TestArchivePublicStatusHidesInfrastructure(t *testing.T) {
	value := publicArchiveJob(jobs.Job{ID: 1, ArchiveKey: "private.zip", LeaseToken: "private-lease", LockedBy: "private-worker", Options: []byte(`{"secret":"value"}`), Error: "private SQL connection"})
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"archive_key", "lease_token", "locked_by", "options", "private"} {
		if bytes.Contains(data, []byte(private)) {
			t.Fatalf("private field leaked: %s", data)
		}
	}
	if value.Error == "" {
		t.Fatal("failure status must still be visible")
	}
}

func TestArchiveBudgetsMatchImport(t *testing.T) {
	writer := &archiveZIPWriter{Writer: zip.NewWriter(io.Discard), entries: maxQueuedArchiveFiles}
	if err := writeArchiveFile(writer, "overflow", []byte("x")); err == nil {
		t.Fatal("entry budget ignored")
	}
	writer = &archiveZIPWriter{Writer: zip.NewWriter(io.Discard), bytes: maxQueuedArchiveUncompressedSize}
	if err := writeArchiveFile(writer, "overflow", []byte("x")); err == nil {
		t.Fatal("uncompressed budget ignored")
	}
	limited := &archiveSizeWriter{dst: io.Discard, bytes: maxQueuedArchiveSize}
	if _, err := limited.Write([]byte("x")); err == nil {
		t.Fatal("compressed import budget ignored")
	}
}
