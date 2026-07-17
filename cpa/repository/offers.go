package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	json "github.com/goccy/go-json"
	"github.com/sqlc-dev/pqtype"

	services "github.com/elum2b/services"
	cpasqlc "github.com/elum2b/services/cpa/sqlc"
	serviceerrors "github.com/elum2b/services/errors"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/internal/utils/target"
)

const (
	maxOfferIDLength           = 128
	maxCodeLength              = 512
	maxGeneratedAlphabetLength = 512
	maxLocaleLength            = 16
	maxLocalizationTitleLength = 255
	maxRewardKeyLength         = 128
)

type UpsertOfferParams struct {
	WorkspaceID       string
	ID                string
	Payload           json.RawMessage
	Target            json.RawMessage
	CodeMode          string
	CodeSource        *string
	SharedCode        *string
	GeneratedLength   *int16
	GeneratedAlphabet *string
	IsActive          bool
	StartAt           *time.Time
	EndAt             *time.Time
}

type FieldValidationError struct {
	Field  string `json:"field"`
	Detail string
}

func (e *FieldValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Detail
}

func (e *FieldValidationError) Code() string {
	return serviceerrors.CodeInvalidFields
}

func (e *FieldValidationError) Message() string {
	if e == nil {
		return ""
	}
	return e.Detail
}

type OfferValidationError = FieldValidationError

func ValidateOffer(params UpsertOfferParams) error {
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return invalidOfferField("workspace_id", err.Error())
	}
	if strings.TrimSpace(params.ID) == "" {
		return invalidOfferField("id", "offer id is required")
	}
	if err := validateStoredString("id", params.ID, maxOfferIDLength); err != nil {
		return err
	}
	if len(params.Payload) == 0 || !json.Valid(params.Payload) {
		return invalidOfferField("payload", "payload must be valid JSON")
	}
	if err := target.Validate(params.Target); err != nil {
		return invalidOfferField("target", err.Error())
	}
	if params.StartAt != nil && params.EndAt != nil && !params.StartAt.Before(*params.EndAt) {
		return invalidOfferField("start_at", "start_at must be before end_at")
	}
	switch params.CodeMode {
	case CodeModeShared:
		if params.SharedCode == nil || strings.TrimSpace(*params.SharedCode) == "" {
			return invalidOfferField("shared_code", "shared code is required")
		}
		if err := validateStoredString("shared_code", *params.SharedCode, maxCodeLength); err != nil {
			return err
		}
	case CodeModePersonal:
		if params.CodeSource == nil {
			return invalidOfferField("code_source", "personal code source is required")
		}
		switch *params.CodeSource {
		case CodeSourcePool:
		case CodeSourceGenerated:
			if params.GeneratedLength == nil || *params.GeneratedLength <= 0 || *params.GeneratedLength > maxCodeLength {
				return invalidOfferField("generated_length", "generated code length must be between 1 and 512")
			}
			if params.GeneratedAlphabet == nil {
				return invalidOfferField("generated_alphabet", "generated alphabet is required")
			}
			if err := validateStoredString("generated_alphabet", *params.GeneratedAlphabet, maxGeneratedAlphabetLength); err != nil {
				return err
			}
			if uniqueRuneCount(*params.GeneratedAlphabet) < 2 {
				return invalidOfferField("generated_alphabet", "generated alphabet needs at least two symbols")
			}
		default:
			return invalidOfferField("code_source", "unsupported personal code source")
		}
	default:
		return invalidOfferField("code_mode", "unsupported code mode")
	}
	return nil
}

func NormalizeOffer(params *UpsertOfferParams) {
	if params == nil {
		return
	}
	if params.CodeMode == CodeModeShared {
		params.CodeSource = nil
		params.GeneratedLength = nil
		params.GeneratedAlphabet = nil
		return
	}
	params.SharedCode = nil
	if params.CodeSource != nil && *params.CodeSource == CodeSourcePool {
		params.GeneratedLength = nil
		params.GeneratedAlphabet = nil
	}
}

func invalidOfferField(field, message string) *FieldValidationError {
	return &FieldValidationError{
		Field:  field,
		Detail: fmt.Sprintf("cpa offer %s: %s", field, message),
	}
}

