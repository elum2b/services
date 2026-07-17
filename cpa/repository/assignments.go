package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	json "github.com/goccy/go-json"
	"github.com/jackc/pgx/v5/pgconn"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/cpa/model"
	cpasqlc "github.com/elum2b/services/cpa/sqlc"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

type UserScope struct {
	WorkspaceID    string
	CPAID          string
	AppID          int64
	PlatformID     int64
	PlatformUserID string
}

type IssueResult struct {
	Assignment    Assignment
	Rewards       []Reward
	AlreadyIssued bool
}

type CompleteResult struct {
	Assignment  Assignment
	Rewards     []Reward
	AlreadyDone bool
}

func (r *Repository) GetAssignment(ctx context.Context, scope UserScope) (Assignment, error) {
	if err := requireUserScope(scope, true); err != nil {
		return Assignment{}, err
	}
	row, err := r.q.GetAssignment(ctx, assignmentParams(scope))
	if err != nil {
		return Assignment{}, err
	}
	return mapAssignment(row)
}

func (r *Repository) FindAssignment(ctx context.Context, scope UserScope) (*Assignment, error) {
	value, err := r.GetAssignment(ctx, scope)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (r *Repository) ListUserAssignments(ctx context.Context, scope UserScope) ([]Assignment, error) {
	if err := requireUserScope(scope, false); err != nil {
		return nil, err
	}
	rows, err := r.q.ListUserAssignments(ctx, cpasqlc.ListUserAssignmentsParams{
		WorkspaceID:    scope.WorkspaceID,
		AppID:          scope.AppID,
		PlatformID:     scope.PlatformID,
		PlatformUserID: scope.PlatformUserID,
	})
	if err != nil {
		return nil, err
	}
	result := make([]Assignment, 0, len(rows))
	for _, row := range rows {
		assignment, err := mapAssignment(row)
		if err != nil {
			return nil, err
		}
		result = append(result, assignment)
	}
	return result, nil
}

func (r *Repository) ListAssignments(ctx context.Context, workspaceID, cpaID string, status model.AssignmentStatus, limit, offset int32) ([]Assignment, error) {
	if err := requireScope(workspaceID, cpaID); err != nil {
		return nil, err
	}

	limit, offset = normalizePage(limit, offset)
	rows, err := r.q.AdminListAssignments(ctx, cpasqlc.AdminListAssignmentsParams{
		WorkspaceID: workspaceID,
		CpaID:       cpaID,
		Column3:     string(status),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		return nil, err
	}
	result := make([]Assignment, 0, len(rows))
	for _, row := range rows {
		assignment, err := mapAssignment(row)
		if err != nil {
			return nil, err
		}
		result = append(result, assignment)
	}
	return result, nil
}

func (r *Repository) ListCodes(ctx context.Context, workspaceID, cpaID string, status model.CodeStatus, limit, offset int32) ([]Code, error) {
	if err := requireScope(workspaceID, cpaID); err != nil {
		return nil, err
	}

	limit, offset = normalizePage(limit, offset)
	rows, err := r.q.AdminListCodes(ctx, cpasqlc.AdminListCodesParams{
		WorkspaceID: workspaceID,
		CpaID:       cpaID,
		Column3:     string(status),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		return nil, err
	}
	result := make([]Code, 0, len(rows))
	for _, row := range rows {
		result = append(result, Code{
			ID:          uint64(row.ID),
			WorkspaceID: row.WorkspaceID,
			CPAID:       row.CpaID,
			Code:        row.Code,
			Source:      string(row.Source),
			Status:      model.CodeStatus(row.Status),
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
			DeletedAt:   sqlwrap.NullTimePtr(row.DeletedAt),
		})
	}
	return result, nil
}

func (r *Repository) ListAssignmentEvents(ctx context.Context, workspaceID, cpaID string, eventType model.AssignmentEventType, limit, offset int32) ([]AssignmentEvent, error) {
	if err := requireScope(workspaceID, cpaID); err != nil {
		return nil, err
	}

	limit, offset = normalizePage(limit, offset)
	rows, err := r.q.AdminListAssignmentEvents(ctx, cpasqlc.AdminListAssignmentEventsParams{
		WorkspaceID: workspaceID,
		CpaID:       cpaID,
		Column3:     string(eventType),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		return nil, err
	}
	result := make([]AssignmentEvent, 0, len(rows))
	for _, row := range rows {
		result = append(result, AssignmentEvent{
			ID:           uint64(row.ID),
			WorkspaceID:  row.WorkspaceID,
			CPAID:        row.CpaID,
			AssignmentID: uint64(row.AssignmentID),
			EventType:    model.AssignmentEventType(row.EventType),
			OccurredAt:   row.OccurredAt,
		})
	}
	return result, nil
}

func (r *Repository) Issue(ctx context.Context, scope UserScope) (IssueResult, error) {
	if err := requireUserScope(scope, true); err != nil {
		return IssueResult{}, err
	}

	existing, err := r.q.GetAssignment(ctx, assignmentParams(scope))
	if err == nil {
		assignment, err := mapAssignment(existing)
		if err != nil {
			return IssueResult{}, err
		}

		return IssueResult{
			Assignment:    assignment,
			Rewards:       assignment.Rewards,
			AlreadyIssued: true,
		}, nil
	}
	if !isNoRows(err) {
		return IssueResult{}, err
	}

	var result IssueResult
	err = r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceCatalogRead(ctx, scope.WorkspaceID); err != nil {
			return err
		}
		if err := txRepo.lockIssueIdentity(ctx, scope); err != nil {
			return err
		}

		existing, err := txRepo.q.GetAssignment(ctx, assignmentParams(scope))
		if err == nil {
			result.Assignment, err = mapAssignment(existing)
			if err != nil {
				return err
			}
			result.Rewards = result.Assignment.Rewards
			result.AlreadyIssued = true
			return nil
		}
		if !isNoRows(err) {
			return err
		}

		offer, err := txRepo.q.GetActiveOffer(ctx, cpasqlc.GetActiveOfferParams{
			WorkspaceID: scope.WorkspaceID,
			ID:          scope.CPAID,
		})
		if err != nil {
			return err
		}

		rewards, err := txRepo.listRewardsDirect(ctx, scope.WorkspaceID, scope.CPAID)
		if err != nil {
			return err
		}
		rewardsSnapshot, err := encodeRewardsSnapshot(rewards)
		if err != nil {
			return err
		}

		code, codeID, err := txRepo.allocateCode(ctx, offer)
		if err != nil {
			return err
		}
		id, err := txRepo.q.CreateAssignment(ctx, cpasqlc.CreateAssignmentParams{
			WorkspaceID:    scope.WorkspaceID,
			CpaID:          scope.CPAID,
			AppID:          scope.AppID,
			PlatformID:     scope.PlatformID,
			PlatformUserID: scope.PlatformUserID,
			CodeID: sqlwrap.NullFromPtr(codeID, func(v uint64) sql.NullInt64 {
				return sql.NullInt64{Int64: int64(v), Valid: true}
			}),
			Code:            code,
			CodeMode:        offer.CodeMode,
			RewardsSnapshot: rewardsSnapshot,
		})
		if err != nil {
			return err
		}
		if codeID != nil {
			affected, err := txRepo.q.MarkCodeIssued(ctx, int64(*codeID))
			if err != nil {
				return err
			}
			if affected != 1 {
				return ErrNoCodesAvailable
			}
		}
		row, err := txRepo.q.GetAssignmentByID(ctx, cpasqlc.GetAssignmentByIDParams{
			WorkspaceID: scope.WorkspaceID,
			ID:          id,
		})
		if err != nil {
			return err
		}
		result.Assignment, err = mapAssignment(row)
		if err != nil {
			return err
		}
		result.Rewards = result.Assignment.Rewards
		return txRepo.recordEvent(ctx, result.Assignment, result.Rewards, model.AssignmentEventTypeIssued)
	})
	return result, err
}

func (r *Repository) Complete(ctx context.Context, scope UserScope) (CompleteResult, error) {
	if err := requireUserScope(scope, true); err != nil {
		return CompleteResult{}, err
	}
	existing, err := r.q.GetAssignment(ctx, assignmentParams(scope))
	if err == nil && existing.Status == cpasqlc.CpaAssignmentStatusCompleted {
		assignment, err := mapAssignment(existing)
		if err != nil {
			return CompleteResult{}, err
		}
		return CompleteResult{
			Assignment:  assignment,
			Rewards:     assignment.Rewards,
			AlreadyDone: true,
		}, nil
	}
	if err != nil && !isNoRows(err) {
		return CompleteResult{}, err
	}
	var result CompleteResult
	err = r.WithTx(ctx, func(txRepo *Repository) error {
		row, err := txRepo.q.GetAssignmentForUpdate(ctx, assignmentForUpdateParams(scope))
		if err != nil {
			return err
		}
		result.Assignment, err = mapAssignment(row)
		if err != nil {
			return err
		}
		result.Rewards = result.Assignment.Rewards
		if result.Assignment.Status == model.AssignmentStatusCompleted {
			result.AlreadyDone = true
			return nil
		}
		affected, err := txRepo.q.CompleteAssignment(ctx, cpasqlc.CompleteAssignmentParams{
			WorkspaceID: scope.WorkspaceID,
			ID:          int64(result.Assignment.ID),
		})
		if err != nil {
			return err
		}
		if affected != 1 {
			return errors.New("cpa: assignment completion conflict")
		}
		if result.Assignment.CodeID != nil {
			if _, err := txRepo.q.MarkCodeCompleted(ctx, int64(*result.Assignment.CodeID)); err != nil {
				return err
			}
		}
		now := time.Now()
		result.Assignment.Status = model.AssignmentStatusCompleted
		result.Assignment.CompletedAt = &now
		return txRepo.recordEvent(ctx, result.Assignment, result.Rewards, model.AssignmentEventTypeCompleted)
	})
	return result, err
}

func requireUserScope(scope UserScope, requireOffer bool) error {
	if err := (services.Identity{
		WorkspaceID:    scope.WorkspaceID,
		AppID:          scope.AppID,
		PlatformID:     scope.PlatformID,
		PlatformUserID: scope.PlatformUserID,
	}).Validate(); err != nil {
		return err
	}
	if err := validateStoredString("platform_user_id", scope.PlatformUserID, 255); err != nil {
		return err
	}
	if requireOffer && strings.TrimSpace(scope.CPAID) == "" {
		return ErrOfferRequired
	}
	if requireOffer {
		return validateStoredString("cpa_id", scope.CPAID, maxOfferIDLength)
	}
	return nil
}

func (r *Repository) AddCodes(ctx context.Context, workspaceID, cpaID string, codes []string) (int, error) {
	if err := requireScope(workspaceID, cpaID); err != nil {
		return 0, err
	}
	if len(codes) == 0 {
		return 0, ErrCodeRequired
	}
	for _, code := range codes {
		if strings.TrimSpace(code) == "" {
			return 0, ErrCodeRequired
		}
		if err := validateStoredString("code", code, maxCodeLength); err != nil {
			return 0, err
		}
	}

	added := 0
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}

		offer, err := txRepo.q.AdminGetOffer(ctx, cpasqlc.AdminGetOfferParams{
			WorkspaceID: workspaceID,
			ID:          cpaID,
		})
		if err != nil {
			return err
		}
		if offer.CodeMode != cpasqlc.CpaCodeModePersonalCode ||
			!offer.CodeSource.Valid ||
			offer.CodeSource.CpaCodeSource != cpasqlc.CpaCodeSourcePool {
			return ErrCodeUploadMode
		}

		for _, code := range codes {
			affected, err := txRepo.q.AdminAddCode(ctx, cpasqlc.AdminAddCodeParams{
				WorkspaceID: workspaceID,
				CpaID:       cpaID,
				Code:        code,
				Source:      cpasqlc.CpaCodeSourcePool,
			})
			if err != nil {
				return err
			}
			added += int(affected)
		}
		return nil
	})
	return added, err
}

