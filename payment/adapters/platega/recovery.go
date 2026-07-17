package platega

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/elum2b/services/payment/repository"
)

func (a *Platega) resolveAttempt(
	ctx context.Context,
	credentials Credentials,
	workspaceID string,
	payload callbackPayload,
	knownTransaction *transactionStatusResponse,
) (repository.Attempt, error) {
	attempt, err := a.repository.GetAttemptByProviderPaymentID(
		ctx,
		workspaceID,
		ProviderCode,
		payload.ID,
	)
	if err == nil {
		return attempt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return repository.Attempt{}, err
	}

	transaction := transactionStatusResponse{}
	if knownTransaction != nil {
		transaction = *knownTransaction
	} else {
		transaction, err = NewClient(credentials).GetTransaction(ctx, payload.ID)
		if err != nil {
			return repository.Attempt{}, err
		}
	}
	if strings.TrimSpace(transaction.ID) != payload.ID || strings.TrimSpace(transaction.Payload) == "" {
		return repository.Attempt{}, repository.ErrPaymentMismatch
	}
	amountMinor, err := rubMinorFromMajor(transaction.PaymentDetails.Amount)
	if err != nil || amountMinor == 0 || strings.TrimSpace(transaction.PaymentDetails.Currency) == "" {
		return repository.Attempt{}, repository.ErrPaymentMismatch
	}
	if strings.TrimSpace(payload.Amount.String()) != "" {
		payloadAmountMinor, err := rubMinorFromMajor(payload.Amount)
		if err != nil || payloadAmountMinor != amountMinor {
			return repository.Attempt{}, repository.ErrPaymentMismatch
		}
	}
	if payload.Currency != "" && payload.Currency != transaction.PaymentDetails.Currency {
		return repository.Attempt{}, repository.ErrPaymentMismatch
	}

	return a.repository.RecoverProviderAttempt(ctx, repository.ProviderAttemptRecoverParams{
		WorkspaceID:       workspaceID,
		OrderPublicID:     transaction.Payload,
		ProviderCode:      ProviderCode,
		ProviderPaymentID: transaction.ID,
		AmountMinor:       amountMinor,
		AssetCode:         transaction.PaymentDetails.Currency,
	})
}
