package asset

import "context"

func (a *Asset) ListUSDTPrices(
	ctx context.Context,
) ([]USDTPriceModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()

	rows, err := a.repository.ListAssetUSDTPrices(mergedCtx)
	if err != nil {
		return nil, err
	}
	result := make([]USDTPriceModel, 0, len(rows))
	for _, row := range rows {
		result = append(result, USDTPriceModel{
			AssetCode:          row.AssetCode,
			AssetTitle:         row.AssetTitle,
			Scale:              uint16(row.Scale),
			ReferenceAssetCode: row.ReferenceAssetCode,
			USDTPerAssetMinor:  uint64(row.ReferencePerAssetMinor),
			Source:             row.Source,
			ObservedAt:         row.ObservedAt,
			UpdatedAt:          row.UpdatedAt,
		})
	}
	return result, nil
}
