package admin

import (
	"time"

	controlmodel "github.com/elum2b/services/control/model"
	json "github.com/goccy/go-json"
)

type AccessScope string

const (
	ScopeGlobal    AccessScope = "global"
	ScopeWorkspace AccessScope = "workspace"
)

type InviteKind string

const (
	InviteKindGlobal    InviteKind = "global"
	InviteKindWorkspace InviteKind = "workspace"
)

type LimitKind string

const (
	LimitKindAccountWorkspace  LimitKind = "account_workspace"
	LimitKindWorkspaceEmployee LimitKind = "workspace_employee"
)

type Page struct {
	Limit    int32
	CursorAt time.Time
	CursorID string
}

type AccountModel struct {
	ID          string
	DisplayName string
	Status      controlmodel.AccountStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type IdentityModel struct {
	AccountID string
	Provider  string
	Subject   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type SessionModel struct {
	ID         string
	AccountID  string
	IP         string
	UserAgent  string
	BindToIP   bool
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	LastUsedAt time.Time
	CreatedAt  time.Time
}

type AuthIdentityParams struct {
	Provider    string
	Subject     string
	DisplayName string
	Payload     json.RawMessage
	InviteToken string
	IP          string
	UserAgent   string
	BindToIP    bool
	ExpiresAt   time.Time
}

type AuthResult struct {
	Account            AccountModel
	Session            SessionModel
	SessionToken       string
	TwoFactorRequired  bool
	TwoFactorChallenge string
	Created            bool
}

type TwoFactorSetupModel struct {
	Secret string
	URI    string
}

type WorkspaceModel struct {
	ID             string
	Slug           string
	Title          string
	Status         controlmodel.WorkspaceStatus
	CreatedBy      string
	OwnerAccountID string
	EmployeeLimit  int32
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type PlatformMemberModel struct {
	AccountID           string
	DisplayName         string
	Status              controlmodel.MembershipStatus
	WorkspaceLimit      int32
	OwnedWorkspaceCount int64
	InvitedBy           string
	JoinedAt            time.Time
	UpdatedAt           time.Time
}

type MemberModel struct {
	WorkspaceID string
	AccountID   string
	DisplayName string
	IsOwner     bool
	RoleIDs     []string
	JoinedAt    time.Time
	UpdatedAt   time.Time
}

type InviteModel struct {
	ID          string
	Kind        InviteKind
	WorkspaceID string
	CreatedBy   string
	ExpiresAt   *time.Time
	AcceptedBy  string
	AcceptedAt  *time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
	RoleIDs     []string
}

type LimitRequestModel struct {
	ID             string
	Kind           LimitKind
	AccountID      string
	WorkspaceID    string
	CurrentLimit   int32
	RequestedLimit int32
	ApprovedLimit  *int32
	Reason         string
	Status         controlmodel.LimitRequestStatus
	RequestedBy    string
	ReviewedBy     string
	ReviewComment  string
	CreatedAt      time.Time
	ReviewedAt     *time.Time
}

type AuditEventModel struct {
	ID          string
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
	OccurredAt  time.Time
}

type RoleModel struct {
	ID          string
	WorkspaceID string
	Code        string
	Title       string
	Description string
	Position    int32
	MemberCount int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type MethodModel struct {
	Key       string
	Service   string
	GroupKey  string
	Scope     AccessScope
	Position  int32
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AccessModel struct {
	Key   string
	Scope AccessScope
	Title string
	Desc  string
}

type AccessGroups struct {
	Key         string
	Title       string
	Description string
	Accesses    []AccessModel
}

type AccessGroupModel struct {
	Service     string
	Title       string
	Description string
	Groups      []AccessGroups
}

type CreateWorkspaceParams struct {
	ActorID string
	ID      string
	Slug    string
	Title   string
}

type UpdateWorkspaceParams struct {
	ActorID     string
	WorkspaceID string
	Slug        string
	Title       string
}

type CreateRoleParams struct {
	ActorID     string
	ID          string
	WorkspaceID string
	Code        string
	Title       string
	Description string
	Position    int32
}

type UpdateRoleParams struct {
	ActorID     string
	ID          string
	WorkspaceID string
	Title       string
	Description string
	Position    int32
}

type SetRoleMemberParams struct {
	ActorID     string
	WorkspaceID string
	AccountID   string
	RoleID      string
}

type CreateInviteParams struct {
	ActorID     string
	WorkspaceID string
	RoleIDs     []string
	ExpiresAt   *time.Time
}

type ReplaceRolePermissionsParams struct {
	ActorID     string
	WorkspaceID string
	RoleID      string
	MethodKeys  []string
}

type ResolveLimitRequestParams struct {
	ActorID       string
	RequestID     string
	Approved      bool
	ApprovedLimit int32
	Comment       string
}
