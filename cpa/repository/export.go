package repository

import (
	"context"
	"time"

	services "github.com/elum2b/services"
)

func (r *Repository) Export(ctx context.Context, workspaceID string, req ExportRequest) (ExportPackage, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return ExportPackage{}, err
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var bundles []OfferBundle
	if err := r.WithReadOnlySnapshot(ctx, func(txRepo *Repository) error {
		var err error
		bundles, err = txRepo.ListAllOfferBundles(ctx, workspaceID)
		return err
	}); err != nil {
		return ExportPackage{}, err
	}
	out := ExportPackage{
		Format:    ExportFormat,
		Service:   "cpa",
		CreatedAt: now.UTC(),
		Offers:    make([]ExportOffer, 0, len(bundles)),
	}
	for _, bundle := range bundles {
		offer := ExportOffer{
			ID:                bundle.Offer.ID,
			Payload:           bundle.Offer.Payload,
			Target:            nullableJSON(bundle.Offer.Target),
			CodeMode:          bundle.Offer.CodeMode,
			CodeSource:        bundle.Offer.CodeSource,
			SharedCode:        bundle.Offer.SharedCode,
			GeneratedLength:   bundle.Offer.GeneratedLength,
			GeneratedAlphabet: bundle.Offer.GeneratedAlphabet,
			IsActive:          bundle.Offer.IsActive,
			StartAt:           bundle.Offer.StartAt,
			EndAt:             bundle.Offer.EndAt,
			Localization:      make(map[string]ExportText, len(bundle.Localizations)),
			Rewards:           make([]ExportReward, 0, len(bundle.Rewards)),
		}
		for _, localization := range bundle.Localizations {
			offer.Localization[localization.Locale] = ExportText{
				Title:       localization.Title,
				Description: localization.Description,
			}
		}
		for _, reward := range bundle.Rewards {
			offer.Rewards = append(offer.Rewards, ExportReward{
				Key:      reward.Key,
				Type:     reward.Type,
				Quantity: reward.Quantity,
				Scale:    reward.Scale,
				Unit:     reward.Unit,
			})
		}
		out.Offers = append(out.Offers, offer)
	}
	return out, nil
}

func nullableJSON(value []byte) []byte {
	if len(value) == 0 || string(value) == "null" {
		return nil
	}
	return value
}
