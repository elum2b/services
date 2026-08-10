package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"

	controlsqlc "github.com/elum2b/services/control/sqlc"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

const applicationCacheTTL = time.Hour

type ApplicationPlatformInput struct {
	WorkspaceID                 string
	AppID                       int64
	PlatformID                  int64
	Provider                    ApplicationProvider
	Secret                      string
	MaxAuthenticationAgeSeconds int32
	IsEnabled                   bool
}

func (r *Repository) UpsertApplicationPlatform(
	ctx context.Context,
	actorID string,
	value ApplicationPlatformInput,
) (ApplicationPlatform, error) {
	value.WorkspaceID = normalizeID(value.WorkspaceID)
	value.Secret = strings.TrimSpace(value.Secret)

	if err := validateApplicationPlatformInput(value); err != nil {
		return ApplicationPlatform{}, err
	}

	encryptedSecret, err := r.encryptSecret(value.Secret)
	if err != nil {
		return ApplicationPlatform{}, err
	}

	var result ApplicationPlatform

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

		row, err := q.UpsertApplicationPlatform(
			ctx,
			controlsqlc.UpsertApplicationPlatformParams{
				WorkspaceID:                 value.WorkspaceID,
				AppID:                       value.AppID,
				PlatformID:                  value.PlatformID,
				Provider:                    string(value.Provider),
				EncryptedSecret:             encryptedSecret,
				MaxAuthenticationAgeSeconds: value.MaxAuthenticationAgeSeconds,
				IsEnabled:                   value.IsEnabled,
			},
		)
		if err != nil {
			return err
		}

		result = mapApplicationPlatformUpsert(row)

		return nil
	})
	if err != nil {
		return ApplicationPlatform{}, err
	}

	if err := r.bumpApplicationCacheVersion(
		value.WorkspaceID,
		value.AppID,
		value.PlatformID,
	); err != nil {
		return ApplicationPlatform{}, err
	}

	return result, nil
}

func (r *Repository) ListApplicationPlatforms(
	ctx context.Context,
	actorID string,
	workspaceID string,
) ([]ApplicationPlatform, error) {
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

	rows, err := r.q.ListApplicationPlatforms(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	result := make([]ApplicationPlatform, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapApplicationPlatformList(row))
	}

	return result, nil
}

func (r *Repository) DeleteApplicationPlatform(
	ctx context.Context,
	actorID string,
	workspaceID string,
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

		affected, err = q.DeleteApplicationPlatform(
			ctx,
			controlsqlc.DeleteApplicationPlatformParams{
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

	if err := r.bumpApplicationCacheVersion(
		workspaceID,
		appID,
		platformID,
	); err != nil {
		return affected, err
	}

	return affected, nil
}

func (r *Repository) GetApplicationAuthentication(
	ctx context.Context,
	workspaceID string,
	appID, platformID int64,
) (ApplicationAuthentication, error) {
	workspaceID = normalizeID(workspaceID)
	if err := validateApplicationKey(
		workspaceID,
		appID,
		platformID,
	); err != nil {
		return ApplicationAuthentication{}, err
	}

	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		KeyParts: []any{
			"control",
			"application-auth",
			workspaceID,
			appID,
			platformID,
		},
		Timeout: r.timeout,
		CacheVersionScope: applicationCacheScope(
			workspaceID,
			appID,
			platformID,
		),
		CacheL1Delay: applicationCacheTTL,
		CacheL2Delay: applicationCacheTTL,
	}, func(ctx context.Context) (ApplicationAuthentication, error) {
		row, err := r.q.GetApplicationPlatform(
			ctx,
			controlsqlc.GetApplicationPlatformParams{
				WorkspaceID: workspaceID,
				AppID:       appID,
				PlatformID:  platformID,
			},
		)
		if err != nil {
			return ApplicationAuthentication{}, noRows(err, ErrNotFound)
		}

		secret, err := r.decryptSecret(row.EncryptedSecret)
		if err != nil {
			return ApplicationAuthentication{}, err
		}

		return ApplicationAuthentication{
			ApplicationPlatform: mapApplicationPlatformGet(row),
			Secret:              secret,
		}, nil
	})
}

