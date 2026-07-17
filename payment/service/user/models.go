package user

import (
	services "github.com/elum2b/services"
	"github.com/elum2b/services/payment/service/asset"
	"github.com/elum2b/services/payment/service/checkout"
	"github.com/elum2b/services/payment/service/product"
	"github.com/elum2b/services/payment/service/subscription"
)

type Identity = services.Identity
type Actor = services.Actor

type USDTPriceModel = asset.USDTPriceModel

type ListAssetsParams struct{}
type GetUSDTPriceParams struct {
	AssetCode string
}
type ListUSDTPricesParams struct{}

type ListProductsParams = product.ListParams
type GetProductParams = product.GetParams
type GetProductByKeyParams = product.GetByKeyParams
type ProductModel = product.ProductModel

type AssetModel = asset.Model

type CreateOrderParams = checkout.CreateOrderParams
type CreateOrderByKeyParams = checkout.CreateOrderByKeyParams
type OrderModel = checkout.Order
type CreateAttemptParams = checkout.CreateAttemptParams
type AttemptModel = checkout.Attempt

type IsSubscriptionActiveParams = subscription.IsActiveParams
