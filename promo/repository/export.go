package repository

import (
	"context"
	"time"

	promosqlc "github.com/elum2b/services/promo/sqlc"
)

func (r *Repository) Export(ctx context.Context, workspaceID string, req ExportRequest) (ExportPackage, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return ExportPackage{}, err
	}

	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var promoRows []promosqlc.PromoOffer
	var localizationRows []promosqlc.PromoLocalization
	var rewardRows []promosqlc.PromoReward
	if err := r.WithTx(ctx, func(txRepo *Repository) error {
		if _, err := txRepo.executor.ExecContext(
			ctx,
			"SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY",
		); err != nil {
			return err
		}

		var err error
		promoRows, err = txRepo.q.ListExportPromos(ctx, workspaceID)
		if err != nil {
			return err
		}

		localizationRows, err = txRepo.q.ListExportLocalizations(ctx, workspaceID)
		if err != nil {
			return err
		}

		rewardRows, err = txRepo.q.ListExportRewards(ctx, workspaceID)
		return err
	}); err != nil {
		return ExportPackage{}, err
	}
	out := ExportPackage{
		Format:    ExportFormat,
		Service:   "promo",
		CreatedAt: now.UTC(),
		Promos:    make([]ExportPromo, 0, len(promoRows)),
	}
	promoIndexByID := make(map[int64]int, len(promoRows))
	for _, row := range promoRows {
		promo := mapPromo(row)
		item := ExportPromo{
			Code:           promo.Code,
			Payload:        promo.Payload,
			Target:         nullableJSON(promo.Target),
			MaxActivations: promo.MaxActivations,
			IsActive:       promo.IsActive,
			StartAt:        promo.StartAt,
			EndAt:          promo.EndAt,
			Localization:   make(map[string]ExportText),
			Rewards:        make([]ExportReward, 0),
		}
		promoIndexByID[row.ID] = len(out.Promos)
		out.Promos = append(out.Promos, item)
	}

	for _, localization := range localizationRows {
		index, ok := promoIndexByID[localization.PromoID]
		if !ok {
			continue
		}
		out.Promos[index].Localization[localization.Locale] = ExportText{
			Title:       localization.Title,
			Description: localization.Description,
		}
	}

	for _, reward := range rewardRows {
		index, ok := promoIndexByID[reward.PromoID]
		if !ok {
			continue
		}
		out.Promos[index].Rewards = append(out.Promos[index].Rewards, ExportReward{
			Key:      reward.RewardKey,
			Type:     string(reward.RewardType),
			Quantity: reward.Quantity,
			Scale:    uint16(reward.Scale),
			Unit:     promoDurationUnitPtr(reward.DurationUnit),
		})
	}

	for index := range out.Promos {
		if len(out.Promos[index].Localization) == 0 {
			out.Promos[index].Localization = nil
		}
		if len(out.Promos[index].Rewards) == 0 {
			out.Promos[index].Rewards = nil
		}
	}

	return out, nil
}

func nullableJSON(value []byte) []byte {
	if len(value) == 0 || string(value) == "null" {
		return nil
	}
	return value
}
