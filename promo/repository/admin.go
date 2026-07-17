package repository

import (
	"context"
	"database/sql"
	"time"

	json "github.com/goccy/go-json"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	promosqlc "github.com/elum2b/services/promo/sqlc"
	"github.com/sqlc-dev/pqtype"
)

type SavePromoParams struct {
	ID             uint64
	WorkspaceID    string
	Code           string
	Payload        json.RawMessage
	Target         json.RawMessage
	MaxActivations uint64
	IsActive       bool
	StartAt        *time.Time
	EndAt          *time.Time
}

func (r *Repository) CreatePromo(ctx context.Context, params SavePromoParams) (uint64, error) {
	target := params.Target
	if len(target) == 0 {
		target = []byte("null")
	}
	var id int64
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, params.WorkspaceID); err != nil {
			return err
		}

		var err error
		id, err = txRepo.q.AdminCreatePromo(ctx, promosqlc.AdminCreatePromoParams{
			WorkspaceID:    params.WorkspaceID,
			Code:           params.Code,
			CodeNormalized: normalizeCode(params.Code),
			Payload:        params.Payload,
			Target:         rawMessageParam(target),
			MaxActivations: int64(params.MaxActivations),
			IsActive:       params.IsActive,
			StartAt: sqlwrap.NullFromPtr(params.StartAt, func(v time.Time) sql.NullTime {
				return sql.NullTime{Time: v, Valid: true}
			}),
			EndAt: sqlwrap.NullFromPtr(params.EndAt, func(v time.Time) sql.NullTime {
				return sql.NullTime{Time: v, Valid: true}
			}),
		})
		return err
	})
	if err != nil {
		return 0, err
	}
	return uint64(id), r.invalidatePromoCache(params.WorkspaceID)
}

func (r *Repository) UpdatePromo(ctx context.Context, params SavePromoParams) (int64, error) {
	target := params.Target
	if len(target) == 0 {
		target = []byte("null")
	}
	var rows int64
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, params.WorkspaceID); err != nil {
			return err
		}

		var err error
		rows, err = txRepo.q.AdminUpdatePromo(ctx, promosqlc.AdminUpdatePromoParams{
			Code:           params.Code,
			CodeNormalized: normalizeCode(params.Code),
			Payload:        params.Payload,
			Target:         rawMessageParam(target),
			MaxActivations: int64(params.MaxActivations),
			IsActive:       params.IsActive,
			StartAt: sqlwrap.NullFromPtr(params.StartAt, func(v time.Time) sql.NullTime {
				return sql.NullTime{Time: v, Valid: true}
			}),
			EndAt: sqlwrap.NullFromPtr(params.EndAt, func(v time.Time) sql.NullTime {
				return sql.NullTime{Time: v, Valid: true}
			}),
			WorkspaceID: params.WorkspaceID,
			ID:          int64(params.ID),
		})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}
	return rows, r.invalidatePromoCache(params.WorkspaceID)
}

func (r *Repository) GetPromo(ctx context.Context, workspaceID string, id uint64) (Promo, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return Promo{}, err
	}

	key := promoCacheKey(promoCacheAdminPromo, workspaceID, id)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: promoCacheScope(promoCacheAdminPromo, workspaceID),
	}, func(ctx context.Context) (Promo, error) {
		row, err := r.q.AdminGetPromo(ctx, promosqlc.AdminGetPromoParams{
			WorkspaceID: workspaceID,
			ID:          int64(id),
		})
		if err != nil {
			return Promo{}, err
		}
		return mapPromo(row), nil
	})
}

func (r *Repository) ListPromos(ctx context.Context, workspaceID string, limit, offset int32) ([]Promo, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	limit, offset = normalizePage(limit, offset)
	key := promoCacheKey(promoCacheAdminList, workspaceID, limit, offset)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: promoCacheScope(promoCacheAdminList, workspaceID),
	}, func(ctx context.Context) ([]Promo, error) {
		rows, err := r.q.AdminListPromos(ctx, promosqlc.AdminListPromosParams{
			WorkspaceID: workspaceID,
			Limit:       limit,
			Offset:      offset,
		})
		if err != nil {
			return nil, err
		}
		result := make([]Promo, 0, len(rows))
		for _, row := range rows {
			result = append(result, mapPromo(row))
		}
		return result, nil
	})
}

func (r *Repository) SoftDeletePromo(ctx context.Context, workspaceID string, id uint64) (int64, error) {
	var rows int64
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}

		var err error
		rows, err = txRepo.q.AdminSoftDeletePromo(ctx, promosqlc.AdminSoftDeletePromoParams{
			WorkspaceID: workspaceID,
			ID:          int64(id),
		})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}
	return rows, r.invalidatePromoCache(workspaceID)
}

func (r *Repository) UpsertLocalization(ctx context.Context, value Localization) error {
	err := r.withWorkspaceMutation(ctx, value.WorkspaceID, func(txRepo *Repository) error {
		return txRepo.q.AdminUpsertLocalization(ctx, promosqlc.AdminUpsertLocalizationParams{
			WorkspaceID: value.WorkspaceID,
			PromoID:     int64(value.PromoID),
			Locale:      value.Locale,
			Title:       value.Title,
			Description: value.Description,
		})
	})
	if err != nil {
		return err
	}

	return r.invalidatePromoCache(value.WorkspaceID)
}

