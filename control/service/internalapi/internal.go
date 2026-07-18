package internalapi

import (
	"context"
	"strings"

	controlmodel "github.com/elum2b/services/control/model"
	"github.com/elum2b/services/control/repository"
	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	json "github.com/goccy/go-json"
)

type AccessScope string

const (
	ScopeGlobal    AccessScope = "global"
	ScopeWorkspace AccessScope = "workspace"
)

type Internal struct {
	rootCtx    context.Context
	repository *repository.Repository
}

type MethodManifest struct {
	Key           string
	Service       string
	GroupKey      string
	GroupPosition int32
	Position      int32
}

type GlobalAccessRequest struct {
	AccountID string
	MethodKey string
}

type WorkspaceAccessRequest struct {
	AccountID   string
	WorkspaceID string
	MethodKey   string
}

type AuthorizedMethod struct {
	Key      string
	Service  string
	GroupKey string
	Scope    AccessScope
	Position int32
}

type AuditEventParams struct {
	Scope       AccessScope
	WorkspaceID string
	ActorID     string
	MethodKey   string
	TargetType  string
	TargetID    string
	Result      controlmodel.AuditResult
	RequestID   string
	BeforeData  json.RawMessage
	AfterData   json.RawMessage
}

func NewWithOptions(
	ctx context.Context,
	db *sqlwrap.Client,
	options repository.Options,
) *Internal {

	return &Internal{
		rootCtx:    contextutil.Normalize(ctx),
		repository: repository.NewWithOptions(db, options),
	}

}

func (i *Internal) Close() error {

	if i == nil || i.repository == nil {
		return nil
	}

	return i.repository.Close()

}

func (i *Internal) withContext(ctx context.Context) (context.Context, context.CancelFunc) {

	return contextutil.Merge(i.rootCtx, ctx)

}

func (i *Internal) RegisterManifest(
	ctx context.Context,
	values []MethodManifest,
) error {

	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	methods := make([]repository.Method, 0, len(values))
	for _, value := range values {
		methods = append(methods, repository.Method{
			Key:           strings.TrimSpace(value.Key),
			Service:       strings.TrimSpace(value.Service),
			GroupKey:      strings.TrimSpace(value.GroupKey),
			GroupPosition: value.GroupPosition,
			Position:      value.Position,
		})
	}

	return i.repository.RegisterMethods(mergedCtx, methods)

}

func (i *Internal) CheckGlobalAccess(
	ctx context.Context,
	value GlobalAccessRequest,
) (bool, error) {

	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	return i.repository.CheckGlobalAccess(
		mergedCtx,
		strings.TrimSpace(value.AccountID),
		strings.TrimSpace(value.MethodKey),
	)

}

func (i *Internal) CheckWorkspaceAccess(
	ctx context.Context,
	value WorkspaceAccessRequest,
) (bool, error) {

	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	return i.repository.CheckWorkspaceAccess(
		mergedCtx,
		strings.TrimSpace(value.AccountID),
		strings.TrimSpace(value.WorkspaceID),
		strings.TrimSpace(value.MethodKey),
	)

}

func (i *Internal) GetAuthorizedGlobalMethods(
	ctx context.Context,
	accountID string,
) ([]AuthorizedMethod, error) {

	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	methods, err := i.repository.ListAuthorizedGlobalMethods(
		mergedCtx,
		strings.TrimSpace(accountID),
	)
	if err != nil {
		return nil, err
	}

	return mapAuthorizedMethods(methods), nil

}

func (i *Internal) GetAuthorizedWorkspaceMethods(
	ctx context.Context,
	accountID string,
	workspaceID string,
) ([]AuthorizedMethod, error) {

	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	methods, err := i.repository.ListAuthorizedWorkspaceMethods(
		mergedCtx,
		strings.TrimSpace(accountID),
		strings.TrimSpace(workspaceID),
	)
	if err != nil {
		return nil, err
	}

	return mapAuthorizedMethods(methods), nil

}

func (i *Internal) AppendAudit(ctx context.Context, params AuditEventParams) error {

	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	return i.repository.AppendAudit(mergedCtx, repository.AuditEvent{
		Scope:       repository.AccessScope(params.Scope),
		WorkspaceID: strings.TrimSpace(params.WorkspaceID),
		ActorID:     strings.TrimSpace(params.ActorID),
		MethodKey:   strings.TrimSpace(params.MethodKey),
		TargetType:  strings.TrimSpace(params.TargetType),
		TargetID:    strings.TrimSpace(params.TargetID),
		Result:      params.Result,
		RequestID:   strings.TrimSpace(params.RequestID),
		BeforeData:  params.BeforeData,
		AfterData:   params.AfterData,
	})

}

func mapAuthorizedMethods(values []repository.Method) []AuthorizedMethod {

	result := make([]AuthorizedMethod, 0, len(values))
	for _, value := range values {
		result = append(result, AuthorizedMethod{
			Key:      value.Key,
			Service:  value.Service,
			GroupKey: value.GroupKey,
			Scope:    AccessScope(value.Scope),
			Position: value.Position,
		})
	}

	return result

}
