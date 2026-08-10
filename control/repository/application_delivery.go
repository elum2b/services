package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	controlsqlc "github.com/elum2b/services/control/sqlc"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

const applicationDeliveryCacheTTL = time.Hour

type ApplicationDeliveryInput struct {
	WorkspaceID string
	AppID       int64
	PlatformID  int64
	URL         string
	Secret      string
	IsEnabled   bool
}

func (r *Repository) UpsertApplicationDelivery(
	ctx context.Context,
	actorID string,
	value ApplicationDeliveryInput,
) (ApplicationDelivery, error) {
	value.WorkspaceID = normalizeID(value.WorkspaceID)
	value.URL = strings.TrimSpace(value.URL)
	value.Secret = strings.TrimSpace(value.Secret)

	if err := validateApplicationDeliveryInput(value); err != nil {
		return ApplicationDelivery{}, err
	}

	secret, err := r.encryptSecret(value.Secret)
	if err != nil {
		return ApplicationDelivery{}, err
	}

	var result ApplicationDelivery

	err = r.withAuditDBTx(ctx, func(tx *sql.Tx, q *controlsqlc.Queries) error {
		if err := requireWorkspaceAuthorization(
			ctx,
			q,
			normalizeID(actorID),
			value.WorkspaceID,
			"",
			"control.workspace.update",
		); err != nil {
			return err
		}

		row, err := q.UpsertApplicationDelivery(
			ctx,
			controlsqlc.UpsertApplicationDeliveryParams{
				WorkspaceID:     value.WorkspaceID,
				AppID:           value.AppID,
				PlatformID:      value.PlatformID,
				Url:             value.URL,
				EncryptedSecret: secret,
				IsEnabled:       value.IsEnabled,
			},
		)
		if err != nil {
			return err
		}

		result = mapApplicationDeliveryUpsert(row)

		return nil
	})
	if err != nil {
		return ApplicationDelivery{}, err
	}

	if err := r.bumpApplicationDeliveryCacheVersion(
		value.WorkspaceID,
		value.AppID,
		value.PlatformID,
	); err != nil {
		return ApplicationDelivery{}, err
	}

	return result, nil
}

func (r *Repository) ListApplicationDeliveries(
	ctx context.Context,
	actorID, workspaceID string,
) ([]ApplicationDelivery, error) {
	workspaceID = normalizeID(workspaceID)
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	if err := r.requireWorkspaceAccess(
		ctx,
		normalizeID(actorID),
		workspaceID,
		"control.workspace.update",
	); err != nil {
		return nil, err
	}

	rows, err := r.q.ListApplicationDeliveries(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	result := make([]ApplicationDelivery, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapApplicationDeliveryList(row))
	}

	return result, nil
}

func (r *Repository) DeleteApplicationDelivery(
	ctx context.Context,
	actorID, workspaceID string,
	appID, platformID int64,
) (int64, error) {
	workspaceID = normalizeID(workspaceID)
	if err := validateApplicationKey(
		workspaceID,
		appID,
		platformID,
	); err != nil {
		return 0, err
	}

	var affected int64

	err := r.withAuditDBTx(ctx, func(tx *sql.Tx, q *controlsqlc.Queries) error {
		if err := requireWorkspaceAuthorization(
			ctx,
			q,
			normalizeID(actorID),
			workspaceID,
			"",
			"control.workspace.update",
		); err != nil {
			return err
		}

		var err error

		affected, err = q.DeleteApplicationDelivery(
			ctx,
			controlsqlc.DeleteApplicationDeliveryParams{
				WorkspaceID: workspaceID,
				AppID:       appID,
				PlatformID:  platformID,
			},
		)

		return err
	})
	if err != nil {
		return 0, err
	}

	if err := r.bumpApplicationDeliveryCacheVersion(
		workspaceID,
		appID,
		platformID,
	); err != nil {
		return affected, err
	}

	return affected, nil
}

