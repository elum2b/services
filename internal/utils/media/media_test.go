package media

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestProcessStaticImage(t *testing.T) {
	source := testPNG(t, 400, 200)

	asset, err := Process(context.Background(), source, Options{})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if asset.Format != FormatPNG || asset.Width != 400 || asset.Height != 200 {
		t.Fatalf("metadata = %+v, want PNG 400x200", asset)
	}

	if !bytes.Equal(asset.Original, source) {
		t.Fatal("original bytes were changed")
	}

	if len(asset.Previews) != 4 {
		t.Fatalf("preview count = %d, want 4", len(asset.Previews))
	}

	for _, preview := range asset.Previews {
		if preview.Width != preview.Size ||
			preview.Height != (preview.Size+1)/2 {
			t.Errorf(
				"preview %d dimensions = %dx%d",
				preview.Size,
				preview.Width,
				preview.Height,
			)
		}

		decoded, err := png.Decode(bytes.NewReader(preview.PNG))
		if err != nil {
			t.Errorf("preview %d is not PNG: %v", preview.Size, err)

			continue
		}

		if decoded.Bounds().Dx() != preview.Width ||
			decoded.Bounds().Dy() != preview.Height {
			t.Errorf(
				"decoded preview dimensions = %v, want %dx%d",
				decoded.Bounds(),
				preview.Width,
				preview.Height,
			)
		}
	}

	if !bytes.HasPrefix(asset.Placeholder, []byte("<svg ")) ||
		!bytes.Contains(asset.Placeholder, []byte("#ff0000")) {
		t.Fatalf("placeholder = %s", asset.Placeholder)
	}
}

func TestProcessLottieUsesRenderer(t *testing.T) {
	source := []byte(`{"v":"5.12.0","w":20,"h":10,"layers":[]}`)

	asset, err := Process(
		context.Background(),
		source,
		Options{PreviewSizes: []int{61}, FirstFrame: testPNG(t, 20, 10)},
	)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if asset.Format != FormatLottie {
		t.Fatalf(
			"format = %q renderer called = %t",
			asset.Format,
			false,
		)
	}

	if got := asset.Previews[0]; got.Width != 61 || got.Height != 31 {
		t.Fatalf(
			"preview dimensions = %dx%d, want 61x31",
			got.Width,
			got.Height,
		)
	}
}

func TestProcessTGSRendersDecompressedLottie(t *testing.T) {
	json := []byte(`{"v":"5.12.0","w":20,"h":10,"layers":[]}`)
	source := testTGS(t, json)
	asset, err := Process(context.Background(), source, Options{PreviewSizes: []int{61}, FirstFrame: testPNG(t, 20, 10)})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if asset.Format != FormatTGS || !bytes.Equal(asset.Original, source) {
		t.Fatalf("format=%q original=%q", asset.Format, asset.Original)
	}
}

func TestProcessSVGCreatesOnlyPlaceholder(t *testing.T) {
	source := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 10"><path d="M0 0h20v10H0z"/></svg>`)
	asset, err := Process(context.Background(), source, Options{FirstFrame: testPNG(t, 20, 10)})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if asset.Format != FormatSVG || len(asset.Previews) != 0 || len(asset.Placeholder) == 0 {
		t.Fatalf("format=%q previews=%d placeholder=%d", asset.Format, len(asset.Previews), len(asset.Placeholder))
	}
}

func TestProcessRejectsTGSDecompressionBomb(t *testing.T) {
	_, err := Process(context.Background(), testTGS(t, bytes.Repeat([]byte("x"), 128)), Options{MaxInputBytes: 64, FirstFrame: testPNG(t, 1, 1)})
	if !errors.Is(err, ErrInputTooLarge) {
		t.Fatalf("Process() error = %v", err)
	}
}

func TestProcessRiveUsesRenderer(t *testing.T) {

	asset, err := Process(
		context.Background(),
		[]byte("RIVE\x00\x01"),
		Options{PreviewSizes: []int{61}, FirstFrame: testPNG(t, 10, 20)},
	)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if asset.Format != FormatRive {
		t.Fatalf(
			"format = %q renderer called = %t",
			asset.Format,
			false,
		)
	}

	if got := asset.Previews[0]; got.Width != 31 || got.Height != 61 {
		t.Fatalf(
			"preview dimensions = %dx%d, want 31x61",
			got.Width,
			got.Height,
		)
	}
}

func TestProcessRequiresVectorFirstFrame(t *testing.T) {
	_, err := Process(
		context.Background(),
		[]byte("RIVE\x00\x01"),
		Options{},
	)

	if !errors.Is(err, ErrFirstFrameRequired) {
		t.Fatalf("Process() error = %v", err)
	}
}

func TestProcessRejectsInvalidAndOversizedInput(t *testing.T) {
	_, err := Process(context.Background(), []byte("invalid"), Options{})
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("invalid error = %v", err)
	}

	_, err = Process(
		context.Background(),
		testPNG(t, 20, 20),
		Options{MaxInputBytes: 1},
	)
	if !errors.Is(err, ErrInputTooLarge) {
		t.Fatalf("size error = %v", err)
	}

	_, err = Process(
		context.Background(),
		testPNG(t, 20, 20),
		Options{PreviewSizes: []int{0}},
	)
	if !errors.Is(err, ErrInvalidPreviewSize) {
		t.Fatalf("preview size error = %v", err)
	}

	_, err = Process(
		context.Background(),
		append(testPNG(t, 20, 20), []byte("<script>bad()</script>")...),
		Options{},
	)
	if !errors.Is(err, ErrUnsafeContent) {
		t.Fatalf("trailing PNG error = %v", err)
	}
}

func TestProcessRejectsInvalidVectorFirstFrame(t *testing.T) {
	_, err := Process(
		context.Background(),
		[]byte(`{"v":"5.12.0","w":1,"h":1,"layers":[]}`),
		Options{FirstFrame: []byte("invalid")},
	)
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("error = %v", err)
	}
}

func TestStaticFormat(t *testing.T) {
	for name, want := range map[string]Format{"jpeg": FormatJPEG, "png": FormatPNG, "webp": FormatWebP, "gif": FormatGIF} {
		got, ok := staticFormat(name)
		if !ok || got != want {
			t.Errorf(
				"staticFormat(%q) = %q, %t; want %q, true",
				name,
				got,
				ok,
				want,
			)
		}
	}
}

func testPNG(t testing.TB, width, height int) []byte {
	t.Helper()

	var result bytes.Buffer

	if err := png.Encode(&result, testImage(width, height)); err != nil {
		t.Fatal(err)
	}

	return result.Bytes()
}

func testTGS(t testing.TB, data []byte) []byte {
	t.Helper()
	var result bytes.Buffer
	writer := gzip.NewWriter(&result)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func testImage(width, height int) *image.NRGBA {
	result := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			result.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}

	return result
}
