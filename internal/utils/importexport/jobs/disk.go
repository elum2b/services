package jobs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// DiskArchive stores job dumps below one private directory. Keys are generated
// by Store and are intentionally not derived from user-controlled fields.
type DiskArchive struct{ directory string }

func NewDiskArchive(directory string) (*DiskArchive, error) {
	if directory == "" {
		return nil, fmt.Errorf(
			"importexport jobs: archive directory is required",
		)
	}

	if err := os.MkdirAll(directory, 0o750); err != nil {
		return nil, fmt.Errorf("create importexport archive directory: %w", err)
	}

	return &DiskArchive{directory: directory}, nil
}

func (a *DiskArchive) Store(
	ctx context.Context,
	_ ArchiveObject,
	source io.Reader,
) (string, error) {
	if a == nil || source == nil {
		return "", fmt.Errorf("importexport jobs: archive source is required")
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	temporary, err := os.CreateTemp(a.directory, ".archive-*")
	if err != nil {
		return "", fmt.Errorf("create importexport archive: %w", err)
	}

	temporaryPath := temporary.Name()
	if err := syscall.Flock(int(temporary.Fd()), syscall.LOCK_SH); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		return "", err
	}

	defer os.Remove(temporaryPath)

	if _, err := io.Copy(
		temporary,
		&contextReader{ctx: ctx, reader: source},
	); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("write importexport archive: %w", err)
	}

	defer temporary.Close()
	if err := temporary.Sync(); err != nil {
		return "", err
	}

	key := newToken() + ".zip"
	if err := os.Rename(
		temporaryPath,
		filepath.Join(a.directory, key),
	); err != nil {
		return "", fmt.Errorf("finalize importexport archive: %w", err)
	}

	return key, nil
}

func (a *DiskArchive) Open(
	ctx context.Context,
	key string,
) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path, err := a.path(key)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open importexport archive: %w", err)
	}

	return file, nil
}

func (a *DiskArchive) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if strings.HasPrefix(key, ".archive-") && filepath.Base(key) == key {
		file, err := os.Open(filepath.Join(a.directory, key))
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		defer file.Close()
		// Never delete a staging file still being written by another process.
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			return nil
		}
		return os.Remove(file.Name())
	}
	path, err := a.path(key)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete importexport archive: %w", err)
	}

	return nil
}

func (a *DiskArchive) List(ctx context.Context) ([]ArchiveInfo, error) {
	if a == nil {
		return nil, fmt.Errorf("importexport jobs: archive is required")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(a.directory)
	if err != nil {
		return nil, fmt.Errorf("list importexport archives: %w", err)
	}

	archives := make([]ArchiveInfo, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".zip") && !strings.HasPrefix(entry.Name(), ".archive-")) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat importexport archive: %w", err)
		}

		archives = append(
			archives,
			ArchiveInfo{Key: entry.Name(), CreatedAt: info.ModTime()},
		)
	}

	sort.Slice(
		archives,
		func(i, j int) bool { return archives[i].CreatedAt.Before(archives[j].CreatedAt) },
	)

	return archives, nil
}

func (a *DiskArchive) path(key string) (string, error) {
	if a == nil || filepath.Base(key) != key || filepath.Ext(key) != ".zip" {
		return "", fmt.Errorf("importexport jobs: invalid archive key")
	}

	return filepath.Join(a.directory, key), nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	return r.reader.Read(data)
}
