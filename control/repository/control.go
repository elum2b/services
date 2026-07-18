package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	controlsqlc "github.com/elum2b/services/control/sqlc"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

func (r *Repository) RegisterMethod(ctx context.Context, method Method) error {

	return r.RegisterMethods(ctx, []Method{method})

}

func (r *Repository) RegisterMethods(ctx context.Context, methods []Method) error {

	if len(methods) == 0 {
		return nil
	}

	err := sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries {
			return controlsqlc.New(tx)
		},
		func(_ *sql.Tx, q *controlsqlc.Queries) error {
			if err := q.LockMethodRegistry(ctx); err != nil {
				return err
			}

			for _, method := range methods {
				if err := validateMethod(method); err != nil {
					return err
				}
				stored, err := q.GetMethod(ctx, method.Key)
				if err == nil && stored.Service != method.Service {
					return ErrMethodOwner
				}
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}
				if err := validateMethodNamespace(method); err != nil {
					return err
				}
				groupExists, err := q.MethodGroupExists(
					ctx,
					controlsqlc.MethodGroupExistsParams{
						Service:  method.Service,
						GroupKey: method.GroupKey,
					},
				)
				if err != nil {
					return err
				}
				if !groupExists {
					return ErrInvalidArgument
				}
				if err := q.UpsertMethodGroup(ctx, controlsqlc.UpsertMethodGroupParams{
					Service:  method.Service,
					GroupKey: method.GroupKey,
					Position: method.GroupPosition,
				}); err != nil {
					return err
				}
				if err := q.UpsertMethod(ctx, controlsqlc.UpsertMethodParams{
					MethodKey: method.Key,
					Service:   method.Service,
					GroupKey:  method.GroupKey,
					Position:  method.Position,
				}); err != nil {
					return err
				}
			}

			return nil
		},
	)
	if err != nil {
		return err
	}

	r.bumpCacheVersion("control", "access-catalog")

	return nil

}

func (r *Repository) ListMethodGroups(ctx context.Context) ([]MethodGroup, error) {

	rows, err := r.q.ListMethodGroups(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]MethodGroup, 0, len(rows))
	for _, row := range rows {
		result = append(result, MethodGroup{
			Service:   row.Service,
			Key:       row.GroupKey,
			Position:  row.Position,
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		})
	}

	return result, nil

}

func (r *Repository) ListAccessCatalog(
	ctx context.Context,
	locale string,
	scope AccessScope,
) ([]AccessCatalogRow, error) {

	if scope != "" && scope != ScopeGlobal && scope != ScopeWorkspace {
		return nil, ErrInvalidArgument
	}

	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               "control:access-catalog:" + locale + ":" + string(scope),
		Timeout:           r.timeout,
		CacheVersionScope: []any{"control", "access-catalog"},
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
	}, func(ctx context.Context) ([]AccessCatalogRow, error) {
		rows, err := r.q.ListAccessCatalog(ctx, controlsqlc.ListAccessCatalogParams{
			Locale: locale,
			Scope:  string(scope),
		})
		if err != nil {
			return nil, err
		}

		result := make([]AccessCatalogRow, 0, len(rows))
		for _, row := range rows {
			result = append(result, AccessCatalogRow{
				Service:            row.Service,
				ServiceTitle:       row.ServiceTitle,
				ServiceDescription: row.ServiceDescription,
				ServicePosition:    row.ServicePosition,
				GroupKey:           row.GroupKey,
				GroupTitle:         row.GroupTitle,
				GroupDescription:   row.GroupDescription,
				GroupPosition:      row.GroupPosition,
				Key:                row.MethodKey,
				Scope:              AccessScope(valueString(row.Scope)),
				Title:              row.AccessTitle,
				Desc:               row.AccessDescription,
				Position:           row.Position,
			})
		}

		return result, nil
	})

}

func (r *Repository) ListMethods(ctx context.Context) ([]Method, error) {

	rows, err := r.q.ListMethods(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]Method, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapMethod(row))
	}

	return result, nil

}

func (r *Repository) GetMethod(ctx context.Context, methodKey string) (Method, error) {

	row, err := r.q.GetMethod(ctx, methodKey)
	if err != nil {
		return Method{}, noRows(err, ErrMethodNotFound)
	}

	return mapMethod(row), nil

}

func (r *Repository) CheckGlobalAccess(
	ctx context.Context,
	accountID string,
	methodKey string,
) (bool, error) {

	return r.q.CheckGlobalAccess(ctx, controlsqlc.CheckGlobalAccessParams{
		AccountID: accountID,
		MethodKey: methodKey,
	})

}

func (r *Repository) CheckWorkspaceAccess(
	ctx context.Context,
	accountID string,
	workspaceID string,
	methodKey string,
) (bool, error) {

	return r.q.CheckWorkspaceAccess(ctx, controlsqlc.CheckWorkspaceAccessParams{
		AccountID:   accountID,
		WorkspaceID: workspaceID,
		MethodKey:   methodKey,
	})

}

func (r *Repository) ListAuthorizedGlobalMethods(
	ctx context.Context,
	accountID string,
) ([]Method, error) {

	rows, err := r.q.ListAuthorizedGlobalMethods(ctx, accountID)
	if err != nil {
		return nil, err
	}

	result := make([]Method, 0, len(rows))
	for _, row := range rows {
		result = append(result, Method{
			Key:      row.MethodKey,
			Service:  row.Service,
			GroupKey: row.GroupKey,
			Scope:    AccessScope(valueString(row.Scope)),
			Position: row.Position,
		})
	}

	return result, nil

}

func (r *Repository) ListAuthorizedWorkspaceMethods(
	ctx context.Context,
	accountID string,
	workspaceID string,
) ([]Method, error) {

	rows, err := r.q.ListAuthorizedWorkspaceMethods(
		ctx,
		controlsqlc.ListAuthorizedWorkspaceMethodsParams{
			WorkspaceID: workspaceID,
			AccountID:   accountID,
		},
	)
	if err != nil {
		return nil, err
	}

	result := make([]Method, 0, len(rows))
	for _, row := range rows {
		result = append(result, Method{
			Key:      row.MethodKey,
			Service:  row.Service,
			GroupKey: row.GroupKey,
			Scope:    AccessScope(valueString(row.Scope)),
			Position: row.Position,
		})
	}

	return result, nil

}

func validateMethod(method Method) error {

	if err := required(method.Key, method.Service, method.GroupKey); err != nil {
		return err
	}
	if method.Scope != "" {
		expected := ScopeWorkspace
		if len(method.Key) >= len("control.global.") && method.Key[:len("control.global.")] == "control.global." {
			expected = ScopeGlobal
		}
		if method.Scope != expected {
			return ErrInvalidArgument
		}
	}

	return nil

}

func validateMethodNamespace(method Method) error {

	if !strings.HasPrefix(method.Key, method.Service+".") {
		return ErrInvalidArgument
	}

	return nil

}

func mapMethod(row controlsqlc.ControlMethod) Method {

	return Method{
		Key:       row.MethodKey,
		Service:   row.Service,
		GroupKey:  row.GroupKey,
		Scope:     AccessScope(valueString(row.Scope)),
		Position:  row.Position,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}

}
