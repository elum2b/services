package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/elum2b/services/collections/model"
	cq "github.com/elum2b/services/collections/sqlc"

	json "github.com/goccy/go-json"
	"github.com/google/uuid"
)

func (r *Repository) SetOwned(ctx context.Context, p model.SetOwnedParams) (model.Receipt, error) {
	return r.setOwned(ctx, model.SetOwnedBatchParams{
		Identity:    p.Identity,
		Source:      p.Source,
		OperationID: p.OperationID,
		Changes:     []model.OwnershipChange{p.OwnershipChange},
	}, "single", "", "")
}
func (r *Repository) SetOwnedBatch(ctx context.Context, p model.SetOwnedBatchParams) (model.Receipt, error) {
	return r.setOwned(ctx, p, "batch", "", "")
}
func (r *Repository) CorrectOwned(ctx context.Context, p model.CorrectOwnedParams) (model.Receipt, error) {
	if !validKey(p.ActorID) || strings.TrimSpace(p.Reason) == "" || len(p.Reason) > 4096 || !utf8.ValidString(p.Reason) || strings.ContainsRune(p.Reason, 0) {
		return model.Receipt{}, ErrInvalid
	}
	p.Source = "admin"
	return r.setOwned(ctx, p.SetOwnedBatchParams, "correction", p.ActorID, p.Reason)
}
func (r *Repository) setOwned(ctx context.Context, p model.SetOwnedBatchParams, method, actor, reason string) (model.Receipt, error) {
	if err := validateIdentity(p.Identity); err != nil {
		return model.Receipt{}, err
	}
	if !validKey(p.Source) || !validKey(p.OperationID) || len(p.Changes) == 0 || len(p.Changes) > model.MaxBatch {
		return model.Receipt{}, ErrInvalid
	}
	if p.Source == "admin" && method != "correction" {
		return model.Receipt{}, ErrForbidden
	}
	seen := map[model.ItemRef]bool{}
	collectionKeys, itemKeys := []string{}, []string{}
	for _, c := range p.Changes {
		if !validKey(c.CollectionKey) || !validKey(c.ItemKey) || seen[c.ItemRef] || c.ExpectedRevision != nil && *c.ExpectedRevision < 0 {
			return model.Receipt{}, ErrInvalid
		}
		seen[c.ItemRef] = true
		collectionKeys = append(collectionKeys, c.CollectionKey)
		itemKeys = append(itemKeys, c.ItemKey)
	}
	// Only semantic identity fields define the operation; profile/target attributes do not.
	raw, err := json.Marshal(struct {
		Method, Actor, Reason string
		Changes               []model.OwnershipChange
	}{method, actor, reason, p.Changes})
	if err != nil {
		return model.Receipt{}, err
	}
	fingerprint := sha256.Sum256(raw)
	i := p.Identity
	owner, _ := json.Marshal([]any{"collections:owner", i.WorkspaceID, i.AppID, i.PlatformID, i.PlatformUserID})
	var receipt model.Receipt
	err = r.transaction(ctx, r.options.QueryTimeout, false, func(ctx context.Context, q *cq.Queries, _ *sql.Tx) error {
		if err := q.LockCatalogShared(ctx, i.WorkspaceID); err != nil {
			return err
		}
		if err := q.LockOwner(ctx, string(owner)); err != nil {
			return err
		}
		existing, err := q.GetReceipt(ctx, cq.GetReceiptParams{
			WorkspaceID:    i.WorkspaceID,
			AppID:          i.AppID,
			PlatformID:     i.PlatformID,
			PlatformUserID: i.PlatformUserID,
			Source:         p.Source,
			OperationID:    p.OperationID,
		})
		if err == nil {
			if !bytes.Equal(existing.Fingerprint, fingerprint[:]) {
				return ErrConflict
			}
			return json.Unmarshal(existing.Result, &receipt)
		}
		if err != sql.ErrNoRows {
			return err
		}
		states, err := q.BatchStates(ctx, cq.BatchStatesParams{
			WorkspaceID:    i.WorkspaceID,
			AppID:          i.AppID,
			PlatformID:     i.PlatformID,
			PlatformUserID: i.PlatformUserID,
			CollectionKeys: collectionKeys,
			ItemKeys:       itemKeys,
		})
		if err != nil {
			return err
		}
		if len(states) != len(p.Changes) {
			return ErrConflict
		}
		receipt = model.Receipt{
			ID:          uuid.NewString(),
			OperationID: p.OperationID,
			Source:      p.Source,
			CreatedAt:   time.Now().UTC(),
			Results:     make([]model.ChangeResult, 0, len(states)),
		}
		stateMap := map[model.ItemRef]cq.BatchStatesRow{}
		for _, state := range states {
			stateMap[model.ItemRef{
				CollectionKey: state.CollectionKey,
				ItemKey:       state.ItemKey,
			}] = state
		}
		var changedCollections, changedItems, eventKeys, payloads []string
		var beforeValues, afterValues []bool
		var revisions []int64
		for n, c := range p.Changes {
			state := stateMap[c.ItemRef]
			if !state.Available && (c.Owned || state.Revision == 0) {
				return ErrNotFound
			}
			if c.ExpectedRevision != nil && *c.ExpectedRevision != state.Revision {
				return ErrConflict
			}
			result := model.ChangeResult{
				ItemState: model.ItemState{
					ItemRef:  c.ItemRef,
					Owned:    c.Owned,
					Revision: state.Revision,
				},
				Before:  state.Owned,
				Changed: state.Owned != c.Owned,
			}
			if state.ChangedAt.Valid {
				t := state.ChangedAt.Time
				result.ChangedAt = &t
			}
			if result.Changed {
				if state.Revision == math.MaxInt64 {
					return ErrConflict
				}
				result.Revision++
				result.ChangedAt = &receipt.CreatedAt
				event := model.ChangeEvent{
					Identity:     i,
					ReceiptID:    receipt.ID,
					OperationID:  p.OperationID,
					Source:       p.Source,
					ChangeResult: result,
				}
				payload, err := json.Marshal(event)
				if err != nil {
					return err
				}
				key := fmt.Sprintf("collections.ownership_changed:%s:%d", receipt.ID, n)
				changedCollections = append(changedCollections, c.CollectionKey)
				changedItems = append(changedItems, c.ItemKey)
				beforeValues = append(beforeValues, state.Owned)
				afterValues = append(afterValues, c.Owned)
				revisions = append(revisions, result.Revision)
				eventKeys = append(eventKeys, key)
				payloads = append(payloads, string(payload))
			}
			receipt.Results = append(receipt.Results, result)
		}
		if len(changedItems) > 0 {
			if err := q.SetOwnershipBatch(ctx, cq.SetOwnershipBatchParams{
				WorkspaceID:    i.WorkspaceID,
				AppID:          i.AppID,
				PlatformID:     i.PlatformID,
				PlatformUserID: i.PlatformUserID,
				CollectionKeys: changedCollections,
				ItemKeys:       changedItems,
				OwnedValues:    afterValues,
				Revisions:      revisions,
				ChangedAt:      receipt.CreatedAt,
			}); err != nil {
				return err
			}
			if err := q.PutChangeBatch(ctx, cq.PutChangeBatchParams{
				ReceiptID:      uuid.MustParse(receipt.ID),
				WorkspaceID:    i.WorkspaceID,
				CollectionKeys: changedCollections,
				ItemKeys:       changedItems,
				BeforeValues:   beforeValues,
				AfterValues:    afterValues,
				Revisions:      revisions,
				OccurredAt:     receipt.CreatedAt,
			}); err != nil {
				return err
			}
			if err := q.PutCallbackBatch(ctx, cq.PutCallbackBatchParams{
				WorkspaceID: i.WorkspaceID,
				RoutingKey:  fmt.Sprintf("%s:%d:%d", i.WorkspaceID, i.AppID, i.PlatformID),
				EventKeys:   eventKeys,
				Payloads:    payloads,
			}); err != nil {
				return err
			}
		}

		snapshot, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		return q.PutReceipt(ctx, cq.PutReceiptParams{
			ID:             uuid.MustParse(receipt.ID),
			WorkspaceID:    i.WorkspaceID,
			AppID:          i.AppID,
			PlatformID:     i.PlatformID,
			PlatformUserID: i.PlatformUserID,
			Source:         p.Source,
			OperationID:    p.OperationID,
			Fingerprint:    fingerprint[:],
			Result:         snapshot,
			ActorID:        actor,
			Reason:         reason,
			CreatedAt:      receipt.CreatedAt,
		})
	})
	if err != nil {
		return model.Receipt{}, err
	}
	return receipt, nil
}
