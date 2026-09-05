package jobs

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"

	json "github.com/goccy/go-json"
)

// WriteManifestZIP spools a bounded manifest ZIP to a private temporary file.
// STORE keeps even highly repetitive exports importable under ZIP ratio limits.
func WriteManifestZIP(ctx context.Context, name string, maxBytes int64, encode func(io.Writer) error, preflight ...func(io.Reader) error) (io.ReadCloser, error) {
	f, err := NewStagingFile("importexport-export-*.zip")
	if err != nil {
		return nil, err
	}
	out := &temporaryArchive{File: f}
	ok := false
	defer func() {
		if !ok {
			_ = out.Close()
		}
	}()
	w := zip.NewWriter(&boundedWriter{ctx: ctx, writer: f, remaining: maxBytes + 4096})
	entry, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
	if err != nil {
		return nil, err
	}
	if err := encode(&boundedWriter{ctx: ctx, writer: entry, remaining: maxBytes}); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	if len(preflight) > 0 {
		info, err := f.Stat()
		if err != nil {
			return nil, err
		}
		archive, err := zip.NewReader(f, info.Size())
		if err != nil {
			return nil, err
		}
		for _, validate := range preflight {
			entry, err := archive.File[0].Open()
			if err != nil {
				return nil, err
			}
			err = validate(&contextReader{ctx: ctx, reader: entry})
			_ = entry.Close()
			if err != nil {
				return nil, err
			}
		}
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	ok = true
	return out, nil
}

// ReadManifestZIP validates the complete ZIP (including CRC and actual inflated
// size) before decoding. It never buffers the compressed upload in memory.
func ReadManifestZIP(ctx context.Context, source io.Reader, name string, limits ZIPLimits, decode func(io.Reader) error, preflight ...func(io.Reader) error) error {
	limits = normalizedZIPLimits(limits)
	f, err := NewStagingFile("importexport-import-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := copyUpload(ctx, f, source, limits.MaxCompressedBytes); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := validateZIPFile(ctx, f.Name(), limits, name); err != nil {
		return err
	}
	archive, err := zip.OpenReader(f.Name())
	if err != nil {
		return err
	}
	defer archive.Close()
	if len(archive.File) != 1 || archive.File[0].Name != name {
		return ErrInvalidZIP
	}
	for _, validate := range preflight {
		entry, err := archive.File[0].Open()
		if err != nil {
			return err
		}
		err = validate(&contextReader{ctx: ctx, reader: entry})
		_ = entry.Close()
		if err != nil {
			return err
		}
	}
	entry, err := archive.File[0].Open()
	if err != nil {
		return err
	}
	defer entry.Close()
	if err := decode(&contextReader{ctx: ctx, reader: entry}); err != nil {
		return fmt.Errorf("decode import manifest: %w", err)
	}
	return ctx.Err()
}

// ValidateJSONShape bounds allocation amplification before a typed decoder
// allocates slices/maps. A tiny JSON null can otherwise allocate a large DTO.
func ValidateJSONShape(ctx context.Context, source io.Reader, maxRecords int64) error {
	decoder := json.NewDecoder(source)
	stack := make([]json.Delim, 0, 32)
	var items int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			if len(stack) != 0 {
				return ErrInvalidZIP
			}
			return nil
		}
		if err != nil {
			return err
		}
		delimiter, isDelimiter := token.(json.Delim)
		closing := isDelimiter && (delimiter == ']' || delimiter == '}')
		if !closing && len(stack) > 0 && stack[len(stack)-1] == '[' {
			items++
		}
		if isDelimiter {
			if closing {
				if len(stack) == 0 {
					return ErrInvalidZIP
				}
				stack = stack[:len(stack)-1]
			} else {
				items++
				stack = append(stack, delimiter)
			}
		}
		if items > maxRecords*4 || len(stack) > 128 {
			return ErrArchiveTooLarge
		}
	}
}

type temporaryArchive struct{ *os.File }

func (f *temporaryArchive) Close() error {
	err := f.File.Close()
	_ = os.Remove(f.Name())
	return err
}

type boundedWriter struct {
	ctx       context.Context
	writer    io.Writer
	remaining int64
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.remaining {
		return 0, ErrArchiveTooLarge
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	return n, err
}