func ValidateLocalization(value Localization) error {
	if err := requireScope(value.WorkspaceID, value.CPAID); err != nil {
		return err
	}
	if strings.TrimSpace(value.Locale) == "" {
		return &FieldValidationError{
			Field:  "locale",
			Detail: "cpa localization locale is required",
		}
	}
	if err := validateStoredString("locale", value.Locale, maxLocaleLength); err != nil {
		return err
	}
	if strings.TrimSpace(value.Title) == "" {
		return &FieldValidationError{
			Field:  "title",
			Detail: "cpa localization title is required",
		}
	}
	if err := validateStoredString("title", value.Title, maxLocalizationTitleLength); err != nil {
		return err
	}
	return nil
}

func NormalizeAndValidateReward(value Reward) (Reward, error) {
	if err := requireScope(value.WorkspaceID, value.CPAID); err != nil {
		return Reward{}, err
	}
	if strings.TrimSpace(value.Key) == "" {
		return Reward{}, &FieldValidationError{
			Field:  "key",
			Detail: "cpa reward key is required",
		}
	}
	if err := validateStoredString("key", value.Key, maxRewardKeyLength); err != nil {
		return Reward{}, err
	}
	if value.Quantity <= 0 {
		return Reward{}, &FieldValidationError{
			Field:  "quantity",
			Detail: "cpa reward quantity must be positive",
		}
	}
	if value.Type == "" {
		value.Type = "quantity"
	}
	switch value.Type {
	case "quantity":
		if value.Unit != nil {
			return Reward{}, &FieldValidationError{
				Field:  "unit",
				Detail: "cpa quantity reward must not have duration unit",
			}
		}
	case "duration":
		if value.Unit == nil || !validDurationUnit(*value.Unit) {
			return Reward{}, &FieldValidationError{
				Field:  "unit",
				Detail: "cpa duration reward requires a valid duration unit",
			}
		}
	default:
		return Reward{}, &FieldValidationError{
			Field:  "type",
			Detail: "cpa reward type must be quantity or duration",
		}
	}
	return value, nil
}

func ValidateReward(value Reward) error {
	_, err := NormalizeAndValidateReward(value)
	return err
}

func validDurationUnit(unit string) bool {
	switch unit {
	case "second", "minute", "hour", "day", "week", "month", "year":
		return true
	default:
		return false
	}
}

func validateStoredString(field, value string, maxLength int) error {
	if !utf8.ValidString(value) {
		return invalidOfferField(field, "value must be valid UTF-8")
	}
	if utf8.RuneCountInString(value) > maxLength {
		return invalidOfferField(field, fmt.Sprintf("value exceeds %d characters", maxLength))
	}
	return nil
}

func uniqueRuneCount(value string) int {
	values := make(map[rune]struct{}, len(value))
	for _, symbol := range value {
		values[symbol] = struct{}{}
	}
	return len(values)
}

func (r *Repository) UpsertOffer(ctx context.Context, params UpsertOfferParams) error {
	if err := ValidateOffer(params); err != nil {
		return err
	}
	NormalizeOffer(&params)
	target := params.Target
	if len(target) == 0 {
		target = []byte("null")
	}
	if err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, params.WorkspaceID); err != nil {
			return err
		}

		return txRepo.q.AdminUpsertOffer(ctx, cpasqlc.AdminUpsertOfferParams{
			WorkspaceID: params.WorkspaceID,
			ID:          params.ID,
			Payload:     params.Payload,
			Target:      rawMessageParam(target),
			CodeMode:    cpasqlc.CpaCodeMode(params.CodeMode),
			CodeSource: sqlwrap.NullFromPtr(params.CodeSource, func(v string) cpasqlc.NullCpaCodeSource {
				return cpasqlc.NullCpaCodeSource{
					CpaCodeSource: cpasqlc.CpaCodeSource(v),
					Valid:         true,
				}
			}),
			SharedCode: sqlwrap.NullFromPtr(params.SharedCode, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			GeneratedLength: sqlwrap.NullFromPtr(params.GeneratedLength, func(v int16) sql.NullInt16 {
				return sql.NullInt16{Int16: v, Valid: true}
			}),
			GeneratedAlphabet: sqlwrap.NullFromPtr(params.GeneratedAlphabet, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			IsActive: params.IsActive,
			StartAt: sqlwrap.NullFromPtr(params.StartAt, func(v time.Time) sql.NullTime {
				return sql.NullTime{Time: v, Valid: true}
			}),
			EndAt: sqlwrap.NullFromPtr(params.EndAt, func(v time.Time) sql.NullTime {
				return sql.NullTime{Time: v, Valid: true}
			}),
		})
	}); err != nil {
		return err
	}
	r.invalidateCPACache(params.WorkspaceID, params.ID)
	return nil
}

