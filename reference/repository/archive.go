package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/elum2b/services/internal/utils/importexport/jobs"
	refsqlc "github.com/elum2b/services/reference/sqlc"
)

// ExportArchiveData returns only current, non-deleted resources and links.
// Media objects are intentionally read by the archive handler, not the DB.
func (r *Repository) ExportArchiveData(
	ctx context.Context,
	workspaceID string,
) ([]ExportResource, []ExportResourceLink, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return nil, nil, err
	}

	rows, err := r.q.ListArchiveResources(ctx, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	resources := make([]ExportResource, 0, len(rows))
	for _, v := range rows {
		resources = append(resources, ExportResource{Key: v.Key, Type: v.ResourceType, Payload: v.Payload, IsActive: v.IsActive, Format: v.Format, ContentType: v.ContentType, SHA256: v.SourceSha256, MediaVersion: v.MediaVersion, Size: v.SourceSize, Width: int(v.Width), Height: int(v.Height), PlaceholderRef: v.PlaceholderRef})
	}
	rowsLinks, err := r.q.ListArchiveLinks(ctx, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	links := make([]ExportResourceLink, 0, len(rowsLinks))
	for _, v := range rowsLinks {
		links = append(links, ExportResourceLink{ItemKey: v.ItemKey, ResourceKey: v.ResourceKey, Position: v.Position})
	}
	return resources, links, nil
}

// ImportArchiveWithMedia keeps package data, media bookkeeping, uploads, and
// links in one workspace-locked transaction. upload fills opaque object refs.
func (r *Repository) ImportArchiveWithMedia(
	ctx context.Context,
	workspaceID string,
	req ImportRequest,
	resources []*Resource,
	links []ExportResourceLink,
	timeout time.Duration,
	upload func(context.Context, []*Resource) error,
) (result ImportResult, err error) {
	return r.importArchiveWithMedia(
		ctx,
		workspaceID,
		0,
		req,
		resources,
		links,
		timeout,
		upload,
	)
}

// ImportArchiveWithMediaJob applies an archive-job media import at most once
// after its transaction commits.
func (r *Repository) ImportArchiveWithMediaJob(
	ctx context.Context,
	workspaceID string,
	jobID int64,
	req ImportRequest,
	resources []*Resource,
	links []ExportResourceLink,
	timeout time.Duration,
	upload func(context.Context, []*Resource) error,
) (result ImportResult, err error) {
	return r.importArchiveWithMedia(
		ctx,
		workspaceID,
		jobID,
		req,
		resources,
		links,
		timeout,
		upload,
	)
}

func (r *Repository) importArchiveWithMedia(
	ctx context.Context,
	workspaceID string,
	jobID int64,
	req ImportRequest,
	resources []*Resource,
	links []ExportResourceLink,
	timeout time.Duration,
	upload func(context.Context, []*Resource) error,
) (result ImportResult, err error) {
	if err = requireWorkspace(workspaceID); err != nil {
		return result, err
	}

	if err = validateExportPackage(req.Package); err != nil {
		return result, err
	}

	strategy := req.ConflictStrategy
	if strategy == "" {
		strategy = ImportConflictUpdate
	}

	if strategy != ImportConflictFail && strategy != ImportConflictSkip &&
		strategy != ImportConflictUpdate {
		return result, fmt.Errorf(
			"unsupported import conflict strategy: %s",
			strategy,
		)
	}

	if upload == nil {
		return result, fmt.Errorf("archive media uploader is required")
	}

	err = r.WithTxTimeout(ctx, timeout, func(ctx context.Context, tx *Repository) error {
		if err := tx.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}

		if jobID != 0 {
			applied, err := jobs.ClaimImportReceipt(
				ctx,
				tx.executor,
				jobID,
				"reference",
				workspaceID,
			)
			if err != nil || !applied {
				return err
			}
		}

		preview, err := tx.PreviewImport(ctx, workspaceID, req.Package)
		if err != nil {
			return err
		}

		if strategy == ImportConflictFail && len(preview.Conflicts) > 0 {
			return fmt.Errorf(
				"import conflicts found: %d",
				len(preview.Conflicts),
			)
		}

		if err := tx.importBulk(
			ctx,
			workspaceID,
			req.Package,
			strategy,
			preview,
			&result,
		); err != nil {
			return err
		}

		existingKeys, err := tx.q.ListImportResourceKeys(ctx, workspaceID)
		if err != nil {
			return err
		}
		existing := make(map[string]bool, len(existingKeys))
		for _, key := range existingKeys {
			existing[key] = true
		}
		accepted := make([]*Resource, 0, len(resources))
		for _, value := range resources {
			exists := existing[value.Key]
			if exists && strategy == ImportConflictFail {
				return fmt.Errorf("import conflict: resource %s", value.Key)
			}
			if exists && strategy == ImportConflictSkip {
				continue
			}
			if exists {
				if err := tx.q.ResourceMediaVersionRetireActive(ctx, refsqlc.ResourceMediaVersionRetireActiveParams{WorkspaceID: workspaceID, ResourceKey: value.Key}); err != nil {
					return err
				}
			}
			if err := tx.q.ImportResource(ctx, refsqlc.ImportResourceParams{WorkspaceID: workspaceID, Key: value.Key, ResourceType: value.Type, Payload: value.Payload, IsActive: value.IsActive, Format: value.Format, ContentType: value.ContentType, SourceSize: value.Size, SourceSha256: value.SHA256, MediaVersion: value.MediaVersion, Width: int32(value.Width), Height: int32(value.Height)}); err != nil {
				return err
			}
			if err := tx.q.ResourceMediaVersionCreate(ctx, refsqlc.ResourceMediaVersionCreateParams{WorkspaceID: workspaceID, ResourceKey: value.Key, MediaVersion: value.MediaVersion}); err != nil {
				return err
			}
			accepted = append(accepted, value)
		}
		if err := upload(ctx, accepted); err != nil {
			return err
		}

		for _, value := range accepted {
			if err := tx.updateResourceObjectRefs(ctx, *value); err != nil {
				return err
			}
		}

		acceptedItems := make(map[string]bool, len(req.Package.Items))
		conflicts := importConflictSet(preview)
		keys := make([]string, 0, len(req.Package.Items))
		for _, item := range req.Package.Items {
			if strategy == ImportConflictSkip && hasImportConflict(conflicts, "item", item.Key) {
				continue
			}
			acceptedItems[item.Key] = true
			keys = append(keys, item.Key)
		}
		if strategy == ImportConflictUpdate {
			if err := tx.q.DeleteImportedItemLinks(ctx, refsqlc.DeleteImportedItemLinksParams{WorkspaceID: workspaceID, Column2: keys}); err != nil {
				return err
			}
		}

		for _, link := range links {
			if !acceptedItems[link.ItemKey] {
				continue
			}
			if _, err := tx.q.ResourceAttach(ctx, refsqlc.ResourceAttachParams{WorkspaceID: workspaceID, ItemKey: link.ItemKey, ResourceKey: link.ResourceKey, Position: link.Position}); err != nil {
				return err
			}
		}

		return nil
	})
	if err == nil {
		methods := append([]string{}, referenceItemMutationCacheMethods...)

		methods = append(methods, referenceLocalizationMutationCacheMethods...)
		methods = append(
			methods,
			"resource_get",
			"resource_list",
			"resource_item_list",
			referenceCacheGet,
			referenceCacheResolve,
			referenceCacheList,
		)
		err = r.bumpReferenceCacheVersions(workspaceID, methods...)
	}

	return result, err
}

