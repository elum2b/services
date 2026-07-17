package admin

import (
	"context"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/promo/repository"
)

type SaveLocalizationParams struct {
	WorkspaceID string
	PromoID     uint64
	Locale      string
	Title       string
	Description string
}

func (a *Admin) UpsertLocalization(ctx context.Context, params SaveLocalizationParams) error {
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return err
	}

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	if params.Locale == "" || params.Title == "" {
		return ErrLocalizationRequired
	}
	return a.repository.UpsertLocalization(mergedCtx, repository.Localization(params))
}

func (a *Admin) GetLocalization(
	ctx context.Context,
	workspaceID string,
	promoID uint64,
	locale string,
) (LocalizationModel, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return LocalizationModel{}, err
	}

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	value, err := a.repository.GetLocalization(mergedCtx, workspaceID, promoID, locale)
	if err != nil {
		return LocalizationModel{}, err
	}
	return LocalizationModel{Locale: value.Locale, Title: value.Title, Description: value.Description}, nil
}

func (a *Admin) ListLocalizations(
	ctx context.Context,
	workspaceID string,
	promoID uint64,
) ([]LocalizationModel, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	values, err := a.repository.ListLocalizations(mergedCtx, workspaceID, promoID)
	if err != nil {
		return nil, err
	}
	result := make([]LocalizationModel, 0, len(values))
	for _, value := range values {
		result = append(
			result,
			LocalizationModel{Locale: value.Locale, Title: value.Title, Description: value.Description},
		)
	}
	return result, nil
}

func (a *Admin) DeleteLocalization(
	ctx context.Context,
	workspaceID string,
	promoID uint64,
	locale string,
) (int64, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return 0, err
	}

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.DeleteLocalization(mergedCtx, workspaceID, promoID, locale)
}