func (r *Repository) GetOfferBundle(ctx context.Context, workspaceID, cpaID string) (OfferBundle, error) {
	if err := requireScope(workspaceID, cpaID); err != nil {
		return OfferBundle{}, err
	}

	key := cpaCacheKey("admin_get_offer_bundle", workspaceID, cpaID)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: cpaOfferCacheVersionScope(workspaceID, cpaID),
	}, func(ctx context.Context) (OfferBundle, error) {
		var result OfferBundle
		err := r.WithReadOnlySnapshot(ctx, func(txRepo *Repository) error {
			offer, err := txRepo.q.AdminGetOffer(ctx, cpasqlc.AdminGetOfferParams{
				WorkspaceID: workspaceID,
				ID:          cpaID,
			})
			if err != nil {
				return err
			}
			localizations, err := txRepo.q.ListLocalizations(ctx, cpasqlc.ListLocalizationsParams{
				WorkspaceID: workspaceID,
				CpaID:       cpaID,
			})
			if err != nil {
				return err
			}
			rewards, err := txRepo.q.ListRewards(ctx, cpasqlc.ListRewardsParams{
				WorkspaceID: workspaceID,
				CpaID:       cpaID,
			})
			if err != nil {
				return err
			}

			result = OfferBundle{
				Offer:         mapOffer(offer),
				Localizations: mapLocalizations(localizations),
				Rewards:       mapRewards(rewards),
			}
			return nil
		})
		return result, err
	})
}

func (r *Repository) ListOfferBundles(ctx context.Context, workspaceID string, limit, offset int32) ([]OfferBundle, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return nil, err
	}
	limit, offset = normalizePage(limit, offset)
	key := cpaCacheKey("admin_list_offer_bundles", workspaceID, limit, offset)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: cpaAdminListCacheVersionScope(workspaceID),
	}, func(ctx context.Context) ([]OfferBundle, error) {
		var result []OfferBundle
		err := r.WithReadOnlySnapshot(ctx, func(txRepo *Repository) error {
			rows, err := txRepo.q.AdminListOfferBundles(ctx, cpasqlc.AdminListOfferBundlesParams{
				WorkspaceID: workspaceID,
				PageLimit:   limit,
				PageOffset:  offset,
			})
			if err != nil {
				return err
			}
			rewardRows, err := txRepo.q.AdminListOfferBundleRewards(ctx, cpasqlc.AdminListOfferBundleRewardsParams{
				WorkspaceID: workspaceID,
				PageLimit:   limit,
				PageOffset:  offset,
			})
			if err != nil {
				return err
			}

			result = mapAdminOfferBundles(rows, rewardRows, int(limit))
			return nil
		})
		return result, err
	})
}

func (r *Repository) ListAllOfferBundles(ctx context.Context, workspaceID string) ([]OfferBundle, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return nil, err
	}
	rows, err := r.q.AdminListOfferBundles(ctx, cpasqlc.AdminListOfferBundlesParams{
		WorkspaceID: workspaceID,
		PageLimit:   0,
		PageOffset:  0,
	})
	if err != nil {
		return nil, err
	}
	rewardRows, err := r.q.AdminListOfferBundleRewards(ctx, cpasqlc.AdminListOfferBundleRewardsParams{
		WorkspaceID: workspaceID,
		PageLimit:   0,
		PageOffset:  0,
	})
	if err != nil {
		return nil, err
	}
	return mapAdminOfferBundles(rows, rewardRows, len(rows)), nil
}

