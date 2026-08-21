package media

import (
	"context"
	"testing"
)

func BenchmarkProcessPNG(b *testing.B) {
	source := testPNG(b, 1024, 1024)
	options := Options{PreviewSizes: []int{61, 128, 256, 512}}

	b.ReportAllocs()
	b.SetBytes(int64(len(source)))

	for b.Loop() {
		if _, err := Process(
			context.Background(),
			source,
			options,
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSVGPlaceholder(b *testing.B) {
	frame := testImage(1024, 1024)

	b.ReportAllocs()

	for b.Loop() {
		_ = SVGPlaceholder(frame)
	}
}
