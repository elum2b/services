package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	importexport "github.com/elum2b/services/internal/utils/importexport"
	"github.com/elum2b/services/internal/utils/target"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

func (r *PaymentRepository) PreviewImport(
	ctx context.Context,
	workspaceID string,
	pkg ExportPackage,
) (ImportPreview, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return ImportPreview{}, err
	}
	pkg = normalizeExportPackage(pkg)
	if err := validateExportPackage(pkg); err != nil {
		return ImportPreview{}, err
	}
	preview := ImportPreview{Format: pkg.Format, Service: pkg.Service, Counts: countPackage(pkg)}
	existing, err := r.importExistingKeys(ctx, workspaceID)
	if err != nil {
		return ImportPreview{}, err
	}
	for _, group := range pkg.Groups {
		if existing.groups[group.Code] {
			preview.Conflicts = append(preview.Conflicts, ImportConflict{Type: "group", Key: group.Code})
		}
		for _, product := range group.Products {
			if existing.products[product.ID] {
				preview.Conflicts = append(preview.Conflicts, ImportConflict{Type: "product", Key: product.ID})
			}
		}
	}
	for _, product := range pkg.Products {
		if existing.products[product.ID] {
			preview.Conflicts = append(preview.Conflicts, ImportConflict{Type: "product", Key: product.ID})
		}
	}
	if existing.tonWallet && len(pkg.TONWallets) > 0 {
		preview.Conflicts = append(preview.Conflicts, ImportConflict{Type: "ton_wallet", Key: "default"})
	}
	return preview, nil
}

func (r *PaymentRepository) Import(ctx context.Context, workspaceID string, req ImportRequest) (ImportResult, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return ImportResult{}, err
	}
	req.Package = normalizeExportPackage(req.Package)
	if err := validateExportPackage(req.Package); err != nil {
		return ImportResult{}, err
	}
	strategy := req.ConflictStrategy
	if strategy == "" {
		strategy = ImportConflictFail
	}
	if strategy != ImportConflictFail && strategy != ImportConflictSkip && strategy != ImportConflictUpdate {
		return ImportResult{}, fmt.Errorf("unsupported import conflict strategy: %s", strategy)
	}
	result := ImportResult{}
	err = r.WithTx(ctx, func(txRepo *PaymentRepository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}

		preview, err := txRepo.PreviewImport(ctx, workspaceID, req.Package)
		if err != nil {
			return err
		}
		if strategy == ImportConflictFail && len(preview.Conflicts) > 0 {
			return fmt.Errorf("import conflicts found: %d", len(preview.Conflicts))
		}

		if err := txRepo.importBulk(ctx, workspaceID, req.Package, strategy, preview, &result); err != nil {
			return err
		}
		if _, err := txRepo.q.DeleteWorkspaceProductCache(ctx, workspaceID); err != nil {
			return err
		}
		return txRepo.q.RebuildWorkspaceProductCache(ctx, paymentsqlc.RebuildWorkspaceProductCacheParams{
			WorkspaceID:   workspaceID,
			WorkspaceID_2: workspaceID,
		})
	})
	if err != nil {
		return ImportResult{}, err
	}
	return result, r.invalidateWorkspaceCache(workspaceID)
}

func (r *PaymentRepository) lockWorkspaceMutation(ctx context.Context, workspaceID string) error {
	if _, err := requireWorkspaceID(workspaceID); err != nil {
		return err
	}

	_, err := r.executor.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		"payment:"+workspaceID,
	)
	return err
}

func (r *PaymentRepository) withWorkspaceMutation(
	ctx context.Context,
	workspaceID string,
	fn func(*PaymentRepository) error,
) error {
	return r.inTransaction(ctx, func(txRepo *PaymentRepository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}

		return fn(txRepo)
	})
}