func (r *Repository) ListActiveForUser(ctx context.Context, scope UserScope, locale string) ([]OfferBundle, error) {
	if err := requireUserScope(scope, false); err != nil {
		return nil, err
	}
	if err := validateStoredString("locale", locale, maxLocaleLength); err != nil {
		return nil, err
	}
	catalog, err := r.listActiveOfferCatalog(ctx, scope.WorkspaceID, locale)
	if err != nil {
		return nil, err
	}
	if len(catalog) == 0 {
		return nil, nil
	}

	now := time.Now().UTC()
	activeCatalog := make([]OfferBundle, 0, len(catalog))
	for _, bundle := range catalog {
		if isOfferActiveAt(bundle.Offer, now) {
			activeCatalog = append(activeCatalog, bundle)
		}
	}
	if len(activeCatalog) == 0 {
		return nil, nil
	}

	assignments, err := r.ListUserAssignments(ctx, scope)
	if err != nil {
		return nil, err
	}
	assignmentByCPAID := make(map[string]Assignment, len(assignments))
	for _, assignment := range assignments {
		assignmentByCPAID[assignment.CPAID] = assignment
	}
	result := make([]OfferBundle, len(activeCatalog))
	copy(result, activeCatalog)
	for index := range result {
		if assignment, ok := assignmentByCPAID[result[index].Offer.ID]; ok {
			value := assignment
			result[index].Assignment = &value
			result[index].Rewards = assignment.Rewards
		}
	}
	return result, nil
}

func isOfferActiveAt(offer Offer, now time.Time) bool {
	if !offer.IsActive {
		return false
	}
	if offer.StartAt != nil && now.Before(*offer.StartAt) {
		return false
	}
	if offer.EndAt != nil && !now.Before(*offer.EndAt) {
		return false
	}
	return true
}

func (r *Repository) listActiveOfferCatalog(ctx context.Context, workspaceID, locale string) ([]OfferBundle, error) {
	key := cpaCacheKey("user_list_active_catalog", workspaceID, locale)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: cpaUserListCacheVersionScope(workspaceID),
	}, func(ctx context.Context) ([]OfferBundle, error) {
		rows, err := r.q.ListActiveOfferCatalog(ctx, cpasqlc.ListActiveOfferCatalogParams{
			Locale:      locale,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			return nil, err
		}
		return mapActiveOfferCatalogRows(rows), nil
	})
}

func (r *Repository) DeleteOffer(ctx context.Context, workspaceID, cpaID string) (int64, error) {
	if err := requireScope(workspaceID, cpaID); err != nil {
		return 0, err
	}
	var rows int64
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}

		var err error
		rows, err = txRepo.q.AdminDeleteOffer(ctx, cpasqlc.AdminDeleteOfferParams{
			WorkspaceID: workspaceID,
			ID:          cpaID,
		})
		return err
	})
	if isForeignKeyViolation(err) {
		return 0, ErrOfferInUse
	}
	if err != nil || rows == 0 {
		return rows, err
	}
	r.invalidateCPACache(workspaceID, cpaID)
	return rows, nil
}

func (r *Repository) UpsertLocalization(ctx context.Context, value Localization) error {
	if err := ValidateLocalization(value); err != nil {
		return err
	}
	if err := r.q.AdminUpsertLocalization(ctx, cpasqlc.AdminUpsertLocalizationParams{
		WorkspaceID: value.WorkspaceID,
		CpaID:       value.CPAID,
		Locale:      value.Locale,
		Title:       value.Title,
		Description: value.Description,
	}); err != nil {
		return err
	}
	r.invalidateCPACache(value.WorkspaceID, value.CPAID)
	return nil
}

func (r *Repository) GetLocalization(ctx context.Context, workspaceID, cpaID, locale string) (Localization, error) {
	if err := requireScope(workspaceID, cpaID); err != nil {
		return Localization{}, err
	}
	if err := requireLocale(locale); err != nil {
		return Localization{}, err
	}

	key := cpaCacheKey("get_localization", workspaceID, cpaID, locale)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: cpaOfferCacheVersionScope(workspaceID, cpaID),
	}, func(ctx context.Context) (Localization, error) {
		row, err := r.q.GetLocalization(ctx, cpasqlc.GetLocalizationParams{
			WorkspaceID: workspaceID,
			CpaID:       cpaID,
			Locale:      locale,
		})
		if err != nil {
			return Localization{}, err
		}
		return mapLocalization(row), nil
	})
}

