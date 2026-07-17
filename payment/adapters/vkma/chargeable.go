package vkma

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	utils "github.com/elum2b/services/internal/utils"
	"github.com/elum2b/services/payment/repository"

	"github.com/elum-utils/sign/vkmashop"
)

func (a *VKMA) ChargeableForWorkspace(ctx context.Context, workspaceID string, params vkmashop.Params) (*ChargeableResponse, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	providerPaymentID := strconv.Itoa(params.OrderID)
	providerSubscriptionID := nullStringFromPositiveInt(params.SubscriptionID)

	attempt, err := a.repository.GetAttemptByProviderPaymentID(
		ctx,
		workspaceID,
		ProviderCode,
		providerPaymentID,
	)
	if err == nil {
		if attempt.Status != "succeeded" {
			if _, completeErr := a.repository.CompleteAttempt(ctx, repository.CompleteAttemptParams{
				WorkspaceID:       workspaceID,
				AttemptID:         attempt.ID,
				ProviderCode:      ProviderCode,
				ProviderPaymentID: utils.Ref(providerPaymentID),
				AmountMinor:       attempt.AmountMinor,
				AssetCode:         AssetCode,
			}); completeErr != nil {
				return nil, completeErr
			}
		}
		order, orderErr := a.repository.GetOrder(ctx, attempt.OrderID)
		if orderErr != nil {
			return nil, orderErr
		}
		return &ChargeableResponse{AppOrderID: order.ID, OrderID: params.OrderID}, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	order, err := a.repository.CreateOrder(ctx, repository.OrderCreateParams{
		AppID:          int64(params.AppID),
		WorkspaceID:    workspaceID,
		PlatformID:     PlatformID,
		PlatformUserID: strconv.Itoa(params.UserID),
		ProductID:      productID(params),
		AssetCode:      AssetCode,
		Locale:         normalizeLocale(params.Lang),
	})
	if err != nil {
		return nil, err
	}

	attempt, err = a.repository.CreateAttempt(ctx, repository.AttemptCreateParams{
		OrderID:                order.ID,
		ProviderCode:           ProviderCode,
		ProviderPaymentID:      utils.Ref(providerPaymentID),
		ProviderSubscriptionID: providerSubscriptionID,
		IdempotencyKey:         utils.Ref(fmt.Sprintf("%s:%s", ProviderCode, providerPaymentID)),
	})
	if err != nil {
		return nil, err
	}

	if _, err := a.repository.CreateEvent(ctx, repository.EventCreateParams{
		WorkspaceID:       workspaceID,
		ProviderCode:      ProviderCode,
		AttemptID:         utils.Ref(int64(attempt.ID)),
		OrderID:           utils.Ref(int64(order.ID)),
		ProviderEventID:   eventID(params),
		ProviderPaymentID: utils.Ref(providerPaymentID),
		EventType:         string(params.NotificationType),
		EventStatus:       utils.Ref(string(params.Status)),
		PayloadHash:       payloadHash(params),
		SignatureValid:    utils.Ref(true),
	}); err != nil {
		return nil, err
	}

	if _, err := a.repository.CompleteAttempt(ctx, repository.CompleteAttemptParams{
		WorkspaceID:       workspaceID,
		AttemptID:         attempt.ID,
		ProviderCode:      ProviderCode,
		ProviderPaymentID: utils.Ref(providerPaymentID),
		AmountMinor:       attempt.AmountMinor,
		AssetCode:         AssetCode,
	}); err != nil {
		return nil, err
	}

	if params.SubscriptionID > 0 {
		if _, err := a.repository.UpsertSubscription(ctx, repository.SubscriptionUpsertParams{
			ProviderCode:           ProviderCode,
			WorkspaceID:            workspaceID,
			ProviderSubscriptionID: strconv.Itoa(params.SubscriptionID),
			AppID:                  int64(params.AppID),
			PlatformID:             PlatformID,
			PlatformUserID:         strconv.Itoa(params.UserID),
			ProductID:              order.ProductID,
			OrderID:                utils.Ref(int64(order.ID)),
			AttemptID:              utils.Ref(int64(attempt.ID)),
			Status:                 "active",
			StartedAt:              time.Now(),
		}); err != nil {
			return nil, err
		}
	}

	return &ChargeableResponse{AppOrderID: order.ID, OrderID: params.OrderID}, nil
}
