package reference

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveMediaFixtures(t *testing.T) {
	directory := filepath.Join("testdata", "archive-media")

	original, err := os.ReadFile(filepath.Join(directory, "original.json"))
	if err != nil {
		t.Fatalf("read original fixture: %v", err)
	}

	if !json.Valid(original) {
		t.Fatal("original fixture is not valid JSON")
	}

	placeholder, err := os.ReadFile(filepath.Join(directory, "placeholder.svg"))
	if err != nil {
		t.Fatalf("read placeholder fixture: %v", err)
	}

	if !bytes.Contains(placeholder, []byte("<svg")) {
		t.Fatal("placeholder fixture is not SVG")
	}

	preview, err := os.ReadFile(filepath.Join(directory, "preview-512.webp"))
	if err != nil {
		t.Fatalf("read preview fixture: %v", err)
	}

	if len(preview) < 12 || !bytes.Equal(preview[:4], []byte("RIFF")) ||
		!bytes.Equal(preview[8:12], []byte("WEBP")) {
		t.Fatal("preview fixture is not WebP")
	}
}