func (r *PaymentRepository) importBulk(
	ctx context.Context,
	workspaceID string,
	pkg ExportPackage,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	products := flattenProducts(pkg)
	if err := r.resolveImportedDynamicPrices(ctx, products, strategy, preview); err != nil {
		return err
	}

	if err := r.importGroupsBulk(ctx, workspaceID, pkg.Groups, strategy, preview, result); err != nil {
		return err
	}
	if err := r.importProductsBulk(ctx, workspaceID, products, strategy, preview, result); err != nil {
		return err
	}
	if err := r.replaceImportedPaymentChildren(
		ctx,
		workspaceID,
		pkg,
		products,
		strategy,
		preview,
	); err != nil {
		return err
	}
	if err := r.importLocalizationsBulk(ctx, workspaceID, pkg, strategy, preview, result); err != nil {
		return err
	}
	if err := r.importProductItemsBulk(ctx, workspaceID, products, strategy, preview, result); err != nil {
		return err
	}
	if err := r.importPricesBulk(ctx, workspaceID, products, strategy, preview, result); err != nil {
		return err
	}
	return r.importTONWalletsBulk(ctx, workspaceID, pkg.TONWallets, strategy, preview, result)
}

type importedAssetRateKey struct {
	assetCode          string
	referenceAssetCode string
}

type importedAssetRate struct {
	referencePerAssetMinor uint64
	targetScale            uint16
}

func (r *PaymentRepository) resolveImportedDynamicPrices(
	ctx context.Context,
	products []ExportProduct,
	strategy string,
	preview ImportPreview,
) error {
	requested := make(map[importedAssetRateKey]struct{})
	assetCodes := make([]string, 0)
	referenceAssetCodes := make([]string, 0)

	for _, product := range products {
		if previewHasConflict(preview, "product", product.ID) && strategy == ImportConflictSkip {
			continue
		}

		for _, price := range product.Prices {
			if defaultString(price.PricingMode, PricingModeFixed) != PricingModeDynamic {
				continue
			}
			if price.ReferenceAssetCode == nil {
				return ErrInvalidPrice
			}

			key := importedAssetRateKey{
				assetCode:          strings.TrimSpace(price.AssetCode),
				referenceAssetCode: strings.TrimSpace(*price.ReferenceAssetCode),
			}
			if _, exists := requested[key]; exists {
				continue
			}

			requested[key] = struct{}{}
			assetCodes = append(assetCodes, key.assetCode)
			referenceAssetCodes = append(referenceAssetCodes, key.referenceAssetCode)
		}
	}
	if len(requested) == 0 {
		return nil
	}

	rows, err := r.q.ListAssetRatesForPricing(ctx, paymentsqlc.ListAssetRatesForPricingParams{
		AssetCodes:          assetCodes,
		ReferenceAssetCodes: referenceAssetCodes,
	})
	if err != nil {
		return err
	}

	rates := make(map[importedAssetRateKey]importedAssetRate, len(rows))
	for _, row := range rows {
		if row.ReferencePerAssetMinor <= 0 || row.TargetScale < 0 {
			return ErrInvalidAssetRate
		}

		rates[importedAssetRateKey{
			assetCode:          row.AssetCode,
			referenceAssetCode: row.ReferenceAssetCode,
		}] = importedAssetRate{
			referencePerAssetMinor: uint64(row.ReferencePerAssetMinor),
			targetScale:            uint16(row.TargetScale),
		}
	}

	for productIndex := range products {
		product := &products[productIndex]
		if previewHasConflict(preview, "product", product.ID) && strategy == ImportConflictSkip {
			continue
		}

		for priceIndex := range product.Prices {
			price := &product.Prices[priceIndex]
			if defaultString(price.PricingMode, PricingModeFixed) != PricingModeDynamic {
				continue
			}
			if price.ReferenceAssetCode == nil || price.ReferenceListAmountMinor == nil ||
				price.ReferenceDiscountAmountMinor == nil || price.Coefficient == nil {
				return ErrInvalidPrice
			}

			key := importedAssetRateKey{
				assetCode:          strings.TrimSpace(price.AssetCode),
				referenceAssetCode: strings.TrimSpace(*price.ReferenceAssetCode),
			}
			rate, exists := rates[key]
			if !exists {
				return fmt.Errorf(
					"payment import product %q price %q: %w",
					product.ID,
					price.AssetCode,
					ErrAssetRateNotFound,
				)
			}

			list, err := convertReferenceAmount(
				*price.ReferenceListAmountMinor,
				rate.targetScale,
				rate.referencePerAssetMinor,
				*price.Coefficient,
			)
			if err != nil {
				return err
			}
			discount, err := convertReferenceAmount(
				*price.ReferenceDiscountAmountMinor,
				rate.targetScale,
				rate.referencePerAssetMinor,
				*price.Coefficient,
			)
			if err != nil {
				return err
			}

			price.ListAmountMinor = list
			price.DiscountAmountMinor = discount
		}
	}

	return nil
}

