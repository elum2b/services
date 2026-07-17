package user

import (
	"context"
	"strings"

	"github.com/elum2b/services/reference/repository"
)

func (u *User) Get(ctx context.Context, params GetParams) (ItemModel, error) {
	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()
	item, err := u.repository.Get(
		mergedCtx, params.WorkspaceID,
		normalizeKey(params.Key), normalizeLocale(params.Locale),
	)
	if err != nil {
		return ItemModel{}, err
	}
	return mapItem(item), nil
}

func (u *User) Resolve(ctx context.Context, params ResolveParams) (ResolveResult, error) {
	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()
	keys := normalizeKeys(params.Keys)
	if len(keys) == 0 {
		return ResolveResult{}, ErrKeysRequired
	}
	if len(keys) > 1000 {
		return ResolveResult{}, ErrTooManyKeys
	}
	items, err := u.repository.Resolve(
		mergedCtx, params.WorkspaceID, keys, normalizeLocale(params.Locale),
	)
	if err != nil {
		return ResolveResult{}, err
	}
	result := ResolveResult{Items: make([]ItemModel, 0, len(items))}
	found := make(map[string]struct{}, len(items))
	for _, item := range items {
		result.Items = append(result.Items, mapItem(item))
		found[item.Key] = struct{}{}
	}
	for _, key := range keys {
		if _, ok := found[key]; !ok {
			result.MissingKeys = append(result.MissingKeys, key)
		}
	}
	return result, nil
}

func (u *User) List(ctx context.Context, params ListParams) ([]ItemModel, error) {
	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()
	limit, offset := normalizePage(params.Page)
	items, err := u.repository.List(
		mergedCtx, params.WorkspaceID, normalizeLocale(params.Locale), limit, offset,
	)
	if err != nil {
		return nil, err
	}
	result := make([]ItemModel, 0, len(items))
	for _, item := range items {
		result = append(result, mapItem(item))
	}
	return result, nil
}

func mapItem(item repository.Item) ItemModel {
	result := ItemModel{
		Key: item.Key, Type: item.Type, Payload: item.Payload,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
	if item.Localization != nil {
		result.Localization = &LocalizationModel{
			Locale: item.Localization.Locale, Title: item.Localization.Title,
			Description: item.Localization.Description,
		}
	}
	return result
}

func normalizeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeLocale(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "en"
	}
	return value
}

func normalizeKeys(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		key := normalizeKey(value)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result
}
