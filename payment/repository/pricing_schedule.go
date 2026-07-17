package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"

	serviceerrors "github.com/elum2b/services/errors"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

const (
	AssetRateSourceDexScreener = "dexscreener"
)

var (
	ErrInvalidAutoUpdateConfig = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"payment asset rate auto-update configuration is invalid",
	)
	ErrAssetRateScheduleNotFound = serviceerrors.New(
		serviceerrors.CodeNotFound,
		"payment asset rate schedule not found",
	)
)

type AssetRateAutoUpdateParams struct {
	AssetCode          string
	ReferenceAssetCode string
	Enabled            bool
	Source             string
	SourceChainID      string
	SourceTokenAddress *string
}

type DueAssetRateUpdate struct {
	AssetCode          string
	ReferenceAssetCode string
	Source             string
	SourceChainID      string
	SourceTokenAddress string
	AssetKind          string
}

func (r *PaymentRepository) SyncAutomaticAssetRates(ctx context.Context) (int64, error) {
	return r.q.SyncAutomaticAssetRates(ctx, USDTAssetCode)
}

func (r *PaymentRepository) ConfigureAssetRateAutoUpdate(
	ctx context.Context,
	params AssetRateAutoUpdateParams,
) error {
	params.AssetCode = strings.TrimSpace(params.AssetCode)
	params.ReferenceAssetCode = strings.TrimSpace(params.ReferenceAssetCode)
	params.Source = strings.ToLower(strings.TrimSpace(params.Source))
	params.SourceChainID = strings.TrimSpace(params.SourceChainID)
	if params.Enabled && (params.Source != AssetRateSourceDexScreener || params.SourceChainID == "") {
		return ErrInvalidAutoUpdateConfig
	}

	sourceAddress := sql.NullString{}
	if params.SourceTokenAddress != nil {
		value := strings.TrimSpace(*params.SourceTokenAddress)
		sourceAddress = sql.NullString{String: value, Valid: value != ""}
	}
	rows, err := r.q.ConfigureAssetRateAutoUpdate(ctx, paymentsqlc.ConfigureAssetRateAutoUpdateParams{
		AutoUpdateEnabled:  params.Enabled,
		AutoUpdateSource:   sql.NullString{String: params.Source, Valid: params.Source != ""},
		SourceChainID:      sql.NullString{String: params.SourceChainID, Valid: params.SourceChainID != ""},
		SourceTokenAddress: sourceAddress,
		AssetCode:          params.AssetCode,
		ReferenceAssetCode: params.ReferenceAssetCode,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrAssetRateScheduleNotFound
	}
	return nil
}

func (r *PaymentRepository) ClaimDueAssetRateUpdates(
	ctx context.Context,
	workerID string,
	limit int32,
	lease time.Duration,
) ([]DueAssetRateUpdate, error) {
	if limit <= 0 {
		limit = 300
	}
	if lease <= 0 {
		lease = time.Minute
	}
	rows, err := r.q.ListDueAssetRateUpdates(ctx, limit)
	if err != nil {
		return nil, err
	}
	claimed := make([]DueAssetRateUpdate, 0, len(rows))
	for _, row := range rows {
		if !row.AutoUpdateSource.Valid || !row.SourceChainID.Valid {
			continue
		}
		affected, err := r.q.ClaimAssetRateUpdate(ctx, paymentsqlc.ClaimAssetRateUpdateParams{
			LeaseOwner:         sql.NullString{String: workerID, Valid: true},
			Column2:            int32(lease / time.Second),
			AssetCode:          row.AssetCode,
			ReferenceAssetCode: row.ReferenceAssetCode,
		})
		if err != nil {
			return nil, err
		}
		if affected == 0 {
			continue
		}
		claimed = append(claimed, DueAssetRateUpdate{
			AssetCode:          row.AssetCode,
			ReferenceAssetCode: row.ReferenceAssetCode,
			Source:             row.AutoUpdateSource.String,
			SourceChainID:      row.SourceChainID.String,
			SourceTokenAddress: strings.TrimSpace(row.SourceTokenAddress.String),
			AssetKind:          string(row.AssetKind),
		})
	}
	return claimed, nil
}

func (r *PaymentRepository) CompleteAssetRateAutoUpdate(
	ctx context.Context,
	workerID string,
	update DueAssetRateUpdate,
) error {
	_, err := r.q.CompleteAssetRateUpdate(ctx, paymentsqlc.CompleteAssetRateUpdateParams{
		AssetCode:          update.AssetCode,
		ReferenceAssetCode: update.ReferenceAssetCode,
		LeaseOwner:         sql.NullString{String: workerID, Valid: true},
	})
	return err
}

func (r *PaymentRepository) FailAssetRateAutoUpdate(
	ctx context.Context,
	workerID string,
	update DueAssetRateUpdate,
	updateErr error,
) error {
	message := "unknown price update error"
	if updateErr != nil {
		message = updateErr.Error()
	}
	if len(message) > 4000 {
		message = message[:4000]
	}
	_, err := r.q.FailAssetRateUpdate(ctx, paymentsqlc.FailAssetRateUpdateParams{
		LastError:          sql.NullString{String: message, Valid: true},
		AssetCode:          update.AssetCode,
		ReferenceAssetCode: update.ReferenceAssetCode,
		LeaseOwner:         sql.NullString{String: workerID, Valid: true},
	})
	return err
}