func (r *PaymentRepository) replaceImportedPaymentChildren(
	ctx context.Context,
	workspaceID string,
	pkg ExportPackage,
	products []ExportProduct,
	strategy string,
	preview ImportPreview,
) error {
	if strategy != ImportConflictUpdate {
		return nil
	}

	productIDs := make([]string, 0, len(products))
	localizationKeys := make([]string, 0)
	appendLocalizationKeys := func(titleKey string, descriptionKey *string) {
		if titleKey != "" {
			localizationKeys = append(localizationKeys, titleKey)
		}
		if descriptionKey != nil && *descriptionKey != "" {
			localizationKeys = append(localizationKeys, *descriptionKey)
		}
	}
	for _, group := range pkg.Groups {
		if previewHasConflict(preview, "group", group.Code) {
			titleKey := ""
			if group.TitleKey != nil {
				titleKey = *group.TitleKey
			}
			appendLocalizationKeys(titleKey, group.DescriptionKey)
		}
	}
	for _, product := range products {
		if previewHasConflict(preview, "product", product.ID) {
			productIDs = append(productIDs, product.ID)
			appendLocalizationKeys(product.TitleKey, product.DescriptionKey)
		}
	}

	if len(productIDs) > 0 {
		for _, table := range []string{"payment_product_item", "payment_price"} {
			if _, err := r.executor.ExecContext(
				ctx,
				"DELETE FROM "+table+" WHERE workspace_id = $1 AND product_id = ANY($2::text[])",
				workspaceID,
				productIDs,
			); err != nil {
				return err
			}
		}
	}
	if len(localizationKeys) == 0 {
		return nil
	}
	_, err := r.executor.ExecContext(
		ctx,
		`DELETE FROM payment_localization
WHERE workspace_id = $1
  AND localization_key = ANY($2::text[])`,
		workspaceID,
		localizationKeys,
	)
	return err
}

func (r *PaymentRepository) importGroupsBulk(
	ctx context.Context,
	workspaceID string,
	groups []ExportProductGroup,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0, len(groups))
	for _, group := range groups {
		if previewHasConflict(preview, "group", group.Code) && strategy == ImportConflictSkip {
			result.Skipped.Groups++
			continue
		}
		rows = append(
			rows,
			[]any{
				workspaceID,
				group.Code,
				nullableString(group.TitleKey),
				nullableString(group.DescriptionKey),
				group.Position,
				group.IsActive,
			},
		)
		result.Imported.Groups++
	}
	return r.execImportBulk(ctx, "payment_product_group",
		[]string{"workspace_id", "code", "title_key", "description_key", "position", "is_active"},
		rows,
		"(workspace_id, code)",
		"title_key = EXCLUDED.title_key, description_key = EXCLUDED.description_key, position = EXCLUDED.position, "+
			"is_active = EXCLUDED.is_active, updated_at = now()",
		strategy,
	)
}

func (r *PaymentRepository) importLocalizationsBulk(
	ctx context.Context,
	workspaceID string,
	pkg ExportPackage,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rowsByKey := make(map[string][]any)
	addText := func(entityType, entityKey string, titleKey string, descriptionKey *string, localization map[string]ExportText) {
		if previewHasConflict(preview, entityType, entityKey) && strategy == ImportConflictSkip {
			return
		}
		for locale, text := range localization {
			if titleKey != "" {
				rowsByKey[locale+"\x00"+titleKey] = []any{workspaceID, locale, titleKey, text.Title}
			}
			if descriptionKey != nil {
				rowsByKey[locale+"\x00"+*descriptionKey] = []any{workspaceID, locale, *descriptionKey, text.Description}
			}
		}
	}
	for _, group := range pkg.Groups {
		titleKey := ""
		if group.TitleKey != nil {
			titleKey = *group.TitleKey
		}
		addText("group", group.Code, titleKey, group.DescriptionKey, group.Localization)
		for _, product := range group.Products {
			addText("product", product.ID, product.TitleKey, product.DescriptionKey, product.Localization)
		}
	}
	for _, product := range pkg.Products {
		addText("product", product.ID, product.TitleKey, product.DescriptionKey, product.Localization)
	}
	rows := make([][]any, 0, len(rowsByKey))
	for _, row := range rowsByKey {
		rows = append(rows, row)
		result.Imported.Localizations++
	}
	return r.execImportBulk(ctx, "payment_localization",
		[]string{"workspace_id", "locale", "localization_key", "value"},
		rows,
		"(workspace_id, locale, localization_key)",
		"value = EXCLUDED.value, updated_at = now()",
		strategy,
	)
}