func (r *Repository) ResolveLocalization(ctx context.Context, workspaceID, cpaID, locale string) (*Localization, error) {
	if locale != "" {
		value, err := r.GetLocalization(ctx, workspaceID, cpaID, locale)
		if err == nil {
			return &value, nil
		}
		if !isNoRows(err) {
			return nil, err
		}
	}
	values, err := r.ListLocalizations(ctx, workspaceID, cpaID)
	if err != nil || len(values) == 0 {
		return nil, err
	}
	return &values[0], nil
}

func (r *Repository) ListLocalizations(ctx context.Context, workspaceID, cpaID string) ([]Localization, error) {
	if err := requireScope(workspaceID, cpaID); err != nil {
		return nil, err
	}

	key := cpaCacheKey("list_localizations", workspaceID, cpaID)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: cpaOfferCacheVersionScope(workspaceID, cpaID),
	}, func(ctx context.Context) ([]Localization, error) {
		rows, err := r.q.ListLocalizations(ctx, cpasqlc.ListLocalizationsParams{
			WorkspaceID: workspaceID,
			CpaID:       cpaID,
		})
		if err != nil {
			return nil, err
		}
		result := make([]Localization, 0, len(rows))
		for _, row := range rows {
			result = append(result, mapLocalization(row))
		}
		return result, nil
	})
}

func (r *Repository) DeleteLocalization(ctx context.Context, workspaceID, cpaID, locale string) (int64, error) {
	if err := requireScope(workspaceID, cpaID); err != nil {
		return 0, err
	}
	if err := requireLocale(locale); err != nil {
		return 0, err
	}

	var rows int64
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}
		var err error
		rows, err = txRepo.q.AdminDeleteLocalization(ctx, cpasqlc.AdminDeleteLocalizationParams{
			WorkspaceID: workspaceID,
			CpaID:       cpaID,
			Locale:      locale,
		})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}
	r.invalidateCPACache(workspaceID, cpaID)
	return rows, nil
}

func (r *Repository) UpsertReward(ctx context.Context, value Reward) error {
	value, err := NormalizeAndValidateReward(value)
	if err != nil {
		return err
	}
	if err := r.q.AdminUpsertReward(ctx, cpasqlc.AdminUpsertRewardParams{
		WorkspaceID: value.WorkspaceID,
		CpaID:       value.CPAID,
		RewardKey:   value.Key,
		RewardType:  cpasqlc.CpaRewardType(value.Type),
		Quantity:    value.Quantity,
		Scale:       int32(value.Scale),
		DurationUnit: cpasqlc.NullCpaDurationUnit{
			CpaDurationUnit: cpasqlc.CpaDurationUnit(valueOrEmpty(value.Unit)),
			Valid:           value.Unit != nil,
		},
	}); err != nil {
		return err
	}
	r.invalidateCPACache(value.WorkspaceID, value.CPAID)
	return nil
}

func (r *Repository) ListRewards(ctx context.Context, workspaceID, cpaID string) ([]Reward, error) {
	if err := requireScope(workspaceID, cpaID); err != nil {
		return nil, err
	}

	key := cpaCacheKey("list_rewards", workspaceID, cpaID)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: cpaOfferCacheVersionScope(workspaceID, cpaID),
	}, func(ctx context.Context) ([]Reward, error) {
		rows, err := r.q.ListRewards(ctx, cpasqlc.ListRewardsParams{
			WorkspaceID: workspaceID,
			CpaID:       cpaID,
		})
		if err != nil {
			return nil, err
		}
		result := make([]Reward, 0, len(rows))
		for _, row := range rows {
			result = append(result, mapReward(row))
		}
		return result, nil
	})
}

func (r *Repository) listRewardsDirect(ctx context.Context, workspaceID, cpaID string) ([]Reward, error) {
	rows, err := r.q.ListRewards(ctx, cpasqlc.ListRewardsParams{
		WorkspaceID: workspaceID,
		CpaID:       cpaID,
	})
	if err != nil {
		return nil, err
	}

	result := make([]Reward, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapReward(row))
	}
	return result, nil
}

