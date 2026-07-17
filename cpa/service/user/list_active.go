package user

import (
	"context"

	"github.com/elum2b/services/internal/utils/target"
)

type ListActiveParams struct {
	Identity Identity
	Locale   string
}

func (u *User) ListActive(ctx context.Context, params ListActiveParams) ([]OfferModel, error) {

	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()

	if err := params.Identity.Validate(); err != nil {
		return nil, err
	}

	bundles, err := u.repository.ListActiveForUser(mergedCtx, scope(params.Identity, ""), params.Locale)
	if err != nil {
		return nil, err
	}

	result := make([]OfferModel, 0, len(bundles))
	for _, bundle := range bundles {
		offer := bundle.Offer
		if !target.Match(offer.Target, target.Context{
			IsPremium:  params.Identity.IsPremium,
			Sex:        params.Identity.Sex,
			Country:    params.Identity.Country,
			Locale:     params.Locale,
			Platform:   params.Identity.Platform,
			PlatformID: params.Identity.PlatformID,
		}) {
			continue
		}
		model := OfferModel{
			ID:       offer.ID,
			Payload:  offer.Payload,
			CodeMode: offer.CodeMode,
			StartAt:  offer.StartAt,
			EndAt:    offer.EndAt,
			Rewards:  mapRewards(bundle.Rewards),
		}
		if bundle.Localization != nil {
			model.Title = bundle.Localization.Title
			model.Description = bundle.Localization.Description
		}
		if bundle.Assignment != nil {
			mapped := mapAssignment(*bundle.Assignment)
			model.Assignment = &mapped
		}
		result = append(result, model)
	}

	return result, nil

}