func (r *Repository) DeleteAvailableCodes(ctx context.Context, workspaceID, cpaID string) (int64, error) {
	if err := requireScope(workspaceID, cpaID); err != nil {
		return 0, err
	}

	var rows int64
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}
		var err error
		rows, err = txRepo.q.AdminDeleteAvailableCodes(ctx, cpasqlc.AdminDeleteAvailableCodesParams{
			WorkspaceID: workspaceID,
			CpaID:       cpaID,
		})
		return err
	})
	return rows, err
}

func (r *Repository) DeleteIssuedCodes(ctx context.Context, workspaceID, cpaID string) (int64, error) {
	if err := requireScope(workspaceID, cpaID); err != nil {
		return 0, err
	}

	var rows int64
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}
		var err error
		rows, err = txRepo.q.AdminDeleteIssuedCodes(ctx, cpasqlc.AdminDeleteIssuedCodesParams{
			WorkspaceID: workspaceID,
			CpaID:       cpaID,
		})
		return err
	})
	return rows, err
}

func (r *Repository) DeleteCompletedCodes(ctx context.Context, workspaceID, cpaID string) (int64, error) {
	if err := requireScope(workspaceID, cpaID); err != nil {
		return 0, err
	}

	var rows int64
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}
		var err error
		rows, err = txRepo.q.AdminDeleteCompletedCodes(ctx, cpasqlc.AdminDeleteCompletedCodesParams{
			WorkspaceID: workspaceID,
			CpaID:       cpaID,
		})
		return err
	})
	return rows, err
}