func (r *PaymentRepository) importProductsBulk(
	ctx context.Context,
	workspaceID string,
	products []ExportProduct,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0, len(products))
	for _, product := range products {
		if previewHasConflict(preview, "product", product.ID) && strategy == ImportConflictSkip {
			result.Skipped.Products++
			continue
		}
		rows = append(rows, []any{
			workspaceID, product.ID, nullableString(product.GroupCode), product.TitleKey,
			nullableString(product.DescriptionKey), defaultJSON(product.Target, "null"),
			nullableString(product.ImageURL), nullableString(product.LinkURL), nullableString(product.SizeLabel),
			nullableInt64(product.PeriodSeconds), nullableInt64(product.TrialDurationSeconds),
			defaultString(product.QuantityMode, "fixed"), product.Position, product.GlobalLimit,
			defaultString(product.GlobalInterval, "UNLIMITED"), product.GlobalIntervalCount,
			product.UserLimit, defaultString(product.UserInterval, "UNLIMITED"), product.UserIntervalCount,
			defaultTime(product.AvailableFrom, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)),
			defaultTime(product.AvailableUntil, time.Date(2124, 1, 1, 0, 0, 0, 0, time.UTC)),
			product.IsVisible, product.IsClosed,
		})
		result.Imported.Products++
	}
	return r.execImportBulk(
		ctx,
		"payment_product",
		[]string{
			"workspace_id", "id", "group_code", "title_key", "description_key", "target", "image_url",
			"link_url", "size_label", "period_seconds", "trial_duration_seconds", "quantity_mode",
			"position", "global_limit", "global_interval", "global_interval_count", "user_limit",
			"user_interval", "user_interval_count", "available_from", "available_until", "is_visible", "is_closed",
		},
		rows,
		"(workspace_id, id)",
		"group_code = EXCLUDED.group_code, title_key = EXCLUDED.title_key, description_key = EXCLUDED.description_key, "+
			"target = EXCLUDED.target, image_url = EXCLUDED.image_url, link_url = EXCLUDED.link_url, size_label = EXCLUDED.size_label, "+
			"period_seconds = EXCLUDED.period_seconds, trial_duration_seconds = EXCLUDED.trial_duration_seconds, "+
			"quantity_mode = EXCLUDED.quantity_mode, position = EXCLUDED.position, global_limit = EXCLUDED.global_limit, "+
			"global_interval = EXCLUDED.global_interval, global_interval_count = EXCLUDED.global_interval_count, "+
			"user_limit = EXCLUDED.user_limit, user_interval = EXCLUDED.user_interval, user_interval_count = EXCLUDED.user_interval_count, "+
			"available_from = EXCLUDED.available_from, available_until = EXCLUDED.available_until, "+
			"is_visible = EXCLUDED.is_visible, is_closed = EXCLUDED.is_closed, updated_at = now()",
		strategy,
	)
}

func (r *PaymentRepository) importProductItemsBulk(
	ctx context.Context,
	workspaceID string,
	products []ExportProduct,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0)
	for _, product := range products {
		if previewHasConflict(preview, "product", product.ID) && strategy == ImportConflictSkip {
			continue
		}
		for _, item := range product.Items {
			rows = append(rows, []any{
				workspaceID, product.ID, item.ItemID, defaultString(item.RewardType, "quantity"),
				item.Quantity, item.Scale, nullableString(item.DurationUnit),
			})
			result.Imported.ProductItems++
		}
	}
	return r.execImportBulk(ctx, "payment_product_item",
		[]string{"workspace_id", "product_id", "item_id", "reward_type", "quantity", "scale", "duration_unit"},
		rows,
		"(workspace_id, product_id, item_id)",
		"reward_type = EXCLUDED.reward_type, quantity = EXCLUDED.quantity, scale = EXCLUDED.scale, "+
			"duration_unit = EXCLUDED.duration_unit, updated_at = now()",
		strategy,
	)
}

