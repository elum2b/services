package subscription

import services "github.com/elum2b/services"

type IsActiveParams struct {
	Identity     services.Identity
	ProductID    string
	ProviderCode string
}