func (r *Repository) requireWorkspaceAccess(
	ctx context.Context,
	actorID, workspaceID, methodKey string,
) error {
	return sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries {
			return controlsqlc.New(tx)
		},
		func(_ *sql.Tx, q *controlsqlc.Queries) error {
			return requireWorkspaceAuthorization(
				ctx,
				q,
				actorID,
				workspaceID,
				"",
				methodKey,
			)
		},
	)
}

func validateApplicationPlatformInput(value ApplicationPlatformInput) error {
	if err := validateApplicationKey(
		value.WorkspaceID,
		value.AppID,
		value.PlatformID,
	); err != nil {
		return err
	}

	if value.Secret == "" ||
		(value.Provider != ApplicationProviderVKMA && value.Provider != ApplicationProviderTMA) ||
		value.MaxAuthenticationAgeSeconds < 1 ||
		value.MaxAuthenticationAgeSeconds > 86400 {
		return ErrInvalidArgument
	}

	return nil
}

func validateApplicationKey(workspaceID string, appID, platformID int64) error {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return err
	}

	if appID <= 0 || platformID <= 0 {
		return ErrInvalidArgument
	}

	return nil
}

func applicationCacheScope(workspaceID string, appID, platformID int64) []any {
	return []any{"control", "application-auth", workspaceID, appID, platformID}
}

func (r *Repository) bumpApplicationCacheVersion(
	workspaceID string,
	appID, platformID int64,
) error {
	if r == nil || r.db == nil {
		return nil
	}

	err := r.db.BumpCacheVersion(
		applicationCacheScope(workspaceID, appID, platformID)...)
	if err != nil && r.onCacheInvalidationError != nil {
		func() {
			defer func() { _ = recover() }()

			r.onCacheInvalidationError(err)
		}()
	}

	return err
}

func mapApplicationPlatform(
	value controlsqlc.ControlApplicationPlatform,
) ApplicationPlatform {
	return ApplicationPlatform{
		WorkspaceID:                 value.WorkspaceID,
		AppID:                       value.AppID,
		PlatformID:                  value.PlatformID,
		Provider:                    ApplicationProvider(value.Provider),
		MaxAuthenticationAgeSeconds: value.MaxAuthenticationAgeSeconds,
		IsEnabled:                   value.IsEnabled,
		CreatedAt:                   value.CreatedAt,
		UpdatedAt:                   value.UpdatedAt,
	}
}

func mapApplicationPlatformUpsert(
	value controlsqlc.UpsertApplicationPlatformRow,
) ApplicationPlatform {
	return ApplicationPlatform{
		WorkspaceID:                 value.WorkspaceID,
		AppID:                       value.AppID,
		PlatformID:                  value.PlatformID,
		Provider:                    ApplicationProvider(value.Provider),
		MaxAuthenticationAgeSeconds: value.MaxAuthenticationAgeSeconds,
		IsEnabled:                   value.IsEnabled,
		CreatedAt:                   value.CreatedAt,
		UpdatedAt:                   value.UpdatedAt,
	}
}

func mapApplicationPlatformList(
	value controlsqlc.ListApplicationPlatformsRow,
) ApplicationPlatform {
	return ApplicationPlatform{
		WorkspaceID:                 value.WorkspaceID,
		AppID:                       value.AppID,
		PlatformID:                  value.PlatformID,
		Provider:                    ApplicationProvider(value.Provider),
		MaxAuthenticationAgeSeconds: value.MaxAuthenticationAgeSeconds,
		IsEnabled:                   value.IsEnabled,
		CreatedAt:                   value.CreatedAt,
		UpdatedAt:                   value.UpdatedAt,
	}
}

func mapApplicationPlatformGet(
	value controlsqlc.ControlApplicationPlatform,
) ApplicationPlatform {
	return mapApplicationPlatform(value)
}