func (r *PaymentRepository) importPricesBulk(
	ctx context.Context,
	workspaceID string,
	products []ExportProduct,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0)
	for _, product := range products {
		if previewHasConflict(preview, "product", product.ID) && strategy == ImportConflictSkip {
			continue
		}
		for _, price := range product.Prices {
			rows = append(rows, []any{
				workspaceID, product.ID, price.AssetCode, price.ListAmountMinor, price.DiscountAmountMinor,
				defaultString(price.PricingMode, "fixed"), nullableString(price.ReferenceAssetCode),
				nullableUint64(price.ReferenceListAmountMinor), nullableUint64(price.ReferenceDiscountAmountMinor),
				nullableString(price.Coefficient), price.IsPromotion, price.StartsAt, price.EndsAt,
			})
			result.Imported.Prices++
		}
	}
	return r.execImportBulk(ctx, "payment_price",
		[]string{
			"workspace_id", "product_id", "asset_code", "list_amount_minor", "discount_amount_minor",
			"pricing_mode", "reference_asset_code", "reference_list_amount_minor",
			"reference_discount_amount_minor", "coefficient", "is_promotion", "starts_at", "ends_at",
		},
		rows,
		"(workspace_id, product_id, asset_code, is_promotion, starts_at, ends_at)",
		"list_amount_minor = EXCLUDED.list_amount_minor, discount_amount_minor = EXCLUDED.discount_amount_minor, "+
			"pricing_mode = EXCLUDED.pricing_mode, reference_asset_code = EXCLUDED.reference_asset_code, "+
			"reference_list_amount_minor = EXCLUDED.reference_list_amount_minor, "+
			"reference_discount_amount_minor = EXCLUDED.reference_discount_amount_minor, coefficient = EXCLUDED.coefficient, "+
			"updated_at = now()",
		strategy,
	)
}

func (r *PaymentRepository) importTONWalletsBulk(
	ctx context.Context,
	workspaceID string,
	wallets []ExportTONWallet,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0, len(wallets))
	for _, wallet := range wallets {
		if previewHasConflict(preview, "ton_wallet", "default") && strategy == ImportConflictSkip {
			result.Skipped.TONWallets++
			continue
		}
		rows = append(rows, []any{
			workspaceID,
			defaultString(wallet.Network, "mainnet"),
			wallet.WalletAddress,
			nullableString(wallet.NetworkConfigURL),
			wallet.IsEnabled,
		})
		result.Imported.TONWallets++
	}
	return r.execImportBulk(ctx, "payment_ton_wallet",
		[]string{"workspace_id", "network", "wallet_address", "network_config_url", "is_enabled"},
		rows,
		"(workspace_id)",
		"network = EXCLUDED.network, wallet_address = EXCLUDED.wallet_address, "+
			"network_config_url = EXCLUDED.network_config_url, is_enabled = EXCLUDED.is_enabled, updated_at = now()",
		strategy,
	)
}

func (r *PaymentRepository) execImportBulk(
	ctx context.Context,
	table string,
	columns []string,
	rows [][]any,
	conflictTarget string,
	duplicateUpdate string,
	strategy string,
) error {
	if len(rows) == 0 {
		return nil
	}
	return importexport.ForEachBatch(
		len(rows),
		len(columns),
		importexport.DefaultBatchLimits,
		func(start, end int) error {
			query, args := compileImportBulkUpsert(
				table,
				columns,
				rows[start:end],
				conflictTarget,
				duplicateUpdate,
				strategy,
			)
			_, err := r.executor.ExecContext(ctx, query, args...)
			return err
		},
	)
}

