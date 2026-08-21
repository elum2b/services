// Package storage persists processed Reference media outside PostgreSQL.
package storage

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrConfigInvalid = errors.New(
		"invalid reference resource storage configuration",
	)
	ErrFilesInvalid = errors.New("invalid resource media files")
)

var requiredPreviewSizes = [...]int{61, 128, 256, 512}

// Config uses S3 when Bucket is set. Otherwise media is kept under Directory,
// or under <binary-dir>/reference when Directory is empty.
type Config struct {
	Directory    string
	Endpoint     string
	Bucket       string
	AccessKey    string
	SecretKey    string
	SessionToken string
	Region       string
	Secure       bool
	UsePathStyle bool
}

// File is one processed media object.
type File struct {
	Data        []byte
	ContentType string
}

// Preview is a PNG rendition.
type Preview struct {
	Size int
	File File
}

// Files is the full set replaced for a resource in one operation.
type Files struct {
	Original    File
	Previews    []Preview
	Placeholder File
}

// Objects contains opaque references persisted by Reference, never paths for
// an HTTP handler to construct or expose directly.
type Objects struct {
	Original    string
	Previews    map[int]string
	Placeholder string
}

// Store replaces every derived object of one resource.
type Store interface {
	Replace(context.Context, string, string, Files) (Objects, error)
}

// New selects S3-compatible storage when Bucket is configured, otherwise a
// local filesystem store.
func New(config Config) (Store, error) {
	if config.Bucket != "" {
		return newS3(config)
	}

	if config.Endpoint != "" || config.AccessKey != "" ||
		config.SecretKey != "" ||
		config.SessionToken != "" ||
		config.Region != "" {
		return nil, ErrConfigInvalid
	}

	return newDisk(config.Directory)
}

func objectPrefix(workspaceID, resourceKey string) (string, error) {
	if strings.TrimSpace(workspaceID) == "" ||
		strings.TrimSpace(resourceKey) == "" ||
		strings.Contains(workspaceID, "/") {
		return "", fmt.Errorf(
			"%w: workspace and resource key are required",
			ErrFilesInvalid,
		)
	}

	digest := sha256.Sum256([]byte(resourceKey))

	return workspaceID + "/" + fmt.Sprintf("%x", digest[:]), nil
}

func validateFiles(files Files) error {
	if len(files.Original.Data) == 0 || files.Original.ContentType == "" ||
		len(files.Placeholder.Data) == 0 ||
		files.Placeholder.ContentType != "image/svg+xml" {
		return ErrFilesInvalid
	}

	seen := make(map[int]struct{}, len(files.Previews))
	for _, preview := range files.Previews {
		if preview.Size <= 0 || len(preview.File.Data) == 0 ||
			preview.File.ContentType != "image/png" {
			return ErrFilesInvalid
		}

		if _, exists := seen[preview.Size]; exists {
			return ErrFilesInvalid
		}

		seen[preview.Size] = struct{}{}
	}

	if len(seen) != len(requiredPreviewSizes) {
		return ErrFilesInvalid
	}

	for _, size := range requiredPreviewSizes {
		if _, exists := seen[size]; !exists {
			return ErrFilesInvalid
		}
	}

	return nil
}
