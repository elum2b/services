package payment

import (
	"context"
	"errors"
	"testing"

	"github.com/elum2b/services/payment/repository"
)

func TestPaymentBalanceRoutingScopes(t *testing.T) {
	exact := balanceTestBinding()
	scoped := BalanceBinding{WorkspaceID: exact.WorkspaceID, AppID: exact.AppID + 1}
	workspace := BalanceBinding{WorkspaceID: exact.WorkspaceID}

	charge := func(context.Context, BalanceChargeRequest) (BalanceChargeResult, error) {
		return BalanceChargeResult{Outcome: BalanceChargeSucceeded}, nil
	}
	options := paymentTestOptions()
	options.BalanceBindings = map[BalanceBinding]string{
		exact: "exact", scoped: "application", workspace: "shared",
		{WorkspaceID: exact.WorkspaceID, AppID: exact.AppID}: "application",
	}
	options.BalanceHandlers = map[string]BalanceChargeHandler{
		"exact": charge, "application": charge, "shared": charge,
	}
	if err := validateBalanceOptions(options); err != nil {
		t.Fatalf("validate configured routes: %v", err)
	}

	env := setupPaymentIntegrationTestWithOptions(t, options)
	params := setupBalanceProduct(t, env, false)
	for index, tc := range []struct {
		appID int64
		name  string
	}{
		{exact.AppID, "exact"},
		{scoped.AppID, "application"},
		{scoped.AppID + 1, "shared"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := params
			candidate.Identity.AppID = tc.appID
			purchase, err := env.api.User.PurchaseWithBalance(env.ctx, candidate)
			if err != nil {
				t.Fatal(err)
			}
			var handler string
			if err := env.db.QueryRowContext(env.ctx,
				"SELECT handler_key FROM payment_balance_purchase WHERE order_id=$1",
				purchase.OrderID,
			).Scan(&handler); err != nil {
				t.Fatal(err)
			}
			if handler != tc.name {
				t.Fatalf("route %d = %q, want %q", index, handler, tc.name)
			}
		})
	}

	unbound := params
	unbound.Identity.WorkspaceID = "fa815979-35b4-4a10-ae36-e6424ea0c9a0"
	if _, err := env.api.User.PurchaseWithBalance(env.ctx, unbound); !errors.Is(err, ErrBalanceUnavailable) {
		t.Fatalf("foreign workspace: %v", err)
	}

	external := params
	external.AssetCode = "RUB"
	external.RequestID = "external"
	if _, err := env.api.User.PurchaseWithBalance(env.ctx, external); !errors.Is(err, ErrBalanceUnavailable) {
		t.Fatalf("external currency through workspace route: %v", err)
	}

	options.BalanceBindings[BalanceBinding{WorkspaceID: exact.WorkspaceID, AssetCode: exact.AssetCode}] = "shared"
	if err := validateBalanceOptions(options); !errors.Is(err, repository.ErrBalanceRequestInvalid) {
		t.Fatalf("asset-only route: %v", err)
	}
}
