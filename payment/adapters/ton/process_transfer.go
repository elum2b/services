package ton

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	utils "github.com/elum2b/services/internal/utils"
	"github.com/elum2b/services/payment/repository"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

func (a *TON) ProcessTransfer(ctx context.Context, transfer IncomingTransfer) (*ProcessResult, error) {

	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()

	ctx = mergedCtx
	transfer.AssetCode = normalizeAsset(transfer.AssetCode)

	network, err := validateNetwork(transfer.Network)
	if err != nil {
		return nil, err
	}

	transfer.Network = network
	transfer.WalletAddress, err = NormalizeWalletAddress(transfer.WalletAddress, network)
	if err != nil {
		return nil, err
	}

	var failedTransaction *repository.AdminProviderTransactionModel
	if transfer.TxHash != "" {
		existing, err := a.repository.GetProviderTransactionByExternalID(ctx, paymentsqlc.GetProviderTransactionByExternalIDParams{
			WorkspaceID:           transfer.WorkspaceID,
			ProviderCode:          ProviderCode,
			Network:               transfer.Network,
			SourceKey:             transfer.WalletAddress,
			ExternalTransactionID: transfer.TxHash,
		})
		if err == nil {
			if !providerTransactionMatchesTransfer(existing, transfer) {
				return nil, repository.ErrPaymentMismatch
			}

			if existing.Status == string(paymentsqlc.PaymentProviderTransactionStatusFailed) {
				failedTransaction = &existing
			} else {
				if err := a.advanceTransferCursor(ctx, transfer); err != nil {
					return nil, err
				}

				return &ProcessResult{
					OrderID:     uint64FromRepositoryNull(existing.OrderID),
					AttemptID:   uint64FromRepositoryNull(existing.AttemptID),
					Transaction: uint64(existing.ID),
					AlreadyDone: true,
					Ignored:     existing.Status == string(paymentsqlc.PaymentProviderTransactionStatusIgnored),
				}, nil
			}
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	attempt, err := a.repository.GetAttemptByProviderPaymentID(
		ctx,
		transfer.WorkspaceID,
		ProviderCode,
		transfer.Comment,
	)
	if err != nil {
		if failedTransaction != nil {
			return nil, err
		}

		return a.storeTransfer(ctx, transfer, 0, 0, paymentsqlc.PaymentProviderTransactionStatusIgnored, err.Error())
	}
	if attempt.AssetCode != transfer.AssetCode || attempt.AmountMinor != transfer.AmountMinor {
		if failedTransaction != nil {
			return nil, repository.ErrPaymentMismatch
		}

		return a.storeTransfer(ctx, transfer, attempt.OrderID, attempt.ID, paymentsqlc.PaymentProviderTransactionStatusFailed, repository.ErrPaymentMismatch.Error())
	}
	if failedTransaction != nil &&
		((failedTransaction.OrderID.Valid && failedTransaction.OrderID.Int64 != int64(attempt.OrderID)) ||
			(failedTransaction.AttemptID.Valid && failedTransaction.AttemptID.Int64 != int64(attempt.ID))) {
		return nil, repository.ErrPaymentMismatch
	}

	completed, err := a.repository.CompleteAttempt(ctx, repository.CompleteAttemptParams{
		WorkspaceID:       attempt.WorkspaceID,
		AttemptID:         attempt.ID,
		ProviderCode:      ProviderCode,
		ProviderPaymentID: utils.Ref(transfer.Comment),
		AmountMinor:       transfer.AmountMinor,
		AssetCode:         transfer.AssetCode,
	})
	if err != nil {
		// A blockchain transfer is irreversible. Keep it available for replay until
		// the local completion transaction succeeds instead of advancing the cursor
		// and permanently suppressing a paid order after a transient local error.
		return nil, err
	}
	if failedTransaction != nil {
		recovered, err := a.repository.RecoverFailedProviderTransaction(
			ctx,
			paymentsqlc.RecoverProviderTransactionParams{
				OrderID:     nullInt64FromUint64(completed.OrderID),
				AttemptID:   nullInt64FromUint64(completed.AttemptID),
				ID:          failedTransaction.ID,
				WorkspaceID: transfer.WorkspaceID,
			},
			providerCursorParams(transfer),
		)
		if err != nil {
			return nil, err
		}

		return &ProcessResult{
			OrderID:     completed.OrderID,
			AttemptID:   completed.AttemptID,
			Transaction: uint64(failedTransaction.ID),
			AlreadyDone: completed.AlreadyDone || !recovered,
		}, nil
	}

	result, err := a.storeTransfer(ctx, transfer, completed.OrderID, completed.AttemptID, paymentsqlc.PaymentProviderTransactionStatusMatched, "")
	if err != nil {
		return nil, err
	}
	result.AlreadyDone = completed.AlreadyDone
	return result, nil
}

func (a *TON) storeTransfer(ctx context.Context, transfer IncomingTransfer, orderID uint64, attemptID uint64, status paymentsqlc.PaymentProviderTransactionStatus, message string) (*ProcessResult, error) {

	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()

	ctx = mergedCtx
	cursor := providerCursorParams(transfer)

	occurredAt := transfer.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	id, err := a.repository.StoreProviderTransaction(ctx, paymentsqlc.CreateProviderTransactionParams{
		WorkspaceID:           transfer.WorkspaceID,
		ProviderCode:          ProviderCode,
		Network:               transfer.Network,
		SourceKey:             transfer.WalletAddress,
		AssetCode:             transfer.AssetCode,
		ExternalTransactionID: transfer.TxHash,
		SequenceNumber:        int64(transfer.LogicalTime),
		SourceAddress:         transfer.SourceAddress,
		DestinationAddress:    transfer.DestinationAddress,
		AmountMinor:           int64(transfer.AmountMinor),
		PaymentReference:      transfer.Comment,
		SenderReference:       nullString(transfer.JettonSender),
		OrderID:               nullInt64FromUint64(orderID),
		AttemptID:             nullInt64FromUint64(attemptID),
		Status:                status,
		Error:                 nullString(message),
		OccurredAt:            occurredAt,
	}, cursor)
	if isDuplicateEntry(err) && transfer.TxHash != "" {
		existing, existingErr := a.repository.GetProviderTransactionByExternalID(ctx, paymentsqlc.GetProviderTransactionByExternalIDParams{
			WorkspaceID:           transfer.WorkspaceID,
			ProviderCode:          ProviderCode,
			Network:               transfer.Network,
			SourceKey:             transfer.WalletAddress,
			ExternalTransactionID: transfer.TxHash,
		})
		if existingErr != nil {
			return nil, existingErr
		}
		if !providerTransactionMatchesTransfer(existing, transfer) {
			return nil, repository.ErrPaymentMismatch
		}
		if _, cursorErr := a.repository.UpsertProviderCursor(ctx, cursor); cursorErr != nil {
			return nil, cursorErr
		}
		return &ProcessResult{
			OrderID:     uint64FromRepositoryNull(existing.OrderID),
			AttemptID:   uint64FromRepositoryNull(existing.AttemptID),
			Transaction: uint64(existing.ID),
			AlreadyDone: true,
			Ignored:     existing.Status == string(paymentsqlc.PaymentProviderTransactionStatusIgnored),
		}, nil
	}
	if err != nil {
		return nil, err
	}

	return &ProcessResult{
		OrderID:     orderID,
		AttemptID:   attemptID,
		Transaction: id,
		Ignored:     status == paymentsqlc.PaymentProviderTransactionStatusIgnored,
	}, nil
}

func (a *TON) advanceTransferCursor(ctx context.Context, transfer IncomingTransfer) error {

	_, err := a.repository.UpsertProviderCursor(ctx, providerCursorParams(transfer))

	return err

}

func providerCursorParams(transfer IncomingTransfer) paymentsqlc.UpsertProviderCursorParams {

	return paymentsqlc.UpsertProviderCursorParams{
		WorkspaceID:    transfer.WorkspaceID,
		ProviderCode:   ProviderCode,
		Network:        transfer.Network,
		SourceKey:      transfer.WalletAddress,
		CursorValue:    strconv.FormatUint(transfer.LogicalTime, 10),
		CursorSequence: int64(transfer.LogicalTime),
	}

}

func providerTransactionMatchesTransfer(
	existing repository.AdminProviderTransactionModel,
	transfer IncomingTransfer,
) bool {

	return existing.WorkspaceID == transfer.WorkspaceID &&
		existing.ProviderCode == ProviderCode &&
		existing.Network == transfer.Network &&
		existing.SourceKey == transfer.WalletAddress &&
		existing.AssetCode == transfer.AssetCode &&
		existing.ExternalTransactionID == transfer.TxHash &&
		existing.SequenceNumber == int64(transfer.LogicalTime) &&
		existing.SourceAddress == transfer.SourceAddress &&
		existing.DestinationAddress == transfer.DestinationAddress &&
		existing.AmountMinor == int64(transfer.AmountMinor) &&
		existing.PaymentReference == transfer.Comment &&
		existing.SenderReference.Valid == (transfer.JettonSender != "") &&
		existing.SenderReference.String == transfer.JettonSender

}

func uint64FromRepositoryNull(value repository.NullableInt64) uint64 {
	if !value.Valid || value.Int64 <= 0 {
		return 0
	}
	return uint64(value.Int64)
}
