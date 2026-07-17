package admin

import (
	"github.com/elum2b/services/cpa/repository"
	"github.com/elum2b/services/cpa/service/user"
)

func mapOffer(value repository.Offer, localizations []repository.Localization, rewards []repository.Reward) OfferModel {
	result := OfferModel{
		ID:                value.ID,
		Payload:           value.Payload,
		Target:            value.Target,
		CodeMode:          value.CodeMode,
		CodeSource:        value.CodeSource,
		SharedCode:        value.SharedCode,
		GeneratedLength:   value.GeneratedLength,
		GeneratedAlphabet: value.GeneratedAlphabet,
		IsActive:          value.IsActive,
		StartAt:           value.StartAt,
		EndAt:             value.EndAt,
		CreatedAt:         value.CreatedAt,
		UpdatedAt:         value.UpdatedAt,
		Localizations:     make([]LocalizationModel, 0, len(localizations)),
		Rewards:           make([]user.RewardModel, 0, len(rewards)),
	}
	for _, localization := range localizations {
		result.Localizations = append(result.Localizations, LocalizationModel{
			Locale:      localization.Locale,
			Title:       localization.Title,
			Description: localization.Description,
		})
	}
	for _, reward := range rewards {
		result.Rewards = append(result.Rewards, user.RewardModel{
			Key:      reward.Key,
			Type:     reward.Type,
			Quantity: reward.Quantity,
			Scale:    reward.Scale,
			Unit:     reward.Unit,
		})
	}
	return result
}

func normalizePage(page Page) (int32, int32) {
	limit := page.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := page.Offset
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
