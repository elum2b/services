// Package media prepares uploaded visual assets for storage and delivery.
package media

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	_ "image/jpeg" // Register the JPEG decoder with image.Decode.
	"io"
	"math"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // Register the WebP decoder with image.Decode.

	"github.com/kolesa-team/go-webp/encoder"

	"github.com/elum2b/services/internal/utils/media/lottie"
	"github.com/elum2b/services/internal/utils/media/svg"
)

const (
	// DefaultMaxInputBytes limits an uploaded source before decoding.
	DefaultMaxInputBytes = 32 << 20
	// DefaultMaxPixels prevents decompression bombs while decoding images.
	DefaultMaxPixels int64 = 32_000_000
)

var (
	ErrUnsupportedFormat  = errors.New("unsupported media format")
	ErrInputTooLarge      = errors.New("media input is too large")
	ErrImageTooLarge      = errors.New("media image is too large")
	ErrInvalidPreviewSize = errors.New("invalid preview size")
	ErrUnsafeContent      = errors.New("unsafe media content")
	ErrFirstFrameRequired = errors.New("vector media first frame is required")
)

// Format identifies the source encoding.
type Format string

const (
	FormatJPEG   Format = "jpeg"
	FormatPNG    Format = "png"
	FormatWebP   Format = "webp"
	FormatGIF    Format = "gif"
	FormatLottie Format = "lottie"
	FormatTGS    Format = "tgs"
	FormatSVG    Format = "svg"
)

// Options controls resource limits and generated variants.
type Options struct {
	PreviewSizes  []int
	MaxInputBytes int
	MaxPixels     int64
	FirstFrame    []byte
}

// Preview is a WebP rendition whose longest side equals Size.
type Preview struct {
	Size   int
	Width  int
	Height int
	WebP   []byte
}

// Asset retains the original bytes and contains derived delivery assets.
type Asset struct {
	Format      Format
	Width       int
	Height      int
	Original    []byte
	Previews    []Preview
	Placeholder []byte // SVG generated from an 8x8 color grid.
}

// Process validates source bytes, uses a client-rendered first frame for vector
// media, and
// produces WebP previews and an SVG placeholder. The original bytes are never
// transcoded, so Lottie sources can be stored without loss.
func Process(
	ctx context.Context,
	source []byte,
	options Options,
) (Asset, error) {
	options = normalizedOptions(options)

	if len(source) == 0 {
		return Asset{}, fmt.Errorf("%w: empty input", ErrUnsupportedFormat)
	}

	if len(source) > options.MaxInputBytes {
		return Asset{}, fmt.Errorf(
			"%w: %d bytes exceeds %d",
			ErrInputTooLarge,
			len(source),
			options.MaxInputBytes,
		)
	}

	for _, size := range options.PreviewSizes {
		if size <= 0 {
			return Asset{}, fmt.Errorf("%w: %d", ErrInvalidPreviewSize, size)
		}
	}

	format, frame, err := decode(
		ctx,
		source,
		options.FirstFrame,
		options.MaxPixels,
		options.MaxInputBytes,
	)
	if err != nil {
		return Asset{}, err
	}

	bounds := frame.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return Asset{}, fmt.Errorf(
			"%w: invalid dimensions",
			ErrUnsupportedFormat,
		)
	}

	if int64(bounds.Dx())*int64(bounds.Dy()) > options.MaxPixels {
		return Asset{}, fmt.Errorf(
			"%w: %dx%d exceeds %d pixels",
			ErrImageTooLarge,
			bounds.Dx(),
			bounds.Dy(),
			options.MaxPixels,
		)
	}

	previews := make([]Preview, 0, len(options.PreviewSizes))
	if format != FormatSVG {
		for _, size := range options.PreviewSizes {
			preview := resize(frame, size)

			var encoded bytes.Buffer

			options, err := encoder.NewLossyEncoderOptions(
				encoder.PresetDefault,
				90,
			)
			if err != nil {
				return Asset{}, fmt.Errorf("create preview %d options: %w", size, err)
			}
			webp, err := encoder.NewEncoder(preview, options)
			if err != nil {
				return Asset{}, fmt.Errorf("create preview %d encoder: %w", size, err)
			}
			if err := webp.Encode(&encoded); err != nil {
				return Asset{}, fmt.Errorf("encode preview %d: %w", size, err)
			}

			previews = append(
				previews,
				Preview{
					Size:   size,
					Width:  preview.Bounds().Dx(),
					Height: preview.Bounds().Dy(),
					WebP:   encoded.Bytes(),
				},
			)
		}
	}

	return Asset{
		Format:      format,
		Width:       bounds.Dx(),
		Height:      bounds.Dy(),
		Original:    source,
		Previews:    previews,
		Placeholder: SVGPlaceholder(frame),
	}, nil
}

