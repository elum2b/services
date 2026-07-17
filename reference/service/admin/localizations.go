package admin

import (
	"context"
	"strings"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/reference/repository"
)

func (a *Admin) UpsertLocalization(ctx context.Context, params SaveLocalizationParams) error {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	params.ItemKey = normalizeKey(params.ItemKey)
	params.Locale = strings.ToLower(strings.TrimSpace(params.Locale))
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return err
	}
	if !itemKeyPattern.MatchString(params.ItemKey) ||
		params.Locale == "" || strings.TrimSpace(params.Title) == "" {
		return ErrLocalizationRequired
	}
	return a.repository.UpsertLocalization(mergedCtx, repository.Localization{
		WorkspaceID: params.WorkspaceID, ItemKey: params.ItemKey, Locale: params.Locale,
		Title: params.Title, Description: params.Description,
	})
}

func (a *Admin) GetLocalization(ctx context.Context, workspaceID, key, locale string) (LocalizationModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	value, err := a.repository.GetLocalization(
		mergedCtx, workspaceID, normalizeKey(key),
		strings.ToLower(strings.TrimSpace(locale)),
	)
	if err != nil {
		return LocalizationModel{}, err
	}
	return mapLocalization(value), nil
}

func (a *Admin) ListLocalizations(ctx context.Context, workspaceID, key string) ([]LocalizationModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	values, err := a.repository.ListLocalizations(mergedCtx, workspaceID, normalizeKey(key))
	if err != nil {
		return nil, err
	}
	result := make([]LocalizationModel, 0, len(values))
	for _, value := range values {
		result = append(result, mapLocalization(value))
	}
	return result, nil
}

func (a *Admin) DeleteLocalization(ctx context.Context, workspaceID, key, locale string) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.DeleteLocalization(
		mergedCtx, workspaceID, normalizeKey(key),
		strings.ToLower(strings.TrimSpace(locale)),
	)
}

func mapLocalization(value repository.Localization) LocalizationModel {
	return LocalizationModel{
		Locale: value.Locale, Title: value.Title, Description: value.Description,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}
