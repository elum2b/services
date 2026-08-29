package admin

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/reference/repository"
	"github.com/elum2b/services/reference/storage"
)

const archiveManifestName = "manifest.json"
const maxArchiveMediaFile = 256 << 20

var archiveResourceKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)

// ExportZIP writes a portable Reference archive. The manifest always contains
// the existing JSON export package; resource records and media are optional.
func (a *Admin) ExportZIP(ctx context.Context, workspaceID string, req ArchiveExportRequest, dst io.Writer) error {
	if a == nil || a.repository == nil || dst == nil {
		return ErrRepositoryNotConfigured
	}
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	pkg, err := a.repository.Export(mergedCtx, workspaceID, req.ExportRequest)
	if err != nil {
		return err
	}
	manifest := repository.ArchivePackage{Package: pkg}
	if req.IncludeMedia {
		if a.store == nil {
			return fmt.Errorf("reference resource storage is not configured")
		}
		manifest.Resources, manifest.Links, err = a.repository.ExportArchiveData(mergedCtx, workspaceID)
		if err != nil {
			return err
		}
	}

	writer := zip.NewWriter(dst)
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := writeArchiveFile(writer, archiveManifestName, data); err != nil {
		return err
	}
	if !req.IncludeMedia {
		return writer.Close()
	}
	for _, resource := range manifest.Resources {
		base := "media/" + resource.Key + "/"
		originalName, ok := storage.OriginalName(resource.Format)
		if !ok {
			return fmt.Errorf("unsupported archive resource format %q", resource.Format)
		}
		if err := writeResourceObject(mergedCtx, writer, a.store, workspaceID, resource, base, originalName, 0); err != nil {
			return err
		}
		if err := writeResourceObject(mergedCtx, writer, a.store, workspaceID, resource, base, "placeholder.svg", -1); err != nil {
			return err
		}
		if resource.Format == "svg" {
			continue
		}
		for _, size := range []int{61, 128, 256, 512} {
			if err := writeResourceObject(mergedCtx, writer, a.store, workspaceID, resource, base, fmt.Sprintf("preview-%d.webp", size), size); err != nil {
				return err
			}
		}
	}
	return writer.Close()
}

func writeResourceObject(ctx context.Context, writer *zip.Writer, store storage.Store, workspaceID string, resource repository.ExportResource, base, name string, size int) error {
	var data []byte
	var err error
	if size == -1 {
		data, err = store.Read(ctx, resource.PlaceholderRef)
	} else {
		data, err = store.ReadVersion(ctx, workspaceID, resource.Key, resource.MediaVersion, name, size)
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	return writeArchiveFile(writer, base+name, data)
}

func writeArchiveFile(writer *zip.Writer, name string, data []byte) error {
	entry, err := writer.Create(name)
	if err != nil {
		return err
	}
	_, err = entry.Write(data)
	return err
}

// ImportZIP restores an archive produced by ExportZIP. IncludeMedia=false
// deliberately ignores every resource record, media object, and link.
func (a *Admin) ImportZIP(ctx context.Context, workspaceID string, archive *zip.Reader, req ArchiveImportRequest) (ImportResult, error) {
	if a == nil || a.repository == nil || archive == nil {
		return ImportResult{}, ErrRepositoryNotConfigured
	}
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	files := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		if file.Name != path.Clean(file.Name) || strings.HasPrefix(file.Name, "/") || files[file.Name] != nil {
			return ImportResult{}, fmt.Errorf("invalid ZIP entry: %q", file.Name)
		}
		files[file.Name] = file
	}
	manifestFile := files[archiveManifestName]
	if manifestFile == nil {
		return ImportResult{}, fmt.Errorf("ZIP manifest.json is required")
	}
	data, err := readArchiveFile(manifestFile, 16<<20)
	if err != nil {
		return ImportResult{}, err
	}
	var manifest repository.ArchivePackage
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ImportResult{}, fmt.Errorf("decode manifest: %w", err)
	}
	if !req.IncludeMedia {
		return a.repository.Import(mergedCtx, workspaceID, repository.ImportRequest{Package: manifest.Package, ConflictStrategy: req.ConflictStrategy})
	}
	if a.store == nil {
		return ImportResult{}, fmt.Errorf("reference resource storage is not configured")
	}
	if err := validateArchiveResources(manifest.Resources, manifest.Links); err != nil {
		return ImportResult{}, err
	}
	if normalizedStrategy(req.ConflictStrategy) == repository.ImportConflictFail {
		conflicts, err := a.repository.ArchiveResourceConflicts(mergedCtx, workspaceID, manifest.Resources)
		if err != nil {
			return ImportResult{}, err
		}
		if len(conflicts) > 0 {
			return ImportResult{}, fmt.Errorf("resource import conflicts found: %d", len(conflicts))
		}
	}
	mediaFiles := make([]storage.Files, len(manifest.Resources))
	for index, value := range manifest.Resources {
		mediaFiles[index], err = archiveResourceFiles(files, value)
		if err != nil {
			return ImportResult{}, err
		}
	}
	result, err := a.repository.Import(mergedCtx, workspaceID, repository.ImportRequest{Package: manifest.Package, ConflictStrategy: req.ConflictStrategy})
	if err != nil {
		return ImportResult{}, err
	}
	resources := make([]repository.Resource, 0, len(manifest.Resources))
	for index, value := range manifest.Resources {
		objects, err := a.store.Replace(mergedCtx, workspaceID, value.Key, value.MediaVersion, mediaFiles[index])
		if err != nil {
			return ImportResult{}, fmt.Errorf("restore resource %s: %w", value.Key, err)
		}
		resources = append(resources, repository.Resource{WorkspaceID: workspaceID, Key: value.Key, Type: value.Type, Payload: value.Payload, IsActive: value.IsActive, Format: value.Format, ContentType: value.ContentType, SHA256: value.SHA256, MediaVersion: value.MediaVersion, Size: value.Size, Width: value.Width, Height: value.Height, OriginalRef: objects.Original, Preview61Ref: objects.Previews[61], Preview128Ref: objects.Previews[128], Preview256Ref: objects.Previews[256], Preview512Ref: objects.Previews[512], PlaceholderRef: objects.Placeholder})
	}
	if err := a.repository.ImportArchiveResources(mergedCtx, workspaceID, resources, manifest.Links, normalizedStrategy(req.ConflictStrategy)); err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

