package internalapi

import (
	"context"
	"strings"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/control/repository"
	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	json "github.com/goccy/go-json"
)

type Internal struct {
	rootCtx    context.Context
	repository *repository.Repository
}

type MethodManifest struct {
	Key, Service, GroupKey string
}

type AccessRequest struct {
	AccountID, WorkspaceID, MethodKey string
}

type AuthorizedMethod struct {
	Key, Service, GroupKey string
}

type AuditEventParams struct {
	WorkspaceID string
	ActorID     string
	MethodKey   string
	TargetType  string
	TargetID    string
	Result      string
	RequestID   string
	BeforeData  json.RawMessage
	AfterData   json.RawMessage
}

func NewWithOptions(ctx context.Context, db *sqlwrap.Client, options repository.Options) *Internal {
	return &Internal{rootCtx: contextutil.Normalize(ctx), repository: repository.NewWithOptions(db, options)}
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

func (i *Internal) RegisterManifest(ctx context.Context, values []MethodManifest) error {
	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()
	methods := make([]repository.Method, 0, len(values))
	for _, value := range values {
		methods = append(methods, repository.Method{
			Key:      strings.TrimSpace(value.Key),
			Service:  strings.TrimSpace(value.Service),
			GroupKey: strings.TrimSpace(value.GroupKey),
		})
	}

	return i.repository.RegisterMethods(mergedCtx, methods)
}

func (i *Internal) CheckAccess(ctx context.Context, value AccessRequest) (bool, error) {
	if err := services.ValidateWorkspaceID(value.WorkspaceID); err != nil {
		return false, err
	}

	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()
	return i.repository.CheckAccess(
		mergedCtx,
		strings.TrimSpace(value.AccountID),
		value.WorkspaceID,
		strings.TrimSpace(value.MethodKey),
	)
}

func (i *Internal) GetAuthorizedMethods(
	ctx context.Context,
	accountID, workspaceID string,
) ([]AuthorizedMethod, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()
	methods, err := i.repository.ListAuthorizedMethods(
		mergedCtx,
		strings.TrimSpace(accountID),
		workspaceID,
	)
	if err != nil {
		return nil, err
	}
	result := make([]AuthorizedMethod, 0, len(methods))
	for _, method := range methods {
		result = append(result, AuthorizedMethod{Key: method.Key, Service: method.Service, GroupKey: method.GroupKey})
	}
	return result, nil
}

// AppendAudit records an action performed by a trusted API orchestrator in
// another service. Control's own mutations write audit events transactionally.
func (i *Internal) AppendAudit(ctx context.Context, params AuditEventParams) error {
	if params.WorkspaceID != "" {
		if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
			return err
		}
	}

	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	return i.repository.AppendAudit(mergedCtx, repository.AuditEvent{
		WorkspaceID: strings.TrimSpace(params.WorkspaceID),
		ActorID:     strings.TrimSpace(params.ActorID),
		MethodKey:   strings.TrimSpace(params.MethodKey),
		TargetType:  strings.TrimSpace(params.TargetType),
		TargetID:    strings.TrimSpace(params.TargetID),
		Result:      strings.TrimSpace(params.Result),
		RequestID:   strings.TrimSpace(params.RequestID),
		BeforeData:  params.BeforeData,
		AfterData:   params.AfterData,
	})
}
