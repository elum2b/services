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

var originalNames = map[string]string{
	"jpeg":   "image.jpeg",
	"png":    "image.png",
	"webp":   "image.webp",
	"gif":    "image.gif",
	"lottie": "lottie.json",
	"tgs":    "animation.tgs",
	"svg":    "image.svg",
}

// OriginalName returns the semantic object name for a processed media format.
func OriginalName(format string) (string, bool) {
	name, ok := originalNames[format]
	return name, ok
}

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

// Preview is a WebP rendition.
type Preview struct {
	Size int
	File File
}

// Files is the full set replaced for a resource in one operation.
type Files struct {
	OriginalName string
	Original     File
	Previews     []Preview
	Placeholder  File
	NoPreviews   bool
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
	Replace(context.Context, string, string, string, Files) (Objects, error)
	Read(context.Context, string) ([]byte, error)
	ReadVersion(
		context.Context,
		string,
		string,
		string,
		string,
		int,
	) ([]byte, error)
	DeleteVersion(context.Context, string, string, string) error
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

func objectPrefix(workspaceID, resourceKey, version string) (string, error) {
	if strings.TrimSpace(workspaceID) == "" ||
		strings.TrimSpace(resourceKey) == "" ||
		!validVersion(version) ||
		strings.Contains(workspaceID, "/") {
		return "", fmt.Errorf(
			"%w: workspace and resource key are required",
			ErrFilesInvalid,
		)
	}

	digest := sha256.Sum256([]byte(resourceKey))

	return workspaceID + "/" + fmt.Sprintf("%x", digest[:]) + "/" + version, nil
}

func validVersion(value string) bool {
	if len(value) != 8 {
		return false
	}

	for _, char := range value {
		if char < 'A' || char > 'Z' && char < 'a' || char > 'z' {
			return false
		}
	}

	return true
}

func versionReference(
	workspaceID, resourceKey, version, originalName string,
	size int,
) (string, error) {
	prefix, err := objectPrefix(workspaceID, resourceKey, version)
	if err != nil {
		return "", err
	}

	if size == 0 {
		if !validOriginalName(originalName) {
			return "", ErrFilesInvalid
		}

		return prefix + "/" + originalName, nil
	}

	for _, allowed := range requiredPreviewSizes {
		if size == allowed {
			return fmt.Sprintf("%s/preview-%d.webp", prefix, size), nil
		}
	}

	return "", ErrFilesInvalid
}

func validateFiles(files Files) error {
	if !validOriginalName(files.OriginalName) ||
		len(files.Original.Data) == 0 ||
		files.Original.ContentType == "" ||
		len(files.Placeholder.Data) == 0 ||
		files.Placeholder.ContentType != "image/svg+xml" {
		return ErrFilesInvalid
	}

	seen := make(map[int]struct{}, len(files.Previews))
	for _, preview := range files.Previews {
		if preview.Size <= 0 || len(preview.File.Data) == 0 ||
			preview.File.ContentType != "image/webp" {
			return ErrFilesInvalid
		}

		if _, exists := seen[preview.Size]; exists {
			return ErrFilesInvalid
		}

		seen[preview.Size] = struct{}{}
	}

	if len(seen) == 0 {
		if files.NoPreviews {
			return nil
		}

		return ErrFilesInvalid
	}

	if files.NoPreviews {
		return ErrFilesInvalid
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

func validOriginalName(name string) bool {
	for _, value := range originalNames {
		if name == value {
			return true
		}
	}

	return false
}