func (r *Repository) allocateCode(ctx context.Context, offer cpasqlc.CpaOffer) (string, *uint64, error) {
	if offer.CodeMode == cpasqlc.CpaCodeModeSharedCode {
		if !offer.SharedCode.Valid || offer.SharedCode.String == "" {
			return "", nil, errors.New("cpa: shared code is empty")
		}
		return offer.SharedCode.String, nil, nil
	}
	if !offer.CodeSource.Valid {
		return "", nil, ErrInvalidCodeConfig
	}
	if offer.CodeSource.CpaCodeSource == cpasqlc.CpaCodeSourcePool {
		row, err := r.q.GetAvailableCodeForUpdate(ctx, cpasqlc.GetAvailableCodeForUpdateParams{
			WorkspaceID: offer.WorkspaceID,
			CpaID:       offer.ID,
		})
		if isNoRows(err) {
			return "", nil, ErrNoCodesAvailable
		}
		if err != nil {
			return "", nil, err
		}
		id := uint64(row.ID)
		return row.Code, &id, nil
	}
	if !offer.GeneratedLength.Valid || !offer.GeneratedAlphabet.Valid {
		return "", nil, ErrInvalidCodeConfig
	}
	for range 16 {
		code, err := randomCode(int(offer.GeneratedLength.Int16), offer.GeneratedAlphabet.String)
		if err != nil {
			return "", nil, err
		}
		id, err := r.q.CreateGeneratedCode(ctx, cpasqlc.CreateGeneratedCodeParams{
			WorkspaceID: offer.WorkspaceID,
			CpaID:       offer.ID,
			Code:        code,
		})
		if err == nil {
			value := uint64(id)
			return code, &value, nil
		}
		if !isUniqueViolation(err) {
			return "", nil, err
		}
	}
	return "", nil, errors.New("cpa: generated code collision limit reached")
}