func (r *Repository) GetApplicationDeliveryEndpoint(
	ctx context.Context,
	workspaceID string,
	appID, platformID int64,
) (ApplicationDeliveryEndpoint, error) {
	workspaceID = normalizeID(workspaceID)
	if err := validateApplicationKey(
		workspaceID,
		appID,
		platformID,
	); err != nil {
		return ApplicationDeliveryEndpoint{}, err
	}

	return sqlwrap.Query(
		ctx,
		r.db,
		sqlwrap.Params{
			KeyParts: []any{
				"control",
				"application-delivery",
				workspaceID,
				appID,
				platformID,
			},
			Timeout: r.timeout,
			CacheVersionScope: applicationDeliveryCacheScope(
				workspaceID,
				appID,
				platformID,
			),
			CacheL1Delay: applicationDeliveryCacheTTL,
			CacheL2Delay: applicationDeliveryCacheTTL,
		},
		func(ctx context.Context) (ApplicationDeliveryEndpoint, error) {
			row, err := r.q.GetApplicationDelivery(
				ctx,
				controlsqlc.GetApplicationDeliveryParams{
					WorkspaceID: workspaceID,
					AppID:       appID,
					PlatformID:  platformID,
				},
			)
			if err != nil {
				return ApplicationDeliveryEndpoint{}, noRows(err, ErrNotFound)
			}

			secret, err := r.decryptSecret(row.EncryptedSecret)
			if err != nil {
				return ApplicationDeliveryEndpoint{}, err
			}

			return ApplicationDeliveryEndpoint{
				ApplicationDelivery: mapApplicationDelivery(row),
				Secret:              secret,
			}, nil
		},
	)
}

func validateApplicationDeliveryInput(value ApplicationDeliveryInput) error {
	if err := validateApplicationKey(
		value.WorkspaceID,
		value.AppID,
		value.PlatformID,
	); err != nil {
		return err
	}

	parsed, err := url.Parse(value.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" ||
		parsed.User != nil {
		return fmt.Errorf(
			"control application delivery URL must be an absolute HTTPS URL without credentials",
		)
	}

	if value.Secret == "" {
		return fmt.Errorf("control application delivery secret is required")
	}

	return nil
}

func applicationDeliveryCacheScope(
	workspaceID string,
	appID, platformID int64,
) []any {
	return []any{
		"control",
		"application-delivery",
		workspaceID,
		appID,
		platformID,
	}
}

func (r *Repository) bumpApplicationDeliveryCacheVersion(
	workspaceID string,
	appID, platformID int64,
) error {
	return r.db.BumpCacheVersion(
		applicationDeliveryCacheScope(workspaceID, appID, platformID)...)
}

func mapApplicationDelivery(
	value controlsqlc.ControlApplicationDelivery,
) ApplicationDelivery {
	return ApplicationDelivery{
		WorkspaceID: value.WorkspaceID,
		AppID:       value.AppID,
		PlatformID:  value.PlatformID,
		URL:         value.Url,
		IsEnabled:   value.IsEnabled,
		CreatedAt:   value.CreatedAt,
		UpdatedAt:   value.UpdatedAt,
	}
}

func mapApplicationDeliveryUpsert(
	value controlsqlc.UpsertApplicationDeliveryRow,
) ApplicationDelivery {
	return ApplicationDelivery{
		WorkspaceID: value.WorkspaceID,
		AppID:       value.AppID,
		PlatformID:  value.PlatformID,
		URL:         value.Url,
		IsEnabled:   value.IsEnabled,
		CreatedAt:   value.CreatedAt,
		UpdatedAt:   value.UpdatedAt,
	}
}

func mapApplicationDeliveryList(
	value controlsqlc.ListApplicationDeliveriesRow,
) ApplicationDelivery {
	return ApplicationDelivery{
		WorkspaceID: value.WorkspaceID,
		AppID:       value.AppID,
		PlatformID:  value.PlatformID,
		URL:         value.Url,
		IsEnabled:   value.IsEnabled,
		CreatedAt:   value.CreatedAt,
		UpdatedAt:   value.UpdatedAt,
	}
}