func (r *Repository) DeleteReward(ctx context.Context, workspaceID, cpaID, rewardKey string) (int64, error) {
	if err := requireScope(workspaceID, cpaID); err != nil {
		return 0, err
	}
	if err := requireRewardKey(rewardKey); err != nil {
		return 0, err
	}

	var rows int64
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}
		var err error
		rows, err = txRepo.q.AdminDeleteReward(ctx, cpasqlc.AdminDeleteRewardParams{
			WorkspaceID: workspaceID,
			CpaID:       cpaID,
			RewardKey:   rewardKey,
		})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}
	r.invalidateCPACache(workspaceID, cpaID)
	return rows, nil
}

func mapAdminOfferBundles(
	rows []cpasqlc.AdminListOfferBundlesRow,
	rewardRows []cpasqlc.AdminListOfferBundleRewardsRow,
	capacity int,
) []OfferBundle {
	result := make([]OfferBundle, 0, capacity)
	indexByID := make(map[string]int, capacity)
	for _, row := range rows {
		index, exists := indexByID[row.ID]
		if !exists {
			index = len(result)
			indexByID[row.ID] = index
			result = append(result, OfferBundle{
				Offer: mapBundleOffer(
					row.WorkspaceID,
					row.ID,
					row.Payload,
					row.Target,
					row.CodeMode,
					row.CodeSource,
					row.SharedCode,
					row.GeneratedLength,
					row.GeneratedAlphabet,
					row.IsActive,
					row.StartAt,
					row.EndAt,
					row.CreatedAt,
					row.UpdatedAt,
				),
				Localizations: make([]Localization, 0),
				Rewards:       make([]Reward, 0),
			})
		}
		if row.Locale.Valid {
			result[index].Localizations = append(result[index].Localizations, Localization{
				WorkspaceID: row.WorkspaceID,
				CPAID:       row.ID,
				Locale:      row.Locale.String,
				Title:       row.LocalizationTitle.String,
				Description: row.LocalizationDescription.String,
			})
		}
	}
	for _, row := range rewardRows {
		index, exists := indexByID[row.CpaID]
		if !exists {
			continue
		}
		result[index].Rewards = append(result[index].Rewards, Reward{
			WorkspaceID: row.WorkspaceID,
			CPAID:       row.CpaID,
			Key:         row.RewardKey,
			Type:        string(row.RewardType),
			Quantity:    row.RewardQuantity,
			Scale:       uint16(row.RewardScale),
			Unit:        cpaDurationUnitPtr(row.DurationUnit),
		})
	}
	for index := range result {
		sort.Slice(result[index].Localizations, func(i, j int) bool {
			return result[index].Localizations[i].Locale < result[index].Localizations[j].Locale
		})
	}
	return result
}