func normalizedOptions(options Options) Options {
	if len(options.PreviewSizes) == 0 {
		options.PreviewSizes = []int{61, 128, 256, 512}
	}

	if options.MaxInputBytes <= 0 {
		options.MaxInputBytes = DefaultMaxInputBytes
	}

	if options.MaxPixels <= 0 {
		options.MaxPixels = DefaultMaxPixels
	}

	return options
}

func decode(
	ctx context.Context,
	source []byte,
	firstFrame []byte,
	maxPixels int64,
	maxInputBytes int,
) (Format, image.Image, error) {
	format, renderSource, ok, err := vectorFormat(source, maxInputBytes)
	if err != nil {
		return "", nil, err
	}
	if ok {
		_ = renderSource
		if len(firstFrame) == 0 {
			return "", nil, fmt.Errorf("%w for %s", ErrFirstFrameRequired, format)
		}
		if len(firstFrame) > maxInputBytes {
			return "", nil, fmt.Errorf("%w: first frame exceeds %d bytes", ErrInputTooLarge, maxInputBytes)
		}
		frame, err := decodeFirstFrame(firstFrame, maxPixels)
		if err != nil {
			return "", nil, err
		}
		return format, frame, nil
	}

	config, name, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrUnsupportedFormat, err)
	}

	format, ok = staticFormat(name)
	if !ok {
		return "", nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, name)
	}

	if config.Width <= 0 || config.Height <= 0 {
		return "", nil, fmt.Errorf(
			"%w: invalid dimensions",
			ErrUnsupportedFormat,
		)
	}

	if int64(config.Width)*int64(config.Height) > maxPixels {
		return "", nil, fmt.Errorf(
			"%w: %dx%d exceeds %d pixels",
			ErrImageTooLarge,
			config.Width,
			config.Height,
			maxPixels,
		)
	}

	if err := validateStatic(format, source); err != nil {
		return "", nil, err
	}

	frame, _, err := image.Decode(bytes.NewReader(source))
	if err != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrUnsupportedFormat, err)
	}

	return format, frame, nil
}

