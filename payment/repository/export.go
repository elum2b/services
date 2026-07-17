package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

func (r *PaymentRepository) Export(ctx context.Context, workspaceID string, req ExportRequest) (ExportPackage, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return ExportPackage{}, err
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var groups []ExportProductGroup
	var products []ExportProduct
	var productItems map[string][]ExportProductItem
	var prices map[string][]ExportPrice
	var tonWallets []ExportTONWallet
	if err := r.WithTx(ctx, func(txRepo *PaymentRepository) error {
		if _, err := txRepo.executor.ExecContext(
			ctx,
			"SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY",
		); err != nil {
			return err
		}

		localizations, err := txRepo.exportLocalizations(ctx, workspaceID)
		if err != nil {
			return err
		}

		groups, err = txRepo.exportGroups(ctx, workspaceID, localizations)
		if err != nil {
			return err
		}

		products, err = txRepo.exportProducts(ctx, workspaceID, localizations)
		if err != nil {
			return err
		}

		productItems, err = txRepo.exportProductItems(ctx, workspaceID)
		if err != nil {
			return err
		}

		prices, err = txRepo.exportPrices(ctx, workspaceID)
		if err != nil {
			return err
		}

		tonWallets, err = txRepo.exportTONWallets(ctx, workspaceID)
		return err
	}); err != nil {
		return ExportPackage{}, err
	}
	groupIndex := make(map[string]int, len(groups))
	for index := range groups {
		groupIndex[groups[index].Code] = index
	}
	rootProducts := make([]ExportProduct, 0)
	for _, product := range products {
		product.Items = productItems[product.ID]
		product.Prices = prices[product.ID]
		if product.GroupCode != nil {
			if index, ok := groupIndex[*product.GroupCode]; ok {
				groups[index].Products = append(groups[index].Products, product)
				continue
			}
		}
		rootProducts = append(rootProducts, product)
	}
	return ExportPackage{
		Format:     ExportFormat,
		Service:    "payment",
		CreatedAt:  now.UTC(),
		Groups:     groups,
		Products:   rootProducts,
		TONWallets: tonWallets,
	}, nil
}

