package admin

import (
	"context"

	"github.com/elum2b/services/cpa/repository"
)

type UpsertLocalizationParams struct {
	WorkspaceID string
	CPAID       string
	Locale      string
	Title       string
	Description string
}

func (a *Admin) UpsertLocalization(ctx context.Context, params UpsertLocalizationParams) error {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.UpsertLocalization(mergedCtx, repository.Localization{
		WorkspaceID: params.WorkspaceID,
		CPAID:       params.CPAID,
		Locale:      params.Locale,
		Title:       params.Title,
		Description: params.Description,
	})

}

func (a *Admin) ListLocalizations(ctx context.Context, workspaceID, cpaID string) ([]LocalizationModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	values, err := a.repository.ListLocalizations(mergedCtx, workspaceID, cpaID)
	if err != nil {
		return nil, err
	}

	result := make([]LocalizationModel, 0, len(values))
	for _, value := range values {
		result = append(result, LocalizationModel{
			Locale:      value.Locale,
			Title:       value.Title,
			Description: value.Description,
		})
	}

	return result, nil

}

func (a *Admin) DeleteLocalization(ctx context.Context, workspaceID, cpaID, locale string) (int64, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.DeleteLocalization(mergedCtx, workspaceID, cpaID, locale)

}