func compileImportBulkUpsert(
	table string,
	columns []string,
	rows [][]any,
	conflictTarget string,
	duplicateUpdate string,
	strategy string,
) (string, []any) {
	var builder strings.Builder
	builder.WriteString("INSERT INTO ")
	builder.WriteString(table)
	builder.WriteString(" (")
	builder.WriteString(strings.Join(columns, ", "))
	builder.WriteString(") VALUES ")
	args := make([]any, 0, len(rows)*len(columns))
	for rowIndex, row := range rows {
		if rowIndex > 0 {
			builder.WriteString(", ")
		}
		builder.WriteByte('(')
		for columnIndex := range columns {
			if columnIndex > 0 {
				builder.WriteString(", ")
			}
			fmt.Fprintf(&builder, "$%d", len(args)+columnIndex+1)
		}
		builder.WriteByte(')')
		args = append(args, row...)
	}
	switch strategy {
	case ImportConflictSkip:
		builder.WriteString(" ON CONFLICT ")
		builder.WriteString(conflictTarget)
		builder.WriteString(" DO NOTHING")
	case ImportConflictUpdate:
		builder.WriteString(" ON CONFLICT ")
		builder.WriteString(conflictTarget)
		builder.WriteString(" DO UPDATE SET ")
		builder.WriteString(duplicateUpdate)
	}
	return builder.String(), args
}

func validateExportPackage(pkg ExportPackage) error {
	if pkg.Format != ExportFormat {
		return fmt.Errorf("unsupported export format: %s", pkg.Format)
	}
	if pkg.Service != "payment" {
		return fmt.Errorf("unsupported export service: %s", pkg.Service)
	}
	if len(pkg.TONWallets) > 1 {
		return fmt.Errorf("payment import ton_wallets contains more than one wallet")
	}
	for index, wallet := range pkg.TONWallets {
		if strings.TrimSpace(wallet.WalletAddress) == "" {
			return fmt.Errorf("payment import ton_wallets[%d].wallet_address is required", index)
		}
		if network := defaultString(strings.TrimSpace(wallet.Network), "mainnet"); network != "mainnet" &&
			network != "testnet" {
			return fmt.Errorf("payment import ton_wallets[%d].network is unsupported", index)
		}
	}

	groupIndexes := make(map[string]int, len(pkg.Groups))
	productPaths := make(map[string]string)
	for groupIndex, group := range pkg.Groups {
		groupPath := fmt.Sprintf("payment import groups[%d]", groupIndex)
		group.Code = strings.TrimSpace(group.Code)
		if group.Code == "" {
			return fmt.Errorf("%s.key is required", groupPath)
		}
		if previous, exists := groupIndexes[group.Code]; exists {
			return fmt.Errorf("%s.key duplicates groups[%d].key", groupPath, previous)
		}
		groupIndexes[group.Code] = groupIndex
		if err := validateExportLocalization(groupPath, group.Localization); err != nil {
			return err
		}

		for productIndex, product := range group.Products {
			path := fmt.Sprintf("%s.products[%d]", groupPath, productIndex)
			if product.GroupCode != nil && strings.TrimSpace(*product.GroupCode) != group.Code {
				return fmt.Errorf("%s.group_code must match parent group", path)
			}
			if err := validateExportProduct(product, path, productPaths); err != nil {
				return err
			}
		}
	}
	for productIndex, product := range pkg.Products {
		path := fmt.Sprintf("payment import products[%d]", productIndex)
		if err := validateExportProduct(product, path, productPaths); err != nil {
			return err
		}
	}
	return nil
}

