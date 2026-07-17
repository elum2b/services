package admin

import (
	"context"
	json "github.com/goccy/go-json"
	"math"
	"strings"
	"time"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/internal/utils/target"
	"github.com/elum2b/services/promo/repository"
	"github.com/elum2b/services/promo/service/user"
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

func (a *Admin) CreatePromo(ctx context.Context, params SavePromoParams) (uint64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	if err := validatePromo(params); err != nil {
		return 0, err
	}
	return a.repository.CreatePromo(mergedCtx, repository.SavePromoParams(params))
}

func (a *Admin) UpdatePromo(ctx context.Context, params SavePromoParams) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	if params.ID == 0 {
		return 0, ErrPromoIDRequired
	}
	if params.ID > math.MaxInt64 {
		return 0, ErrPromoNumberOutOfRange
	}
	if err := validatePromo(params); err != nil {
		return 0, err
	}
	return a.repository.UpdatePromo(mergedCtx, repository.SavePromoParams(params))
}

func (a *Admin) GetPromo(ctx context.Context, workspaceID string, id uint64) (PromoModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return PromoModel{}, err
	}
	if id == 0 {
		return PromoModel{}, ErrPromoScopeRequired
	}
	if id > math.MaxInt64 {
		return PromoModel{}, ErrPromoNumberOutOfRange
	}

	promo, err := a.repository.GetPromo(mergedCtx, workspaceID, id)
	if err != nil {
		return PromoModel{}, err
	}
	localizations, err := a.repository.ListLocalizations(mergedCtx, workspaceID, id)
	if err != nil {
		return PromoModel{}, err
	}
	rewards, err := a.repository.ListRewards(mergedCtx, workspaceID, id)
	if err != nil {
		return PromoModel{}, err
	}
	return mapPromo(promo, localizations, rewards), nil
}

func (a *Admin) ListPromos(ctx context.Context, workspaceID string, page Page) ([]PromoModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	limit, offset := normalizePage(page)
	values, err := a.repository.ListPromos(mergedCtx, workspaceID, limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]PromoModel, 0, len(values))
	for _, value := range values {
		result = append(result, mapPromo(value, nil, nil))
	}
	return result, nil
}

func (a *Admin) DeletePromo(ctx context.Context, workspaceID string, id uint64) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, ErrPromoScopeRequired
	}
	if id > math.MaxInt64 {
		return 0, ErrPromoNumberOutOfRange
	}

	return a.repository.SoftDeletePromo(mergedCtx, workspaceID, id)
}

func validatePromo(params SavePromoParams) error {
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return err
	}
	if strings.TrimSpace(params.Code) == "" {
		return ErrPromoScopeRequired
	}
	if len(params.Payload) == 0 || !json.Valid(params.Payload) {
		return ErrPromoPayloadInvalid
	}
	if params.MaxActivations > math.MaxInt64 {
		return ErrPromoNumberOutOfRange
	}
	if err := target.Validate(params.Target); err != nil {
		return ErrPromoPayloadInvalid
	}
	if params.StartAt != nil && params.EndAt != nil && !params.StartAt.Before(*params.EndAt) {
		return ErrPromoRangeInvalid
	}
	return nil
}

func mapPromo(value repository.Promo, localizations []repository.Localization, rewards []repository.Reward) PromoModel {
	result := PromoModel{
		ID: value.ID, Code: value.Code, Payload: value.Payload, Target: value.Target, MaxActivations: value.MaxActivations,
		ActivationCount: value.ActivationCount, IsActive: value.IsActive, StartAt: value.StartAt,
		EndAt: value.EndAt, DeletedAt: value.DeletedAt, CreatedAt: value.CreatedAt,
		UpdatedAt: value.UpdatedAt, Localizations: make([]LocalizationModel, 0, len(localizations)),
		Rewards: make([]user.RewardModel, 0, len(rewards)),
	}
	for _, item := range localizations {
		result.Localizations = append(result.Localizations, LocalizationModel{
			Locale: item.Locale, Title: item.Title, Description: item.Description,
		})
	}
	for _, reward := range rewards {
		result.Rewards = append(result.Rewards, user.RewardModel{
			Key: reward.Key, Type: reward.Type, Quantity: reward.Quantity, Scale: reward.Scale, Unit: reward.Unit,
		})
	}
	return result
}
