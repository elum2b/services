package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type diskStore struct{ directory string }

func newDisk(directory string) (*diskStore, error) {
	if directory == "" {
		binaryPath, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf(
				"resolve reference storage directory: %w",
				err,
			)
		}

		directory = filepath.Join(filepath.Dir(binaryPath), "reference")
	}

	return &diskStore{directory: directory}, nil
}

func (s *diskStore) Replace(
	ctx context.Context,
	workspaceID, resourceKey, version string,
	files Files,
) (Objects, error) {
	if err := ctx.Err(); err != nil {
		return Objects{}, err
	}

	if err := validateFiles(files); err != nil {
		return Objects{}, err
	}

	prefix, err := objectPrefix(workspaceID, resourceKey, version)
	if err != nil {
		return Objects{}, err
	}

	result := Objects{Previews: make(map[int]string, len(files.Previews))}
	if result.Original, err = s.write(
		prefix+"/"+files.OriginalName,
		files.Original,
	); err != nil {
		return Objects{}, err
	}

	for _, preview := range files.Previews {
		ref, err := s.write(
			fmt.Sprintf("%s/preview-%d.webp", prefix, preview.Size),
			preview.File,
		)
		if err != nil {
			return Objects{}, err
		}

		result.Previews[preview.Size] = ref
	}

	result.Placeholder, err = s.write(
		prefix+"/placeholder.svg",
		files.Placeholder,
	)
	if err != nil {
		return Objects{}, err
	}

	return result, nil
}

func (s *diskStore) Read(
	ctx context.Context,
	reference string,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(
		filepath.Join(s.directory, filepath.FromSlash(reference)),
	)
	if err != nil {
		return nil, fmt.Errorf("read reference media file: %w", err)
	}

	return data, nil
}

func (s *diskStore) ReadVersion(
	ctx context.Context,
	workspaceID, resourceKey, version, originalName string,
	size int,
) ([]byte, error) {
	reference, err := versionReference(
		workspaceID,
		resourceKey,
		version,
		originalName,
		size,
	)
	if err != nil {
		return nil, err
	}

	return s.Read(ctx, reference)
}

func (s *diskStore) DeleteVersion(
	ctx context.Context,
	workspaceID, resourceKey, version string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	prefix, err := objectPrefix(workspaceID, resourceKey, version)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(
		filepath.Join(s.directory, filepath.FromSlash(prefix)),
	); err != nil {
		return fmt.Errorf("delete resource media version: %w", err)
	}

	return nil
}

func (s *diskStore) write(reference string, file File) (string, error) {
	path := filepath.Join(s.directory, filepath.FromSlash(reference))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", fmt.Errorf("create reference media directory: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return "", fmt.Errorf("create temporary reference media file: %w", err)
	}

	temporaryPath := temporary.Name()

	defer os.Remove(temporaryPath)

	if _, err := temporary.Write(file.Data); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("write reference media file: %w", err)
	}

	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close reference media file: %w", err)
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("remove replaced reference media file: %w", err)
	}

	if err := os.Rename(temporaryPath, path); err != nil {
		return "", fmt.Errorf("replace reference media file: %w", err)
	}

	return reference, nil
}
