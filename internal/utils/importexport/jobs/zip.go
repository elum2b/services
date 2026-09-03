package jobs

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
)

func normalizedZIPLimits(limits ZIPLimits) ZIPLimits {
	if limits.MaxEntries <= 0 {
		limits.MaxEntries = DefaultMaxZIPEntries
	}

	if limits.MaxCompressedBytes <= 0 {
		limits.MaxCompressedBytes = DefaultMaxUploadBytes
	}

	if limits.MaxUncompressedBytes <= 0 {
		limits.MaxUncompressedBytes = DefaultMaxZIPUncompressedBytes
	}

	if limits.MaxCompressionRatio <= 0 {
		limits.MaxCompressionRatio = DefaultMaxZIPCompressionRatio
	}

	return limits
}

// ValidateManifestZIP validates ZIP bomb limits and requires one manifest when
// manifestName is non-empty. It consumes source and is intended for upload
// boundaries; callers that already have a file should use validateZIPFile.
func ValidateManifestZIP(
	ctx context.Context,
	source io.Reader,
	limits ZIPLimits,
	manifestName string,
) error {
	if source == nil {
		return fmt.Errorf("%w: archive source is required", ErrInvalidZIP)
	}

	limits = normalizedZIPLimits(limits)

	temporary, err := os.CreateTemp("", "importexport-validate-*.zip")
	if err != nil {
		return fmt.Errorf("create ZIP validation file: %w", err)
	}

	path := temporary.Name()

	defer os.Remove(path)
	defer temporary.Close()

	if err := copyUpload(
		ctx,
		temporary,
		source,
		limits.MaxCompressedBytes,
	); err != nil {
		return fmt.Errorf("copy ZIP for validation: %w", err)
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close ZIP validation file: %w", err)
	}

	return validateZIPFile(ctx, path, limits, manifestName)
}

func validateZIPFile(
	ctx context.Context,
	path string,
	limits ZIPLimits,
	manifestName string,
) error {
	limits = normalizedZIPLimits(limits)

	archive, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidZIP, err)
	}
	defer archive.Close()

	if len(archive.File) > limits.MaxEntries {
		return fmt.Errorf("%w: too many entries", ErrInvalidZIP)
	}

	var compressed, uncompressed, actualUncompressed int64

	if manifestName != "" &&
		(len(archive.File) != 1 || archive.File[0].Name != manifestName) {
		return fmt.Errorf("%w: expected only %s", ErrInvalidZIP, manifestName)
	}

	for _, file := range archive.File {
		if err := ctx.Err(); err != nil {
			return err
		}

		if file.CompressedSize64 > uint64(limits.MaxCompressedBytes) ||
			file.UncompressedSize64 > uint64(limits.MaxUncompressedBytes) {
			return fmt.Errorf("%w: entry size exceeds limit", ErrInvalidZIP)
		}

		compressed += int64(file.CompressedSize64)
		uncompressed += int64(file.UncompressedSize64)

		if compressed > limits.MaxCompressedBytes ||
			uncompressed > limits.MaxUncompressedBytes {
			return fmt.Errorf("%w: archive size exceeds limit", ErrInvalidZIP)
		}

		if file.CompressedSize64 == 0 && file.UncompressedSize64 > 0 ||
			file.CompressedSize64 > 0 &&
				file.UncompressedSize64/file.CompressedSize64 > uint64(
					limits.MaxCompressionRatio,
				) {
			return fmt.Errorf(
				"%w: compression ratio exceeds limit",
				ErrInvalidZIP,
			)
		}

		reader, err := file.Open()
		if err != nil {
			return fmt.Errorf("%w: open entry: %w", ErrInvalidZIP, err)
		}

		remaining := limits.MaxUncompressedBytes - actualUncompressed
		read, readErr := io.Copy(
			io.Discard,
			io.LimitReader(
				&contextReader{ctx: ctx, reader: reader},
				remaining+1,
			),
		)
		closeErr := reader.Close()

		actualUncompressed += read

		if readErr != nil || closeErr != nil {
			return fmt.Errorf("%w: read entry", ErrInvalidZIP)
		}

		if actualUncompressed > limits.MaxUncompressedBytes {
			return fmt.Errorf("%w: archive size exceeds limit", ErrInvalidZIP)
		}
	}

	return nil
}

func copyUpload(
	ctx context.Context,
	destination io.Writer,
	source io.Reader,
	maxBytes int64,
) error {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxUploadBytes
	}

	written, err := io.Copy(
		destination,
		io.LimitReader(&contextReader{ctx: ctx, reader: source}, maxBytes+1),
	)
	if err != nil {
		return err
	}

	if written > maxBytes {
		return ErrArchiveTooLarge
	}

	return nil
}

func isTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	type temporary interface{ Temporary() bool }

	var temporaryError temporary

	if errors.As(err, &temporaryError) && temporaryError.Temporary() {
		return true
	}

	var networkError net.Error

	return errors.As(err, &networkError) && networkError.Timeout()
}
