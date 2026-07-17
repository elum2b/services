package user

import (
	"context"

	"github.com/elum2b/services/promo/repository"
)

type ApplyParams struct {
	Identity Identity
	Code     string
	Locale   string
}

type ApplyResult struct {
	Status     string           `json:"status"`
	Promo      PromoModel       `json:"promo"`
	Redemption *RedemptionModel `json:"redemption,omitempty"`
}

func (u *User) Apply(ctx context.Context, params ApplyParams) (ApplyResult, error) {
	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()

	if err := params.Identity.Validate(); err != nil {
		return ApplyResult{}, err
	}

	result, err := u.repository.Apply(mergedCtx, repository.Identity{
		WorkspaceID: params.Identity.WorkspaceID, AppID: params.Identity.AppID,
		PlatformID: params.Identity.PlatformID, PlatformUserID: params.Identity.PlatformUserID,
		Platform:  params.Identity.Platform,
		IsPremium: params.Identity.IsPremium, Sex: params.Identity.Sex, Country: params.Identity.Country,
	}, params.Code, params.Locale)
	if err != nil {
		return ApplyResult{}, err
	}
	model := ApplyResult{Status: result.Status, Promo: mapPromo(result)}
	if result.Redemption != nil {
		model.Redemption = &RedemptionModel{ID: result.Redemption.ID, RedeemedAt: result.Redemption.RedeemedAt}
	}
	return model, nil
}

func mapPromo(result repository.ApplyResult) PromoModel {
	model := PromoModel{
		ID: result.Promo.ID, Code: result.Promo.Code, Payload: result.Promo.Payload,
		MaxActivations: result.Promo.MaxActivations, ActivationCount: result.Promo.ActivationCount,
		IsActive: result.Promo.IsActive, StartAt: result.Promo.StartAt, EndAt: result.Promo.EndAt,
		Rewards: make([]RewardModel, 0, len(result.Rewards)),
	}
	if result.Localization != nil {
		model.Title = result.Localization.Title
		model.Description = result.Localization.Description
	}
	for _, reward := range result.Rewards {
		model.Rewards = append(model.Rewards, RewardModel{
			Key: reward.Key, Type: reward.Type, Quantity: reward.Quantity, Scale: reward.Scale, Unit: reward.Unit,
		})
	}
	return model
}
