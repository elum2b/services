package jobs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func stagingDirectory() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("elum-importexport-staging-%d", os.Geteuid()))
}

func prepareStagingDirectory() error {
	if err := os.MkdirAll(stagingDirectory(), 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(stagingDirectory())
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 || !ok || int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("archive staging directory must be private and owned by this user")
	}
	return nil
}

// NewStagingFile creates a private, locked spool cleaned by the archive sweeper after a crash.
// Callers must close and remove the file after use.
func NewStagingFile(pattern string) (*os.File, error) {
	if err := prepareStagingDirectory(); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(stagingDirectory(), pattern)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, err
	}
	return f, nil
}

// OS file locks disappear on process exit. Old files with a live writer or
// reader stay protected across nodes sharing the staging directory.
func sweepStaging(ctx context.Context, olderThan time.Time, limit int) error {
	if err := prepareStagingDirectory(); err != nil {
		return err
	}
	directory, err := os.Open(stagingDirectory())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer directory.Close()
	deleted := 0
	for deleted < limit {
		entries, err := directory.ReadDir(100)
		if err != nil && err != io.EOF {
			return err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			if deleted >= limit {
				return nil
			}
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), "importexport-") {
				continue
			}
			info, err := entry.Info()
			if err != nil || info.ModTime().After(olderThan) {
				continue
			}
			file, err := os.Open(filepath.Join(stagingDirectory(), entry.Name()))
			if err != nil {
				continue
			}
			if syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
				if os.Remove(file.Name()) == nil {
					deleted++
				}
			}
			_ = file.Close()
		}
		if err == io.EOF {
			return nil
		}
	}
	return nil
}
