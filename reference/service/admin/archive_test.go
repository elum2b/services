package admin

import (
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
			t.Errorf("OriginalName(%q) = %q, %t; want %q, true", format, got, ok, name)
		}
	}
}