func decodeFirstFrame(source []byte, maxPixels int64) (image.Image, error) {
	config, name, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil {
		return nil, fmt.Errorf("%w: first frame: %w", ErrUnsupportedFormat, err)
	}
	format, ok := staticFormat(name)
	if !ok || (format != FormatPNG && format != FormatWebP) {
		return nil, fmt.Errorf("%w: first frame must be PNG or WebP", ErrUnsupportedFormat)
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxPixels {
		return nil, ErrImageTooLarge
	}
	if err := validateStatic(format, source); err != nil {
		return nil, err
	}
	frame, _, err := image.Decode(bytes.NewReader(source))
	if err != nil {
		return nil, fmt.Errorf("%w: first frame: %w", ErrUnsupportedFormat, err)
	}
	return frame, nil
}

func vectorFormat(source []byte, maxInputBytes int) (Format, []byte, bool, error) {
	if err := svg.Validate(source); err == nil {
		return FormatSVG, source, true, nil
	}
	if len(source) >= 2 && source[0] == 0x1f && source[1] == 0x8b {
		decoded, err := decodeTGS(source, maxInputBytes)
		if err != nil {
			return "", nil, false, fmt.Errorf("%w: invalid TGS: %w", ErrUnsupportedFormat, err)
		}
		if _, err := lottie.Validate(decoded); err == nil {
			return FormatTGS, decoded, true, nil
		}
		return "", nil, false, fmt.Errorf("%w: invalid TGS Lottie document", ErrUnsupportedFormat)
	}
	if _, err := lottie.Validate(source); err == nil {
		return FormatLottie, source, true, nil
	}

	return "", nil, false, nil
}

func decodeTGS(source []byte, maxBytes int) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(source))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	decoded, err := io.ReadAll(io.LimitReader(reader, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(decoded) > maxBytes {
		return nil, ErrInputTooLarge
	}
	return decoded, nil
}

func validateStatic(format Format, source []byte) error {
	switch format {
	case FormatPNG:
		return validatePNG(source)
	case FormatWebP:
		return validateWebP(source)
	case FormatJPEG:
		if len(source) < 4 || source[0] != 0xff || source[1] != 0xd8 ||
			source[len(source)-2] != 0xff ||
			source[len(source)-1] != 0xd9 {
			return fmt.Errorf(
				"%w: JPEG has invalid boundaries",
				ErrUnsafeContent,
			)
		}
	case FormatGIF:
		if len(source) < 14 ||
			(string(source[:6]) != "GIF87a" && string(source[:6]) != "GIF89a") ||
			source[len(source)-1] != 0x3b {
			return fmt.Errorf(
				"%w: GIF has invalid boundaries",
				ErrUnsafeContent,
			)
		}
	}

	return nil
}

func validatePNG(source []byte) error {
	const signatureLength = 8

	if len(source) < signatureLength ||
		!bytes.Equal(source[:signatureLength], []byte("\x89PNG\r\n\x1a\n")) {
		return fmt.Errorf("%w: invalid PNG signature", ErrUnsafeContent)
	}

	offset := signatureLength
	seenHeader, seenData := false, false

	for offset < len(source) {
		if len(source)-offset < 12 {
			return fmt.Errorf("%w: truncated PNG chunk", ErrUnsafeContent)
		}

		length := int(binary.BigEndian.Uint32(source[offset:]))
		if length > len(source)-offset-12 {
			return fmt.Errorf("%w: invalid PNG chunk length", ErrUnsafeContent)
		}

		chunkType := string(source[offset+4 : offset+8])
		dataEnd := offset + 8 + length

		if binary.BigEndian.Uint32(
			source[dataEnd:dataEnd+4],
		) != crc32.ChecksumIEEE(
			source[offset+4:dataEnd],
		) {
			return fmt.Errorf("%w: PNG chunk checksum", ErrUnsafeContent)
		}

		switch chunkType {
		case "IHDR":
			if seenHeader || offset != signatureLength || length != 13 {
				return fmt.Errorf("%w: invalid PNG header", ErrUnsafeContent)
			}

			seenHeader = true
		case "PLTE", "tRNS":
			if !seenHeader || seenData {
				return fmt.Errorf(
					"%w: invalid PNG palette chunk",
					ErrUnsafeContent,
				)
			}
		case "IDAT":
			if !seenHeader {
				return fmt.Errorf(
					"%w: PNG data before header",
					ErrUnsafeContent,
				)
			}

			seenData = true
		case "IEND":
			if !seenHeader || !seenData || length != 0 ||
				dataEnd+4 != len(source) {
				return fmt.Errorf("%w: invalid PNG end", ErrUnsafeContent)
			}

			return nil
		default:
			// Preview delivery needs pixels only, never embedded metadata or extensions.
			return fmt.Errorf(
				"%w: disallowed PNG chunk %q",
				ErrUnsafeContent,
				chunkType,
			)
		}

		offset = dataEnd + 4
	}

	return fmt.Errorf("%w: PNG is missing IEND", ErrUnsafeContent)
}

func validateWebP(source []byte) error {
	if len(source) < 20 || string(source[:4]) != "RIFF" ||
		string(source[8:12]) != "WEBP" ||
		int(binary.LittleEndian.Uint32(source[4:8]))+8 != len(source) {
		return fmt.Errorf("%w: invalid WebP RIFF container", ErrUnsafeContent)
	}

	offset, hasImage := 12, false
	for offset < len(source) {
		if len(source)-offset < 8 {
			return fmt.Errorf("%w: truncated WebP chunk", ErrUnsafeContent)
		}

		chunkType := string(source[offset : offset+4])
		length := int(binary.LittleEndian.Uint32(source[offset+4 : offset+8]))

		offset += 8

		if length > len(source)-offset {
			return fmt.Errorf("%w: invalid WebP chunk length", ErrUnsafeContent)
		}

		switch chunkType {
		case "VP8 ", "VP8L", "VP8X", "ALPH", "ANIM", "ANMF":
			if chunkType == "VP8 " || chunkType == "VP8L" ||
				chunkType == "ANMF" {
				hasImage = true
			}
		default:
			return fmt.Errorf(
				"%w: disallowed WebP chunk %q",
				ErrUnsafeContent,
				chunkType,
			)
		}

		offset += length
		if length%2 != 0 {
			offset++
		}
	}

	if offset != len(source) || !hasImage {
		return fmt.Errorf("%w: incomplete WebP image", ErrUnsafeContent)
	}

	return nil
}

func staticFormat(name string) (Format, bool) {
	switch strings.ToLower(name) {
	case "jpeg":
		return FormatJPEG, true
	case "png":
		return FormatPNG, true
	case "webp":
		return FormatWebP, true
	case "gif":
		return FormatGIF, true
	default:
		return "", false
	}
}

func resize(source image.Image, longestSide int) *image.NRGBA {
	bounds := source.Bounds()
	scale := float64(longestSide) / float64(max(bounds.Dx(), bounds.Dy()))
	width := max(1, int(math.Round(float64(bounds.Dx())*scale)))
	height := max(1, int(math.Round(float64(bounds.Dy())*scale)))
	result := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(
		result,
		result.Bounds(),
		source,
		bounds,
		draw.Over,
		nil,
	)

	return result
}

// SVGPlaceholder returns a compact, dependency-free blurred color preview.
func SVGPlaceholder(source image.Image) []byte {
	const cells = 8

	bounds := source.Bounds()

	var output strings.Builder

	output.Grow(2_800)
	fmt.Fprintf(
		&output,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" preserveAspectRatio="none">`,
		cells,
		cells,
	)

	for y := range cells {
		for x := range cells {
			pixel := averageCell(source, bounds, x, y, cells)
			fmt.Fprintf(
				&output,
				`<path fill="#%02x%02x%02x" fill-opacity="%.3f" d="M%d %dh1v1h-1z"/>`,
				pixel.R,
				pixel.G,
				pixel.B,
				float64(pixel.A)/255,
				x,
				y,
			)
		}
	}

	output.WriteString(`</svg>`)

	return []byte(output.String())
}

func averageCell(
	source image.Image,
	bounds image.Rectangle,
	x, y, cells int,
) color.NRGBA {
	x0, x1 := bounds.Min.X+x*bounds.Dx()/cells, bounds.Min.X+(x+1)*bounds.Dx()/cells
	y0, y1 := bounds.Min.Y+y*bounds.Dy()/cells, bounds.Min.Y+(y+1)*bounds.Dy()/cells
	if x0 == x1 {
		x1++
	}
	if y0 == y1 {
		y1++
	}
	// A bounded sample count keeps placeholder generation independent of source size.
	xStep := max(1, (x1-x0+15)/16)
	yStep := max(1, (y1-y0+15)/16)

	var (
		red, green, blue, alpha uint64
		count                   uint64
	)

	for py := y0; py < y1; py += yStep {
		for px := x0; px < x1; px += xStep {
			if nrgba, ok := source.(*image.NRGBA); ok {
				offset := (py-nrgba.Rect.Min.Y)*nrgba.Stride + (px-nrgba.Rect.Min.X)*4

				red += uint64(nrgba.Pix[offset])
				green += uint64(nrgba.Pix[offset+1])
				blue += uint64(nrgba.Pix[offset+2])
				alpha += uint64(nrgba.Pix[offset+3])
			} else {
				r, g, b, a := source.At(px, py).RGBA()

				red += uint64(r >> 8)
				green += uint64(g >> 8)
				blue += uint64(b >> 8)
				alpha += uint64(a >> 8)
			}

			count++
		}
	}

	return color.NRGBA{
		R: uint8(red / count),
		G: uint8(green / count),
		B: uint8(blue / count),
		A: uint8(alpha / count),
	}
}

func max(left, right int) int {
	if left > right {
		return left
	}

	return right
}
