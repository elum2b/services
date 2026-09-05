package repository

import (
	"context"
	"fmt"
	"time"

	cpasqlc "github.com/elum2b/services/cpa/sqlc"
)

type importIdentity struct {
	offer         string
	app, platform int64
	user          string
}

type importCodeKey struct{ offer, code string }

func archiveIdentity(a ExportAssignment) importIdentity {
	return importIdentity{offer: a.CPAID, app: a.AppID, platform: a.PlatformID, user: a.PlatformUserID}
}

// Completed assignments are irreversible. Import replaces only unfinished
// runtime; their IDs, promises, code references and events survive every import.
func (r *Repository) importRuntime(ctx context.Context, workspaceID string, pkg ExportPackage, strategy string, preview ImportPreview) error {
	include := func(id string) bool {
		return strategy != ImportConflictSkip || !previewHasConflict(preview, "offer", id)
	}
	ids := make([]string, 0, len(pkg.Offers))
	for _, offer := range pkg.Offers {
		if include(offer.ID) {
			ids = append(ids, offer.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	existing, err := r.q.ListImportAssignments(ctx, cpasqlc.ListImportAssignmentsParams{WorkspaceID: workspaceID, Column2: ids})
	if err != nil {
		return err
	}
	completed := make(map[importIdentity]bool)
	owners := make(map[importCodeKey]importIdentity)
	preservedCodes := make(map[importCodeKey]bool)
	for _, a := range existing {
		key := importIdentity{offer: a.CpaID, app: a.AppID, platform: a.PlatformID, user: a.PlatformUserID}
		completed[key] = a.Status == cpasqlc.CpaAssignmentStatusCompleted
		if a.CodeRef.Valid {
			code := importCodeKey{offer: a.CpaID, code: a.CodeRef.String}
			owners[code] = key
			preservedCodes[code] = completed[key]
		}
	}
	incoming := make(map[importIdentity]bool)
	incomingOwners := make(map[importCodeKey]importIdentity)
	ignoredCodes := make(map[importCodeKey]bool)
	for _, a := range pkg.Assignments {
		if !include(a.CPAID) {
			continue
		}
		identity := archiveIdentity(a)
		incoming[identity] = true
		if a.CodeRef != nil {
			key := importCodeKey{offer: a.CPAID, code: *a.CodeRef}
			incomingOwners[key] = identity
			ignoredCodes[key] = completed[identity]
		}
	}
	for _, code := range pkg.Codes {
		if !include(code.CPAID) {
			continue
		}
		key := importCodeKey{offer: code.CPAID, code: code.Code}
		owner, owned := owners[key]
		if !owned {
			continue
		}
		if !incoming[owner] && !completed[owner] {
			return fmt.Errorf("cpa import cannot replace code %q for offer %q: target assignment belongs to a different identity", code.Code, code.CPAID)
		}
		if assigned, ok := incomingOwners[key]; completed[owner] && ok && assigned != owner {
			return fmt.Errorf("cpa import cannot reassign completed code %q for offer %q", code.Code, code.CPAID)
		}
	}
	if strategy == ImportConflictUpdate {
		if err := r.q.DeleteImportAssignmentEvents(ctx, cpasqlc.DeleteImportAssignmentEventsParams{WorkspaceID: workspaceID, Column2: ids}); err != nil {
			return err
		}
		if err := r.q.DeleteImportAssignments(ctx, cpasqlc.DeleteImportAssignmentsParams{WorkspaceID: workspaceID, Column2: ids}); err != nil {
			return err
		}
		if err := r.q.DeleteImportCodes(ctx, cpasqlc.DeleteImportCodesParams{WorkspaceID: workspaceID, Column2: ids}); err != nil {
			return err
		}
	}

	rows := make([][]any, 0, len(pkg.Codes))
	for _, code := range pkg.Codes {
		key := importCodeKey{offer: code.CPAID, code: code.Code}
		if !include(code.CPAID) || preservedCodes[key] {
			continue
		}
		// The incoming assignment cannot replace a completed target. Keep
		// its different personal code as a tombstone so it cannot be reissued.
		if ignoredCodes[key] {
			code.Status = "deleted"
			if now := time.Now().UTC(); now.After(code.UpdatedAt) {
				code.UpdatedAt = now
			}
			code.DeletedAt = &code.UpdatedAt
		}
		rows = append(rows, []any{workspaceID, code.CPAID, code.Code, code.Source, code.Status, code.CreatedAt, code.UpdatedAt, nullTime(code.DeletedAt)})
	}
	if err := r.execImportBulk(ctx, "cpa_code", []string{"workspace_id", "cpa_id", "code", "source", "status", "created_at", "updated_at", "deleted_at"}, rows, ""); err != nil {
		return err
	}
	codes, err := r.q.ListImportCodes(ctx, cpasqlc.ListImportCodesParams{WorkspaceID: workspaceID, Column2: ids})
	if err != nil {
		return err
	}
	codeIDs := make(map[importCodeKey]int64, len(codes))
	for _, code := range codes {
		codeIDs[importCodeKey{offer: code.CpaID, code: code.Code}] = code.ID
	}
	rows = make([][]any, 0, len(pkg.Assignments))
	for _, a := range pkg.Assignments {
		if !include(a.CPAID) || completed[archiveIdentity(a)] {
			continue
		}
		var codeID any
		if a.CodeRef != nil {
			codeID = codeIDs[importCodeKey{offer: a.CPAID, code: *a.CodeRef}]
		}
		rows = append(rows, []any{workspaceID, a.CPAID, a.AppID, a.PlatformID, a.PlatformUserID, codeID, a.Code, a.CodeMode, a.RewardsSnapshot, a.Status, a.IssuedAt, nullTime(a.CompletedAt)})
	}
	if err := r.execImportBulk(ctx, "cpa_assignment", []string{"workspace_id", "cpa_id", "app_id", "platform_id", "platform_user_id", "code_id", "code", "code_mode", "rewards_snapshot", "status", "issued_at", "completed_at"}, rows, ""); err != nil {
		return err
	}
	assignments, err := r.q.ListImportAssignments(ctx, cpasqlc.ListImportAssignmentsParams{WorkspaceID: workspaceID, Column2: ids})
	if err != nil {
		return err
	}
	assignmentIDs := make(map[importIdentity]int64, len(assignments))
	for _, a := range assignments {
		assignmentIDs[importIdentity{offer: a.CpaID, app: a.AppID, platform: a.PlatformID, user: a.PlatformUserID}] = a.ID
	}
	rows = make([][]any, 0, len(pkg.Assignments)*2)
	for _, a := range pkg.Assignments {
		if !include(a.CPAID) || completed[archiveIdentity(a)] {
			continue
		}
		for _, event := range a.Events {
			rows = append(rows, []any{workspaceID, a.CPAID, assignmentIDs[archiveIdentity(a)], event.EventType, event.OccurredAt})
		}
	}
	if err := r.execImportBulk(ctx, "cpa_assignment_event", []string{"workspace_id", "cpa_id", "assignment_id", "event_type", "occurred_at"}, rows, ""); err != nil {
		return err
	}
	if err := r.q.DeleteWorkspaceDailyStats(ctx, workspaceID); err != nil {
		return err
	}
	return r.q.RebuildWorkspaceDailyStats(ctx, workspaceID)
}
