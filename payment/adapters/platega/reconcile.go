package platega

import (
	"context"
	"errors"
	"time"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/payment/repository"
)

func (a *Platega) ReconcilePending(ctx context.Context, params ReconcileParams) (ReconcileResult, error) {
	if a == nil || a.repository == nil {
		return ReconcileResult{}, ErrNotInitialized
	}
	if params.ResolveCredentials == nil || params.CreatedTo.IsZero() || params.Limit <= 0 {
		return ReconcileResult{}, repository.ErrAttemptFieldsInvalid
	}

	attempts, err := a.repository.ListProviderAttemptsForReconciliation(
		ctx,
		ProviderCode,
		params.CreatedTo,
		params.Limit,
	)
	if err != nil {
		return ReconcileResult{}, err
	}
	result := ReconcileResult{Scanned: len(attempts)}
	if len(attempts) == 0 {
		return result, nil
	}

	byWorkspace := make(map[string][]repository.ProviderAttemptForReconciliation)
	for _, attempt := range attempts {
		byWorkspace[attempt.WorkspaceID] = append(byWorkspace[attempt.WorkspaceID], attempt)
	}

	var resultErr error
	for workspaceID, workspaceAttempts := range byWorkspace {
		credentials, err := params.ResolveCredentials(ctx, workspaceID)
		if err != nil {
			resultErr = errors.Join(resultErr, err)
			continue
		}
		from := workspaceAttempts[0].CreatedAt
		to := workspaceAttempts[0].CreatedAt
		for _, attempt := range workspaceAttempts[1:] {
			if attempt.CreatedAt.Before(from) {
				from = attempt.CreatedAt
			}
			if attempt.CreatedAt.After(to) {
				to = attempt.CreatedAt
			}
		}
		from = from.Add(-time.Minute)
		to = to.Add(params.MissingAfter)
		if params.MissingAfter <= 0 || to.After(params.CreatedTo) {
			to = params.CreatedTo
		}
		records, err := NewClient(credentials).ExportTransactions(ctx, exportTransactionsRequest{
			From:       from,
			To:         to,
			TimeZoneID: "UTC",
		})
		if err != nil {
			resultErr = errors.Join(resultErr, err)
			continue
		}

		recordByPayload := make(map[string]exportedTransaction, len(records))
		recordByID := make(map[string]exportedTransaction, len(records))
		ambiguousPayload := make(map[string]struct{})
		ambiguousID := make(map[string]struct{})
		for _, record := range records {
			if record.RecordID == "" {
				continue
			}
			if previous, exists := recordByID[record.RecordID]; exists && previous.Payload != record.Payload {
				ambiguousID[record.RecordID] = struct{}{}
			}
			recordByID[record.RecordID] = record
			if record.Payload != "" {
				if previous, exists := recordByPayload[record.Payload]; exists && previous.RecordID != record.RecordID {
					ambiguousPayload[record.Payload] = struct{}{}
				}
				recordByPayload[record.Payload] = record
			}
		}
		for _, attempt := range workspaceAttempts {
			record, ok, ambiguous := reconciliationRecord(
				attempt,
				recordByPayload,
				recordByID,
				ambiguousPayload,
				ambiguousID,
			)
			if ambiguous {
				resultErr = errors.Join(resultErr, repository.ErrPaymentMismatch)
				continue
			}
			if !ok {
				if attempt.ProviderPaymentID == nil && params.MissingAfter > 0 &&
					!attempt.CreatedAt.Add(params.MissingAfter).After(params.CreatedTo) {
					if err := a.repository.FailProviderAttempt(
						ctx,
						workspaceID,
						attempt.ID,
						ProviderCode,
					); err != nil {
						resultErr = errors.Join(resultErr, err)
						continue
					}
					result.Released++
				}
				continue
			}
			if record.Payload != attempt.OrderPublicID {
				resultErr = errors.Join(resultErr, repository.ErrPaymentMismatch)
				continue
			}
			recordAmountMinor, err := rubMinorFromMajor(record.Amount)
			if err != nil || recordAmountMinor != attempt.AmountMinor || record.CurrencyCode != attempt.AssetCode {
				resultErr = errors.Join(resultErr, repository.ErrPaymentMismatch)
				continue
			}

			payload := callbackPayload{
				ID:            record.RecordID,
				Amount:        record.Amount,
				Currency:      record.CurrencyCode,
				Status:        record.Status,
				PaymentMethod: PaymentMethodAny,
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				resultErr = errors.Join(resultErr, err)
				continue
			}
			transaction := transactionStatusResponse{
				ID:             record.RecordID,
				Status:         record.Status,
				PaymentDetails: paymentDetails{Amount: record.Amount, Currency: record.CurrencyCode},
				Payload:        record.Payload,
			}
			webhookResult, err := a.handlePayload(
				ctx,
				credentials,
				workspaceID,
				payload,
				raw,
				false,
				&transaction,
			)
			if err != nil {
				resultErr = errors.Join(resultErr, err)
				continue
			}
			result.Recovered++
			if webhookResult.FulfilledID != nil || webhookResult.AlreadyDone {
				result.Completed++
			}
		}
	}

	return result, resultErr
}

func reconciliationRecord(
	attempt repository.ProviderAttemptForReconciliation,
	recordByPayload map[string]exportedTransaction,
	recordByID map[string]exportedTransaction,
	ambiguousPayload map[string]struct{},
	ambiguousID map[string]struct{},
) (exportedTransaction, bool, bool) {
	if attempt.ProviderPaymentID != nil {
		if _, ambiguous := ambiguousID[*attempt.ProviderPaymentID]; ambiguous {
			return exportedTransaction{}, false, true
		}
		record, ok := recordByID[*attempt.ProviderPaymentID]
		return record, ok, false
	}

	if _, ambiguous := ambiguousPayload[attempt.OrderPublicID]; ambiguous {
		return exportedTransaction{}, false, true
	}
	record, ok := recordByPayload[attempt.OrderPublicID]
	return record, ok, false
}
