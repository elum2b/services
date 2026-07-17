package checkout

import (
	"github.com/elum2b/services/payment/repository"
)

func mapOrder(order repository.Order) *Order {
	return &Order{
		ID:                  order.ID,
		PublicID:            order.PublicID,
		WorkspaceID:         order.WorkspaceID,
		AppID:               order.AppID,
		PlatformID:          order.PlatformID,
		PlatformUserID:      order.PlatformUserID,
		InternalUserID:      uint64Ptr(order.InternalUserID),
		PayerPlatformID:     uint64Ptr(order.PayerPlatformID),
		PayerPlatformUserID: order.PayerPlatformUserID,
		PayerInternalUserID: uint64Ptr(order.PayerInternalUserID),
		ProductID:           order.ProductID,
		Quantity:            order.Quantity,
		PriceID:             order.PriceID,
		AssetCode:           order.AssetCode,
		Locale:              order.Locale,
		ListAmountMinor:     order.ListAmountMinor,
		DiscountAmountMinor: order.DiscountAmountMinor,
		PayableAmountMinor:  order.PayableAmountMinor,
		Status:              order.Status,
	}
}

func mapAttempt(attempt repository.Attempt) *Attempt {
	return &Attempt{
		ID:                attempt.ID,
		OrderID:           attempt.OrderID,
		ProviderCode:      attempt.ProviderCode,
		AssetCode:         attempt.AssetCode,
		AmountMinor:       attempt.AmountMinor,
		Status:            attempt.Status,
		ProviderPaymentID: attempt.ProviderPaymentID,
	}
}

func uint64Ptr(value *int64) *uint64 {
	if value == nil {
		return nil
	}
	v := uint64(*value)
	return &v
}
