package vkma

import (
	"context"
	"database/sql"

	"github.com/elum-utils/sign/vkmashop"
)

type Request struct {
	WorkspaceID string
	Params      vkmashop.Params
}

func (a *VKMA) HandleRequest(ctx context.Context, request Request) (any, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	params := request.Params
	workspaceID := request.WorkspaceID
	switch params.NotificationType {
	case vkmashop.GetItem, vkmashop.GetItemTest:
		return a.GetItemForWorkspace(ctx, workspaceID, params)
	case vkmashop.GetSubscription:
		return a.GetSubscriptionForWorkspace(ctx, workspaceID, params)
	case vkmashop.OrderStatusChange, vkmashop.OrderStatusChangeTest:
		switch params.Status {
		case vkmashop.Chargeable:
			return a.ChargeableForWorkspace(ctx, workspaceID, params)
		case vkmashop.Refunded:
			return a.RefundOrderForWorkspace(ctx, workspaceID, params)
		}
	case vkmashop.SubscriptionStatusChange, vkmashop.SubscriptionStatusChangeTest:
		switch params.Status {
		case vkmashop.Chargeable:
			return a.ChargeableForWorkspace(ctx, workspaceID, params)
		case vkmashop.Active:
			return a.Active(ctx, workspaceID, params)
		case vkmashop.Canceled:
			return a.Canceled(ctx, workspaceID, params)
		case vkmashop.Refunded:
			return a.Refunded(ctx, workspaceID, params)
		}
	}

	return nil, sql.ErrNoRows
}