func archiveResourceFiles(entries map[string]*zip.File, resource repository.ExportResource) (storage.Files, error) {
	base := "media/" + resource.Key + "/"
	originalName, ok := storage.OriginalName(resource.Format)
	if !ok {
		return storage.Files{}, fmt.Errorf("unsupported archive resource format %q", resource.Format)
	}
	original, err := readArchiveFile(entries[base+originalName], resource.Size)
	if err != nil {
		return storage.Files{}, err
	}
	digest := sha256.Sum256(original)
	if fmt.Sprintf("%x", digest) != resource.SHA256 {
		return storage.Files{}, fmt.Errorf("resource %s original checksum mismatch", resource.Key)
	}
	placeholder, err := readArchiveFile(entries[base+"placeholder.svg"], 16<<20)
	if err != nil {
		return storage.Files{}, err
	}
	files := storage.Files{OriginalName: originalName, Original: storage.File{Data: original, ContentType: resource.ContentType}, Placeholder: storage.File{Data: placeholder, ContentType: "image/svg+xml"}, NoPreviews: resource.Format == "svg"}
	if files.NoPreviews {
		return files, nil
	}
	for _, size := range []int{61, 128, 256, 512} {
		data, err := readArchiveFile(entries[base+fmt.Sprintf("preview-%d.webp", size)], 64<<20)
		if err != nil {
			return storage.Files{}, err
		}
		files.Previews = append(files.Previews, storage.Preview{Size: size, File: storage.File{Data: data, ContentType: "image/webp"}})
	}
	return files, nil
}

func readArchiveFile(file *zip.File, limit int64) ([]byte, error) {
	if file == nil {
		return nil, fmt.Errorf("required ZIP entry is missing")
	}
	if file.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("ZIP entry %q exceeds limit", file.Name)
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("ZIP entry %q exceeds limit", file.Name)
	}
	return data, nil
}

func validateArchiveResources(resources []repository.ExportResource, links []repository.ExportResourceLink) error {
	seen := make(map[string]bool, len(resources))
	for _, resource := range resources {
		if _, ok := storage.OriginalName(resource.Format); !ok || !archiveResourceKeyPattern.MatchString(resource.Key) || seen[resource.Key] || !json.Valid(resource.Payload) || resource.Type == "" || resource.ContentType == "" || resource.Size <= 0 || resource.Size > maxArchiveMediaFile || resource.Width <= 0 || resource.Height <= 0 || len(resource.SHA256) != 64 || !regexp.MustCompile(`^[A-Za-z]{8}$`).MatchString(resource.MediaVersion) {
			return fmt.Errorf("invalid archive resource %q", resource.Key)
		}
		seen[resource.Key] = true
	}
	for _, link := range links {
		if !archiveResourceKeyPattern.MatchString(link.ItemKey) || !seen[link.ResourceKey] {
			return fmt.Errorf("invalid archive resource link")
		}
	}
	return nil
}

func normalizedStrategy(value string) string {
	if value == "" {
		return repository.ImportConflictFail
	}
	return value
}