func validateExportProduct(product ExportProduct, path string, productPaths map[string]string) error {
	product.ID = strings.TrimSpace(product.ID)
	if product.ID == "" {
		return fmt.Errorf("%s.key is required", path)
	}
	if previous, exists := productPaths[product.ID]; exists {
		return fmt.Errorf("%s.key duplicates %s.key", path, previous)
	}
	productPaths[product.ID] = path

	if err := target.Validate(product.Target); err != nil {
		return fmt.Errorf("%s.target: %w", path, err)
	}
	quantityMode := defaultString(product.QuantityMode, "fixed")
	if quantityMode != "fixed" && quantityMode != "flexible" {
		return fmt.Errorf("%s.quantity_mode is unsupported", path)
	}
	if product.PeriodSeconds != nil && *product.PeriodSeconds < 0 {
		return fmt.Errorf("%s.period_seconds must not be negative", path)
	}
	if product.TrialDurationSeconds != nil && *product.TrialDurationSeconds < 0 {
		return fmt.Errorf("%s.trial_duration_seconds must not be negative", path)
	}
	if product.GlobalLimit < 0 || product.GlobalIntervalCount < 0 ||
		product.UserLimit < 0 || product.UserIntervalCount < 0 {
		return fmt.Errorf("%s limits must not be negative", path)
	}
	globalInterval := defaultString(product.GlobalInterval, "UNLIMITED")
	userInterval := defaultString(product.UserInterval, "UNLIMITED")
	if !validPaymentInterval(globalInterval) || !validPaymentInterval(userInterval) {
		return fmt.Errorf("%s limit interval is unsupported", path)
	}
	availableFrom := defaultTime(product.AvailableFrom, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	availableUntil := defaultTime(product.AvailableUntil, time.Date(2124, 1, 1, 0, 0, 0, 0, time.UTC))
	if !availableFrom.Before(availableUntil) {
		return fmt.Errorf("%s.available_from must be before available_until", path)
	}
	if err := validateExportLocalization(path, product.Localization); err != nil {
		return err
	}

	itemIndexes := make(map[string]int, len(product.Items))
	for itemIndex, item := range product.Items {
		itemPath := fmt.Sprintf("%s.items[%d]", path, itemIndex)
		item.ItemID = strings.TrimSpace(item.ItemID)
		if item.ItemID == "" {
			return fmt.Errorf("%s.item_id is required", itemPath)
		}
		if previous, exists := itemIndexes[item.ItemID]; exists {
			return fmt.Errorf("%s.item_id duplicates items[%d].item_id", itemPath, previous)
		}
		itemIndexes[item.ItemID] = itemIndex
		if item.Quantity <= 0 || item.Scale > math.MaxInt16 ||
			!validReward(defaultString(item.RewardType, "quantity"), item.DurationUnit) {
			return fmt.Errorf("%s reward is invalid", itemPath)
		}
	}

	priceIndexes := make(map[string]int, len(product.Prices))
	for priceIndex, price := range product.Prices {
		pricePath := fmt.Sprintf("%s.prices[%d]", path, priceIndex)
		if err := validateExportPrice(price, pricePath); err != nil {
			return err
		}
		key := fmt.Sprintf(
			"%s\x00%t\x00%s\x00%s",
			price.AssetCode,
			price.IsPromotion,
			price.StartsAt.UTC(),
			price.EndsAt.UTC(),
		)
		if previous, exists := priceIndexes[key]; exists {
			return fmt.Errorf("%s duplicates prices[%d] window", pricePath, previous)
		}
		priceIndexes[key] = priceIndex
	}
	return nil
}

func validateExportPrice(price ExportPrice, path string) error {
	price.AssetCode = strings.TrimSpace(price.AssetCode)
	if price.AssetCode == "" {
		return fmt.Errorf("%s.asset_code is required", path)
	}
	if price.ListAmountMinor > math.MaxInt64 || price.DiscountAmountMinor > price.ListAmountMinor {
		return fmt.Errorf("%s amount is invalid", path)
	}
	if !price.StartsAt.Before(price.EndsAt) {
		return fmt.Errorf("%s.starts_at must be before ends_at", path)
	}

	mode := defaultString(price.PricingMode, PricingModeFixed)
	switch mode {
	case PricingModeFixed:
		if price.ReferenceAssetCode != nil || price.ReferenceListAmountMinor != nil ||
			price.ReferenceDiscountAmountMinor != nil || price.Coefficient != nil {
			return fmt.Errorf("%s fixed price must not contain reference fields", path)
		}
	case PricingModeDynamic:
		if price.ReferenceAssetCode == nil || strings.TrimSpace(*price.ReferenceAssetCode) == "" ||
			price.ReferenceListAmountMinor == nil || price.ReferenceDiscountAmountMinor == nil ||
			price.Coefficient == nil {
			return fmt.Errorf("%s dynamic price requires reference fields", path)
		}
		if strings.TrimSpace(*price.ReferenceAssetCode) == price.AssetCode ||
			*price.ReferenceListAmountMinor > math.MaxInt64 ||
			*price.ReferenceDiscountAmountMinor > *price.ReferenceListAmountMinor ||
			!positiveDecimal(*price.Coefficient) {
			return fmt.Errorf("%s dynamic reference values are invalid", path)
		}
	default:
		return fmt.Errorf("%s.pricing_mode is unsupported", path)
	}
	return nil
}

func validateExportLocalization(path string, localization map[string]ExportText) error {
	for locale, text := range localization {
		if strings.TrimSpace(locale) == "" || strings.TrimSpace(text.Title) == "" {
			return fmt.Errorf("%s.localization requires locale and title", path)
		}
	}
	return nil
}

func validPaymentInterval(value string) bool {
	switch value {
	case "SECOND", "MINUTE", "HOUR", "DAY", "WEEK", "MONTH", "ONCE", "UNLIMITED":
		return true
	default:
		return false
	}
}

func positiveDecimal(value string) bool {
	number, ok := new(big.Float).SetString(strings.TrimSpace(value))
	return ok && number.Sign() > 0
}

func countPackage(pkg ExportPackage) ImportCounts {
	var counts ImportCounts
	counts.Groups = uint64(len(pkg.Groups))
	counts.TONWallets = uint64(len(pkg.TONWallets))
	for _, group := range pkg.Groups {
		counts.Localizations += uint64(len(group.Localization))
		countProducts(&counts, group.Products)
	}
	countProducts(&counts, pkg.Products)
	return counts
}

func countProducts(counts *ImportCounts, products []ExportProduct) {
	counts.Products += uint64(len(products))
	for _, product := range products {
		counts.Localizations += uint64(len(product.Localization))
		counts.ProductItems += uint64(len(product.Items))
		counts.Prices += uint64(len(product.Prices))
	}
}

type importExisting struct {
	groups    map[string]bool
	products  map[string]bool
	tonWallet bool
}

func (r *PaymentRepository) importExistingKeys(ctx context.Context, workspaceID string) (importExisting, error) {
	existing := importExisting{
		groups:   make(map[string]bool),
		products: make(map[string]bool),
	}
	groupRows, err := r.q.ImportListProductGroupCodes(ctx, workspaceID)
	if err != nil {
		return existing, err
	}
	for _, key := range groupRows {
		existing.groups[key] = true
	}
	productRows, err := r.q.ImportListProductIDs(ctx, workspaceID)
	if err != nil {
		return existing, err
	}
	for _, key := range productRows {
		existing.products[key] = true
	}
	existing.tonWallet, err = r.q.ImportHasTONWallet(ctx, workspaceID)
	if err != nil {
		return existing, err
	}
	return existing, nil
}

func previewHasConflict(preview ImportPreview, kind, key string) bool {
	for _, conflict := range preview.Conflicts {
		if conflict.Type == kind && conflict.Key == key {
			return true
		}
	}
	return false
}

func flattenProducts(pkg ExportPackage) []ExportProduct {
	total := len(pkg.Products)
	for _, group := range pkg.Groups {
		total += len(group.Products)
	}
	result := make([]ExportProduct, 0, total)
	for _, group := range pkg.Groups {
		for _, product := range group.Products {
			if product.GroupCode == nil {
				product.GroupCode = &group.Code
			}
			result = append(result, product)
		}
	}
	result = append(result, pkg.Products...)
	return result
}

func normalizeExportPackage(pkg ExportPackage) ExportPackage {
	for index := range pkg.Groups {
		group := &pkg.Groups[index]
		if group.TitleKey == nil && group.Code != "" {
			group.TitleKey = stringPtr("payment.group." + group.Code + ".title")
		}
		if group.DescriptionKey == nil && group.Code != "" {
			group.DescriptionKey = stringPtr("payment.group." + group.Code + ".description")
		}
		for productIndex := range group.Products {
			normalizeExportProduct(&group.Products[productIndex])
		}
	}
	for index := range pkg.Products {
		normalizeExportProduct(&pkg.Products[index])
	}
	return pkg
}

func normalizeExportProduct(product *ExportProduct) {
	if product == nil || product.ID == "" {
		return
	}
	if product.TitleKey == "" {
		product.TitleKey = "payment.product." + product.ID + ".title"
	}
	if product.DescriptionKey == nil {
		product.DescriptionKey = stringPtr("payment.product." + product.ID + ".description")
	}
}

func stringPtr(value string) *string {
	return &value
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func defaultJSON(value []byte, fallback string) string {
	if len(value) == 0 {
		return fallback
	}
	return string(value)
}

func defaultTime(value, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value
}

func nullableString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func nullableInt64(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}

func nullableUint64(value *uint64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*value), Valid: true}
}
