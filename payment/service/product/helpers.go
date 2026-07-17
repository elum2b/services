package product

import (
	sqlwrap "github.com/elum2b/services/internal/utils/sql"

	"github.com/elum2b/services/payment/repository"
)

func mapProduct(product repository.Product) *ProductModel {
	items := make([]ProductItem, 0, len(product.Items))
	for _, item := range product.Items {
		items = append(items, ProductItem{
			ID:           item.ID,
			RewardType:   item.RewardType,
			Quantity:     item.Quantity,
			Scale:        item.Scale,
			DurationUnit: item.DurationUnit,
		})
	}

	return &ProductModel{
		ID:                   product.ID,
		LinkURL:              sqlwrap.NullStringPtr(product.LinkURL),
		SizeLabel:            sqlwrap.NullStringPtr(product.SizeLabel),
		GroupCode:            sqlwrap.NullStringPtr(product.GroupCode),
		Title:                product.Title,
		Description:          product.Description,
		ImageURL:             sqlwrap.NullStringPtr(product.ImageURL),
		PeriodSeconds:        sqlwrap.NullInt64Ptr(product.PeriodSeconds),
		TrialDurationSeconds: sqlwrap.NullInt64Ptr(product.TrialDurationSeconds),
		QuantityMode:         product.QuantityMode,
		Price: Price{
			ID:                  product.Price.ID,
			AssetCode:           product.Price.AssetCode,
			ListAmountMinor:     product.Price.ListAmountMinor,
			DiscountAmountMinor: product.Price.DiscountAmountMinor,
			PayableAmountMinor:  product.Price.PayableAmountMinor,
		},
		Limit: Limit{
			Global: LimitRule{
				Limit:         product.Limit.Global.Limit,
				Interval:      product.Limit.Global.Interval,
				IntervalCount: product.Limit.Global.IntervalCount,
				LockUntil:     sqlwrap.NullTimePtr(product.Limit.Global.LockUntil),
			},
			User: LimitRule{
				Limit:         product.Limit.User.Limit,
				Interval:      product.Limit.User.Interval,
				IntervalCount: product.Limit.User.IntervalCount,
				LockUntil:     sqlwrap.NullTimePtr(product.Limit.User.LockUntil),
			},
		},
		Items: items,
	}
}