func (r *PaymentRepository) exportGroups(
	ctx context.Context,
	workspaceID string,
	localizations map[string]map[string]string,
) ([]ExportProductGroup, error) {
	rows, err := r.q.ExportListProductGroups(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make([]ExportProductGroup, 0, len(rows))
	for _, row := range rows {
		group := ExportProductGroup{
			Code:           row.Code,
			TitleKey:       exportNullStringPtr(row.TitleKey),
			DescriptionKey: exportNullStringPtr(row.DescriptionKey),
			Position:       row.Position,
			IsActive:       row.IsActive,
		}
		group.Localization = exportText(localizations, group.TitleKey, group.DescriptionKey)
		result = append(result, group)
	}
	return result, nil
}

func (r *PaymentRepository) exportProducts(
	ctx context.Context,
	workspaceID string,
	localizations map[string]map[string]string,
) ([]ExportProduct, error) {
	rows, err := r.q.ExportListProducts(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make([]ExportProduct, 0, len(rows))
	for _, row := range rows {
		product := ExportProduct{
			ID:                   row.ID,
			GroupCode:            exportNullStringPtr(row.GroupCode),
			TitleKey:             row.TitleKey,
			DescriptionKey:       exportNullStringPtr(row.DescriptionKey),
			ImageURL:             exportNullStringPtr(row.ImageUrl),
			LinkURL:              exportNullStringPtr(row.LinkUrl),
			SizeLabel:            exportNullStringPtr(row.SizeLabel),
			PeriodSeconds:        exportNullInt64Ptr(row.PeriodSeconds),
			TrialDurationSeconds: exportNullInt64Ptr(row.TrialDurationSeconds),
			QuantityMode:         string(row.QuantityMode),
			Position:             row.Position,
			GlobalLimit:          row.GlobalLimit,
			GlobalInterval:       string(row.GlobalInterval),
			GlobalIntervalCount:  row.GlobalIntervalCount,
			UserLimit:            row.UserLimit,
			UserInterval:         string(row.UserInterval),
			UserIntervalCount:    row.UserIntervalCount,
			AvailableFrom:        row.AvailableFrom,
			AvailableUntil:       row.AvailableUntil,
			IsVisible:            row.IsVisible,
			IsClosed:             row.IsClosed,
		}
		if row.Target.Valid {
			product.Target = append(product.Target[:0], row.Target.RawMessage...)
		}
		product.Localization = exportText(localizations, &product.TitleKey, product.DescriptionKey)
		if len(product.Target) == 0 || string(product.Target) == "null" {
			product.Target = nil
		}
		result = append(result, product)
	}
	return result, nil
}

func (r *PaymentRepository) exportProductItems(
	ctx context.Context,
	workspaceID string,
) (map[string][]ExportProductItem, error) {
	rows, err := r.q.ExportListProductItems(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]ExportProductItem)
	for _, row := range rows {
		if row.Scale < 0 {
			return nil, fmt.Errorf("payment export product %s item %s has negative scale", row.ProductID, row.ItemID)
		}
		item := ExportProductItem{
			ItemID:       row.ItemID,
			RewardType:   string(row.RewardType),
			Quantity:     row.Quantity,
			Scale:        uint16(row.Scale),
			DurationUnit: exportProductItemDurationUnit(row.DurationUnit),
		}
		result[row.ProductID] = append(result[row.ProductID], item)
	}
	return result, nil
}

func (r *PaymentRepository) exportPrices(ctx context.Context, workspaceID string) (map[string][]ExportPrice, error) {
	rows, err := r.q.ExportListPrices(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]ExportPrice)
	for _, row := range rows {
		if row.ID < 0 || row.ListAmountMinor < 0 || row.DiscountAmountMinor < 0 {
			return nil, fmt.Errorf("payment export product %s price contains negative values", row.ProductID)
		}
		price := ExportPrice{
			ID:                           uint64(row.ID),
			AssetCode:                    row.AssetCode,
			ListAmountMinor:              uint64(row.ListAmountMinor),
			DiscountAmountMinor:          uint64(row.DiscountAmountMinor),
			PricingMode:                  string(row.PricingMode),
			ReferenceAssetCode:           exportNullStringPtr(row.ReferenceAssetCode),
			ReferenceListAmountMinor:     exportNullUint64Ptr(row.ReferenceListAmountMinor),
			ReferenceDiscountAmountMinor: exportNullUint64Ptr(row.ReferenceDiscountAmountMinor),
			Coefficient:                  exportNullStringPtr(row.Coefficient),
			IsPromotion:                  row.IsPromotion,
			StartsAt:                     row.StartsAt,
			EndsAt:                       row.EndsAt,
		}
		result[row.ProductID] = append(result[row.ProductID], price)
	}
	return result, nil
}

func (r *PaymentRepository) exportLocalizations(
	ctx context.Context,
	workspaceID string,
) (map[string]map[string]string, error) {
	rows, err := r.q.ExportListLocalizations(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make(map[string]map[string]string)
	for _, row := range rows {
		if result[row.LocalizationKey] == nil {
			result[row.LocalizationKey] = make(map[string]string)
		}
		result[row.LocalizationKey][row.Locale] = row.Value
	}
	return result, nil
}

func (r *PaymentRepository) exportTONWallets(ctx context.Context, workspaceID string) ([]ExportTONWallet, error) {
	rows, err := r.q.ExportListTONWallets(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make([]ExportTONWallet, 0, len(rows))
	for _, row := range rows {
		result = append(result, ExportTONWallet{
			Network:          row.Network,
			WalletAddress:    row.WalletAddress,
			NetworkConfigURL: exportNullStringPtr(row.NetworkConfigUrl),
			IsEnabled:        row.IsEnabled,
		})
	}
	return result, nil
}

func exportProductItemDurationUnit(value paymentsqlc.NullPaymentProductItemDurationUnit) *string {
	if !value.Valid {
		return nil
	}
	unit := string(value.PaymentProductItemDurationUnit)
	return &unit
}

func exportText(
	localizations map[string]map[string]string,
	titleKey *string,
	descriptionKey *string,
) map[string]ExportText {
	if titleKey == nil && descriptionKey == nil {
		return nil
	}
	result := make(map[string]ExportText)
	if titleKey != nil {
		for locale, value := range localizations[*titleKey] {
			text := result[locale]
			text.Title = value
			result[locale] = text
		}
	}
	if descriptionKey != nil {
		for locale, value := range localizations[*descriptionKey] {
			text := result[locale]
			text.Description = value
			result[locale] = text
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func exportNullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func exportNullInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func exportNullUint64Ptr(value sql.NullInt64) *uint64 {
	if !value.Valid || value.Int64 < 0 {
		return nil
	}
	out := uint64(value.Int64)
	return &out
}
