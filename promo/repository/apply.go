package repository

import (
	"context"
	"database/sql"
	"fmt"
	json "github.com/goccy/go-json"
	"time"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/internal/utils/target"
	promosqlc "github.com/elum2b/services/promo/sqlc"
)

func (r *Repository) Apply(ctx context.Context, identity Identity, code, locale string) (ApplyResult, error) {
	if err := identity.Validate(); err != nil {
		return ApplyResult{}, err
	}

	result := ApplyResult{Status: StatusNotFound}
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		rows, err := txRepo.q.GetApplyBundleForUpdate(ctx, promosqlc.GetApplyBundleForUpdateParams{
			Locale:         locale,
			AppID:          identity.AppID,
			PlatformID:     identity.PlatformID,
			PlatformUserID: identity.PlatformUserID,
			WorkspaceID:    identity.WorkspaceID,
			CodeNormalized: normalizeCode(code),
		})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		result, err = mapApplyBundle(rows)
		if err != nil {
			return err
		}
		if result.Redemption != nil {
			result.Status = StatusAlreadyApplied
			return nil
		}
		if !target.Match(result.Promo.Target, target.Context{
			IsPremium:  identity.IsPremium,
			Sex:        identity.Sex,
			Country:    identity.Country,
			Locale:     locale,
			Platform:   identity.Platform,
			PlatformID: identity.PlatformID,
		}) {
			result.Status = StatusNotFound
			return nil
		}

		now := time.Now()
		switch {
		case result.Promo.DeletedAt != nil:
			result.Status = StatusNotFound
		case !result.Promo.IsActive:
			result.Status = StatusInactive
		case result.Promo.StartAt != nil && now.Before(*result.Promo.StartAt):
			result.Status = StatusNotStarted
		case result.Promo.EndAt != nil && !now.Before(*result.Promo.EndAt):
			result.Status = StatusExpired
		case result.Promo.MaxActivations > 0 &&
			result.Promo.ActivationCount >= result.Promo.MaxActivations:
			result.Status = StatusLimitReached
		default:
			rewardSnapshot, err := json.Marshal(result.Rewards)
			if err != nil {
				return err
			}
			created, err := txRepo.q.CreateRedemption(ctx, promosqlc.CreateRedemptionParams{
				WorkspaceID:    identity.WorkspaceID,
				PromoID:        int64(result.Promo.ID),
				AppID:          identity.AppID,
				PlatformID:     identity.PlatformID,
				PlatformUserID: identity.PlatformUserID,
				RewardSnapshot: rewardSnapshot,
			})
			if err != nil {
				return err
			}
			redemption := Redemption{
				ID:             uint64(created.ID),
				WorkspaceID:    identity.WorkspaceID,
				PromoID:        result.Promo.ID,
				AppID:          identity.AppID,
				PlatformID:     identity.PlatformID,
				PlatformUserID: identity.PlatformUserID,
				RedeemedAt:     created.RedeemedAt,
			}
			result.Redemption = &redemption
			result.Status = StatusSuccess
			result.Promo.ActivationCount++
		}
		return nil
	})
	return result, err
}

func mapApplyBundle(rows []promosqlc.GetApplyBundleForUpdateRow) (ApplyResult, error) {
	first := rows[0]
	result := ApplyResult{
		Status: StatusNotFound,
		Promo: Promo{
			ID:              uint64(first.ID),
			WorkspaceID:     first.WorkspaceID,
			Code:            first.Code,
			Payload:         first.Payload,
			Target:          nullRawMessage(first.Target),
			MaxActivations:  uint64(first.MaxActivations),
			ActivationCount: uint64(first.ActivationCount),
			IsActive:        first.IsActive,
			StartAt:         sqlwrap.NullTimePtr(first.StartAt),
			EndAt:           sqlwrap.NullTimePtr(first.EndAt),
			DeletedAt:       sqlwrap.NullTimePtr(first.DeletedAt),
			CreatedAt:       first.CreatedAt,
			UpdatedAt:       first.UpdatedAt,
		},
		Rewards: make([]Reward, 0, len(rows)),
	}
	if first.LocalizationLocale.Valid {
		result.Localization = &Localization{
			WorkspaceID: first.WorkspaceID,
			PromoID:     uint64(first.ID),
			Locale:      first.LocalizationLocale.String,
			Title:       first.LocalizationTitle.String,
			Description: first.LocalizationDescription.String,
		}
	}
	if first.RedemptionID.Valid {
		result.Redemption = &Redemption{
			ID:             uint64(first.RedemptionID.Int64),
			WorkspaceID:    first.WorkspaceID,
			PromoID:        uint64(first.ID),
			AppID:          first.RedemptionAppID.Int64,
			PlatformID:     first.RedemptionPlatformID.Int64,
			PlatformUserID: first.RedemptionPlatformUserID.String,
			RedeemedAt:     first.RedemptionRedeemedAt.Time,
		}
		if err := json.Unmarshal(nullRawMessage(first.RedemptionRewardSnapshot), &result.Rewards); err != nil {
			return ApplyResult{}, fmt.Errorf("promo redemption reward snapshot decode failed: %w", err)
		}

		return result, nil
	}
	for _, row := range rows {
		if row.RewardID.Valid {
			result.Rewards = append(result.Rewards, Reward{
				Key:      row.RewardKey.String,
				Type:     string(row.RewardType.PromoRewardType),
				Quantity: row.RewardQuantity.Int64,
				Scale:    uint16FromNull(row.RewardScale),
				Unit:     promoDurationUnitPtr(row.DurationUnit),
			})
		}
	}
	return result, nil
}

func uint16FromNull(value sql.NullInt16) uint16 {
	if !value.Valid || value.Int16 < 0 {
		return 0
	}
	return uint16(value.Int16)
}

func mapRedemption(row promosqlc.PromoRedemption) Redemption {
	return Redemption{
		ID:             uint64(row.ID),
		WorkspaceID:    row.WorkspaceID,
		PromoID:        uint64(row.PromoID),
		AppID:          row.AppID,
		PlatformID:     row.PlatformID,
		PlatformUserID: row.PlatformUserID,
		RedeemedAt:     row.RedeemedAt,
	}
}