func mapOffer(row cpasqlc.CpaOffer) Offer {
	return Offer{
		WorkspaceID:       row.WorkspaceID,
		ID:                row.ID,
		Payload:           row.Payload,
		Target:            nullRawMessage(row.Target),
		CodeMode:          string(row.CodeMode),
		CodeSource:        nullCodeSourcePtr(row.CodeSource),
		SharedCode:        sqlwrap.NullStringPtr(row.SharedCode),
		GeneratedLength:   nullInt16Ptr(row.GeneratedLength),
		GeneratedAlphabet: sqlwrap.NullStringPtr(row.GeneratedAlphabet),
		IsActive:          row.IsActive,
		StartAt:           sqlwrap.NullTimePtr(row.StartAt),
		EndAt:             sqlwrap.NullTimePtr(row.EndAt),
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func mapLocalization(row cpasqlc.CpaLocalization) Localization {
	return Localization{
		WorkspaceID: row.WorkspaceID,
		CPAID:       row.CpaID,
		Locale:      row.Locale,
		Title:       row.Title,
		Description: row.Description,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func mapLocalizations(rows []cpasqlc.CpaLocalization) []Localization {
	result := make([]Localization, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapLocalization(row))
	}
	return result
}

func mapReward(row cpasqlc.CpaReward) Reward {
	return Reward{
		WorkspaceID: row.WorkspaceID,
		CPAID:       row.CpaID,
		Key:         row.RewardKey,
		Type:        string(row.RewardType),
		Quantity:    row.Quantity,
		Scale:       uint16(row.Scale),
		Unit:        cpaDurationUnitPtr(row.DurationUnit),
	}
}

func mapRewards(rows []cpasqlc.CpaReward) []Reward {
	result := make([]Reward, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapReward(row))
	}
	return result
}

func mapActiveOfferCatalogRows(rows []cpasqlc.ListActiveOfferCatalogRow) []OfferBundle {
	result := make([]OfferBundle, 0, len(rows))
	indexByID := make(map[string]int, len(rows))
	for _, row := range rows {
		index, exists := indexByID[row.ID]
		if !exists {
			bundle := OfferBundle{
				Offer: mapBundleOffer(
					row.WorkspaceID,
					row.ID,
					row.Payload,
					row.Target,
					row.CodeMode,
					row.CodeSource,
					row.SharedCode,
					row.GeneratedLength,
					row.GeneratedAlphabet,
					row.IsActive,
					row.StartAt,
					row.EndAt,
					row.CreatedAt,
					row.UpdatedAt,
				),
				Rewards: make([]Reward, 0),
			}
			if row.LocalizedLocale.Valid {
				bundle.Localization = &Localization{
					WorkspaceID: row.WorkspaceID,
					CPAID:       row.ID,
					Locale:      row.LocalizedLocale.String,
					Title:       row.LocalizedTitle.String,
					Description: row.LocalizedDescription.String,
				}
			}
			index = len(result)
			indexByID[row.ID] = index
			result = append(result, bundle)
		}
		if row.RewardKey.Valid {
			result[index].Rewards = append(result[index].Rewards, Reward{
				WorkspaceID: row.WorkspaceID,
				CPAID:       row.ID,
				Key:         row.RewardKey.String,
				Type:        string(row.RewardType.CpaRewardType),
				Quantity:    row.RewardQuantity.Int64,
				Scale:       uint16FromNull(row.RewardScale),
				Unit:        cpaDurationUnitPtr(row.DurationUnit),
			})
		}
	}
	return result
}

func cpaDurationUnitPtr(value cpasqlc.NullCpaDurationUnit) *string {
	if !value.Valid {
		return nil
	}
	unit := string(value.CpaDurationUnit)
	return &unit
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func mapBundleOffer(
	workspaceID string,
	id string,
	payload json.RawMessage,
	target pqtype.NullRawMessage,
	codeMode cpasqlc.CpaCodeMode,
	codeSource cpasqlc.NullCpaCodeSource,
	sharedCode sql.NullString,
	generatedLength sql.NullInt16,
	generatedAlphabet sql.NullString,
	isActive bool,
	startAt sql.NullTime,
	endAt sql.NullTime,
	createdAt time.Time,
	updatedAt time.Time,
) Offer {
	return mapOffer(cpasqlc.CpaOffer{
		WorkspaceID:       workspaceID,
		ID:                id,
		Payload:           payload,
		Target:            target,
		CodeMode:          codeMode,
		CodeSource:        codeSource,
		SharedCode:        sharedCode,
		GeneratedLength:   generatedLength,
		GeneratedAlphabet: generatedAlphabet,
		IsActive:          isActive,
		StartAt:           startAt,
		EndAt:             endAt,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
	})
}

func nullCodeSourcePtr(value cpasqlc.NullCpaCodeSource) *string {
	if !value.Valid {
		return nil
	}
	result := string(value.CpaCodeSource)
	return &result
}

func nullRawMessage(value pqtype.NullRawMessage) json.RawMessage {
	if !value.Valid {
		return nil
	}
	return json.RawMessage(value.RawMessage)
}

func rawMessageParam(value json.RawMessage) pqtype.NullRawMessage {
	if len(value) == 0 {
		return pqtype.NullRawMessage{}
	}
	return pqtype.NullRawMessage{RawMessage: value, Valid: true}
}

func nullInt16Ptr(value sql.NullInt16) *int16 {
	if !value.Valid {
		return nil
	}
	return &value.Int16
}

func uint16FromNull(value sql.NullInt32) uint16 {
	if !value.Valid || value.Int32 < 0 {
		return 0
	}
	return uint16(value.Int32)
}

func normalizePage(limit, offset int32) (int32, int32) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