func (r *Repository) recordEvent(ctx context.Context, assignment Assignment, rewards []Reward, eventType model.AssignmentEventType) error {
	_, err := r.q.CreateAssignmentEvent(ctx, cpasqlc.CreateAssignmentEventParams{
		WorkspaceID:  assignment.WorkspaceID,
		CpaID:        assignment.CPAID,
		AssignmentID: int64(assignment.ID),
		EventType:    cpasqlc.CpaAssignmentEventType(eventType),
	})
	if err != nil {
		return err
	}
	payload := callbackPayload{
		AssignmentID:   assignment.ID,
		WorkspaceID:    assignment.WorkspaceID,
		CPAID:          assignment.CPAID,
		AppID:          assignment.AppID,
		PlatformID:     assignment.PlatformID,
		PlatformUserID: assignment.PlatformUserID,
		Code:           assignment.Code,
		CodeMode:       assignment.CodeMode,
		Status:         eventType,
		Rewards:        make([]callbackReward, 0, len(rewards)),
	}
	for _, reward := range rewards {
		payload.Rewards = append(payload.Rewards, callbackReward{
			Key:      reward.Key,
			Type:     reward.Type,
			Quantity: reward.Quantity,
			Scale:    reward.Scale,
			Unit:     reward.Unit,
		})
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	eventKey := fmt.Sprintf("cpa.%s:%d", eventType, assignment.ID)
	_, err = r.callbacks.CreateEvent(ctx, callbackutil.CreateParams{
		WorkspaceID:        assignment.WorkspaceID,
		SourceService:      "cpa",
		EventType:          "cpa." + string(eventType),
		EventKey:           eventKey,
		IdempotencyKey:     eventKey,
		Payload:            raw,
		PayloadContentType: callbackutil.JSONContentType,
	})
	return err
}

type callbackPayload struct {
	AssignmentID   uint64                    `json:"assignment_id"`
	WorkspaceID    string                    `json:"workspace_id"`
	CPAID          string                    `json:"cpa_id"`
	AppID          int64                     `json:"app_id"`
	PlatformID     int64                     `json:"platform_id"`
	PlatformUserID string                    `json:"platform_user_id"`
	Code           string                    `json:"code"`
	CodeMode       string                    `json:"code_mode"`
	Status         model.AssignmentEventType `json:"status"`
	Rewards        []callbackReward          `json:"rewards"`
}

type callbackReward struct {
	Key      string  `json:"key"`
	Type     string  `json:"type"`
	Quantity int64   `json:"quantity"`
	Scale    uint16  `json:"scale"`
	Unit     *string `json:"unit,omitempty"`
}

func assignmentParams(scope UserScope) cpasqlc.GetAssignmentParams {
	return cpasqlc.GetAssignmentParams{
		WorkspaceID:    scope.WorkspaceID,
		CpaID:          scope.CPAID,
		AppID:          scope.AppID,
		PlatformID:     scope.PlatformID,
		PlatformUserID: scope.PlatformUserID,
	}
}

func assignmentForUpdateParams(scope UserScope) cpasqlc.GetAssignmentForUpdateParams {
	return cpasqlc.GetAssignmentForUpdateParams(assignmentParams(scope))
}

func mapAssignment(row cpasqlc.CpaAssignment) (Assignment, error) {
	var codeID *uint64
	if row.CodeID.Valid {
		value := uint64(row.CodeID.Int64)
		codeID = &value
	}
	rewards, err := decodeRewardsSnapshot(row.WorkspaceID, row.CpaID, row.RewardsSnapshot)
	if err != nil {
		return Assignment{}, err
	}

	return Assignment{
		ID:             uint64(row.ID),
		WorkspaceID:    row.WorkspaceID,
		CPAID:          row.CpaID,
		AppID:          row.AppID,
		PlatformID:     row.PlatformID,
		PlatformUserID: row.PlatformUserID,
		CodeID:         codeID,
		Code:           row.Code,
		CodeMode:       string(row.CodeMode),
		Rewards:        rewards,
		Status:         model.AssignmentStatus(row.Status),
		IssuedAt:       row.IssuedAt,
		CompletedAt:    sqlwrap.NullTimePtr(row.CompletedAt),
	}, nil
}

func encodeRewardsSnapshot(rewards []Reward) (json.RawMessage, error) {
	values := make([]callbackReward, 0, len(rewards))
	for _, reward := range rewards {
		values = append(values, callbackReward{
			Key:      reward.Key,
			Type:     reward.Type,
			Quantity: reward.Quantity,
			Scale:    reward.Scale,
			Unit:     reward.Unit,
		})
	}
	return json.Marshal(values)
}

func decodeRewardsSnapshot(workspaceID, cpaID string, raw json.RawMessage) ([]Reward, error) {
	var values []callbackReward
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("cpa assignment reward snapshot decode failed: %w", err)
	}

	result := make([]Reward, 0, len(values))
	for _, reward := range values {
		result = append(result, Reward{
			WorkspaceID: workspaceID,
			CPAID:       cpaID,
			Key:         reward.Key,
			Type:        reward.Type,
			Quantity:    reward.Quantity,
			Scale:       reward.Scale,
			Unit:        reward.Unit,
		})
	}
	return result, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func randomCode(length int, alphabet string) (string, error) {
	runes := []rune(alphabet)
	if length <= 0 || length > maxCodeLength || uniqueRuneCount(alphabet) < 2 {
		return "", ErrInvalidCodeConfig
	}
	result := make([]rune, length)
	max := big.NewInt(int64(len(runes)))
	for index := range result {
		value, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		result[index] = runes[value.Int64()]
	}
	return string(result), nil
}
