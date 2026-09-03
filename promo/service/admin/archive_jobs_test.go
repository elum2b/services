package admin

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"

	"github.com/elum2b/services/internal/utils/importexport/jobs"
)

func TestArchiveJobHandlerImportRejectsUnexpectedEntries(t *testing.T) {
	for _, names := range [][]string{
		{"manifest.json", "manifest.json"},
		{"manifest.json", "unexpected.json"},
	} {
		t.Run(names[1], func(t *testing.T) {
			var data bytes.Buffer

			writer := zip.NewWriter(&data)

			for _, name := range names {
				entry, err := writer.Create(name)
				if err != nil {
					t.Fatal(err)
				}

				if _, err := entry.Write([]byte(`{}`)); err != nil {
					t.Fatal(err)
				}
			}

			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}

			err := (archiveJobHandler{admin: &Admin{}}).Import(
				context.Background(),
				jobs.Job{Options: []byte(`{}`)},
				bytes.NewReader(data.Bytes()),
			)
			if err == nil {
				t.Fatal("unexpected ZIP entries were accepted")
			}
		})
	}
}