// ArchiveMediaVersionDurable reports whether a planned version survived an
// ambiguous commit result and must therefore not be compensated.
func (r *Repository) ArchiveMediaVersionDurable(ctx context.Context, workspaceID, key, version string) (bool, error) {
	var durable bool
	// A lost COMMIT response can precede server-side transaction completion.
	// Wait for its workspace lock before interpreting absence as safe to delete.
	err := r.withWorkspaceMutation(ctx, workspaceID, func(ctx context.Context, tx *Repository) error {
		var err error
		durable, err = tx.q.ResourceMediaVersionDurable(ctx, refsqlc.ResourceMediaVersionDurableParams{WorkspaceID: workspaceID, ResourceKey: key, MediaVersion: version})
		return err
	})
	return durable, err
}

// ReconcileArchiveImport refreshes all read scopes without needing the source ZIP.
func (r *Repository) ReconcileArchiveImport(workspaceID string) error {
	if err := requireWorkspace(workspaceID); err != nil {
		return err
	}
	methods := append([]string{}, referenceItemMutationCacheMethods...)
	methods = append(methods, referenceLocalizationMutationCacheMethods...)
	methods = append(methods, "resource_get", "resource_list", "resource_item_list")
	return r.bumpReferenceCacheVersions(workspaceID, methods...)
}
