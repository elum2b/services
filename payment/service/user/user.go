package user

import (
	"github.com/elum2b/services/payment/service/asset"
	"github.com/elum2b/services/payment/service/checkout"
	"github.com/elum2b/services/payment/service/product"
	"github.com/elum2b/services/payment/service/subscription"
)

type User struct {
	assets       *asset.Asset
	products     *product.Product
	checkout     *checkout.Checkout
	subscription *subscription.Subscription
}

func New(
	assets *asset.Asset,
	products *product.Product,
	checkoutAPI *checkout.Checkout,
	subscriptionAPI *subscription.Subscription,
) *User {
	return &User{
		assets:       assets,
		products:     products,
		checkout:     checkoutAPI,
		subscription: subscriptionAPI,
	}
}
