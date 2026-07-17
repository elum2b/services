package admin

import (
	"context"
	json "github.com/goccy/go-json"
	"regexp"
	"strings"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/reference/repository"
)

var itemKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)

func (a *Admin) CreateItem(ctx context.Context, params SaveItemParams) error {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	params.Key = normalizeKey(params.Key)
	if err := validateItem(params.WorkspaceID, params.Key, params.Type, params.Payload); err != nil {
		return err
	}
	return a.repository.CreateItem(mergedCtx, repository.SaveItemParams(params))
}

func (a *Admin) UpdateItem(ctx context.Context, params UpdateItemParams) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	params.Key = normalizeKey(params.Key)
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return 0, err
	}
	if !itemKeyPattern.MatchString(params.Key) {
		return 0, ErrItemScopeInvalid
	}
	if len(params.Payload) == 0 || !json.Valid(params.Payload) {
		return 0, ErrItemPayloadInvalid
	}
	return a.repository.UpdateItem(mergedCtx, repository.SaveItemParams{
		WorkspaceID: params.WorkspaceID, Key: params.Key,
		Payload: params.Payload, IsActive: params.IsActive,
	})
}

func (a *Admin) DangerousChangeType(ctx context.Context, params DangerousChangeTypeParams) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	if params.Confirmation != DangerousTypeConfirmation {
		return 0, ErrTypeChangeNotConfirmed
	}
	params.Key = normalizeKey(params.Key)
	if err := validateIdentity(params.WorkspaceID, params.Key, params.CurrentType); err != nil {
		return 0, err
	}
	if !validType(params.NewType) {
		return 0, ErrItemTypeInvalid
	}
	return a.repository.DangerousChangeType(mergedCtx, repository.DangerousChangeTypeParams{
		WorkspaceID: params.WorkspaceID, Key: params.Key,
		CurrentType: params.CurrentType, NewType: params.NewType,
	})
}

func (a *Admin) GetItem(ctx context.Context, workspaceID, key string) (ItemModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	item, err := a.repository.AdminGetItem(mergedCtx, workspaceID, normalizeKey(key))
	if err != nil {
		return ItemModel{}, err
	}
	return mapItem(item), nil
}

func (a *Admin) ListItems(ctx context.Context, params ItemListParams) ([]ItemModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	if params.Type != "" && !validType(params.Type) {
		return nil, ErrItemTypeFilterInvalid
	}
	limit, offset := normalizePage(params.Page)
	items, err := a.repository.AdminListItems(mergedCtx, repository.ListItemsParams{
		WorkspaceID: params.WorkspaceID, Type: params.Type,
		OnlyNotDeleted: params.OnlyNotDeleted, Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, err
	}
	result := make([]ItemModel, 0, len(items))
	for _, item := range items {
		result = append(result, mapItem(item))
	}
	return result, nil
}

func (a *Admin) SoftDeleteItem(ctx context.Context, workspaceID, key string) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.SoftDeleteItem(mergedCtx, workspaceID, normalizeKey(key))
}

func (a *Admin) RestoreItem(ctx context.Context, workspaceID, key string, active bool) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.RestoreItem(mergedCtx, workspaceID, normalizeKey(key), active)
}

func validateItem(workspaceID, key, itemType string, payload json.RawMessage) error {
	if err := validateIdentity(workspaceID, key, itemType); err != nil {
		return err
	}
	if len(payload) == 0 || !json.Valid(payload) {
		return ErrItemPayloadInvalid
	}
	return nil
}

func validateIdentity(workspaceID, key, itemType string) error {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return err
	}
	if !itemKeyPattern.MatchString(key) {
		return ErrItemScopeInvalid
	}
	if !validType(itemType) {
		return ErrItemTypeInvalid
	}
	return nil
}

func validType(value string) bool {
	return value == repository.ItemTypeQuantity || value == repository.ItemTypeDuration
}

func normalizeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func mapItem(item repository.Item) ItemModel {
	result := ItemModel{
		Key: item.Key, Type: item.Type, Payload: item.Payload, IsActive: item.IsActive,
		DeletedAt: item.DeletedAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		Localizations: make([]LocalizationModel, 0, len(item.Localizations)),
	}
	for _, localization := range item.Localizations {
		result.Localizations = append(result.Localizations, LocalizationModel{
			Locale: localization.Locale, Title: localization.Title,
			Description: localization.Description,
			CreatedAt:   localization.CreatedAt, UpdatedAt: localization.UpdatedAt,
		})
	}
	return result
}