func (r *Repository) GetLocalization(
	ctx context.Context,
	workspaceID string,
	promoID uint64,
	locale string,
) (Localization, error) {
	key := promoCacheKey(promoCacheAdminLocalization, workspaceID, promoID, locale)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: promoCacheScope(promoCacheAdminLocalization, workspaceID),
	}, func(ctx context.Context) (Localization, error) {
		row, err := r.q.AdminGetLocalization(ctx, promosqlc.AdminGetLocalizationParams{
			WorkspaceID: workspaceID,
			PromoID:     int64(promoID),
			Locale:      locale,
		})
		if err != nil {
			return Localization{}, err
		}
		return mapLocalization(row), nil
	})
}

func (r *Repository) ListLocalizations(
	ctx context.Context,
	workspaceID string,
	promoID uint64,
) ([]Localization, error) {
	key := promoCacheKey(promoCacheAdminLocalizations, workspaceID, promoID)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: promoCacheScope(promoCacheAdminLocalizations, workspaceID),
	}, func(ctx context.Context) ([]Localization, error) {
		rows, err := r.q.AdminListLocalizations(ctx, promosqlc.AdminListLocalizationsParams{
			WorkspaceID: workspaceID,
			PromoID:     int64(promoID),
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

func (r *Repository) DeleteLocalization(
	ctx context.Context,
	workspaceID string,
	promoID uint64,
	locale string,
) (int64, error) {
	var rows int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		var err error
		rows, err = txRepo.q.AdminDeleteLocalization(ctx, promosqlc.AdminDeleteLocalizationParams{
			WorkspaceID: workspaceID,
			PromoID:     int64(promoID),
			Locale:      locale,
		})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}

	return rows, r.invalidatePromoCache(workspaceID)
}

func (r *Repository) UpsertReward(ctx context.Context, workspaceID string, promoID uint64, reward Reward) error {
	err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		return txRepo.q.AdminUpsertReward(ctx, promosqlc.AdminUpsertRewardParams{
			WorkspaceID: workspaceID,
			PromoID:     int64(promoID),
			RewardKey:   reward.Key,
			RewardType:  promosqlc.PromoRewardType(reward.Type),
			Quantity:    reward.Quantity,
			Scale:       int16(reward.Scale),
			DurationUnit: promosqlc.NullPromoDurationUnit{
				PromoDurationUnit: promosqlc.PromoDurationUnit(stringValue(reward.Unit)),
				Valid:             reward.Unit != nil,
			},
		})
	})
	if err != nil {
		return err
	}

	return r.invalidatePromoCache(workspaceID)
}

func (r *Repository) GetReward(ctx context.Context, workspaceID string, promoID uint64, key string) (Reward, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return Reward{}, err
	}

	cacheKey := promoCacheKey(promoCacheAdminReward, workspaceID, promoID, key)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               cacheKey,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: promoCacheScope(promoCacheAdminReward, workspaceID),
	}, func(ctx context.Context) (Reward, error) {
		row, err := r.q.AdminGetReward(ctx, promosqlc.AdminGetRewardParams{
			WorkspaceID: workspaceID,
			PromoID:     int64(promoID),
			RewardKey:   key,
		})
		if err != nil {
			return Reward{}, err
		}
		return mapReward(row), nil
	})
}

func (r *Repository) ListRewards(ctx context.Context, workspaceID string, promoID uint64) ([]Reward, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	key := promoCacheKey(promoCacheAdminRewards, workspaceID, promoID)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: promoCacheScope(promoCacheAdminRewards, workspaceID),
	}, func(ctx context.Context) ([]Reward, error) {
		rows, err := r.q.ListRewards(ctx, promosqlc.ListRewardsParams{
			WorkspaceID: workspaceID,
			PromoID:     int64(promoID),
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

func mapReward(row promosqlc.PromoReward) Reward {
	return Reward{
		Key:      row.RewardKey,
		Type:     string(row.RewardType),
		Quantity: row.Quantity,
		Scale:    uint16(row.Scale),
		Unit:     promoDurationUnitPtr(row.DurationUnit),
	}
}

func promoDurationUnitPtr(value promosqlc.NullPromoDurationUnit) *string {
	if !value.Valid {
		return nil
	}
	unit := string(value.PromoDurationUnit)
	return &unit
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (r *Repository) DeleteReward(ctx context.Context, workspaceID string, promoID uint64, key string) (int64, error) {
	var rows int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		var err error
		rows, err = txRepo.q.AdminDeleteReward(ctx, promosqlc.AdminDeleteRewardParams{
			WorkspaceID: workspaceID,
			PromoID:     int64(promoID),
			RewardKey:   key,
		})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}

	return rows, r.invalidatePromoCache(workspaceID)
}

func mapPromo(row promosqlc.PromoOffer) Promo {
	return Promo{
		ID:              uint64(row.ID),
		WorkspaceID:     row.WorkspaceID,
		Code:            row.Code,
		Payload:         row.Payload,
		Target:          nullRawMessage(row.Target),
		MaxActivations:  uint64(row.MaxActivations),
		ActivationCount: uint64(row.ActivationCount),
		IsActive:        row.IsActive,
		StartAt:         sqlwrap.NullTimePtr(row.StartAt),
		EndAt:           sqlwrap.NullTimePtr(row.EndAt),
		DeletedAt:       sqlwrap.NullTimePtr(row.DeletedAt),
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func mapLocalization(row promosqlc.PromoLocalization) Localization {
	return Localization{
		WorkspaceID: row.WorkspaceID,
		PromoID:     uint64(row.PromoID),
		Locale:      row.Locale,
		Title:       row.Title,
		Description: row.Description,
	}
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
