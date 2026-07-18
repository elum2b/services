package repository

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

type Account struct {
	ID          string
	DisplayName string
	Status      controlmodel.AccountStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type AuthPrincipal struct {
	Account
	PlatformStatus   controlmodel.MembershipStatus
	WorkspaceLimit   int32
	TwoFactorEnabled bool
}

type AuthCompletion struct {
	Account            Account
	Session            Session
	SessionToken       string
	TwoFactorRequired  bool
	TwoFactorChallenge string
	Created            bool
}

type Identity struct {
	AccountID       string
	Provider        string
	ProviderSubject string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Session struct {
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

type Platform struct {
	OwnerAccountID string
	InitializedBy  string
	InitializedAt  time.Time
	UpdatedAt      time.Time
}

type PlatformMember struct {
	AccountID           string
	DisplayName         string
	Status              controlmodel.MembershipStatus
	WorkspaceLimit      int32
	OwnedWorkspaceCount int64
	InvitedBy           string
	JoinedAt            time.Time
	UpdatedAt           time.Time
}

type Workspace struct {
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

type Member struct {
	WorkspaceID string
	AccountID   string
	DisplayName string
	IsOwner     bool
	RoleIDs     []string
	JoinedAt    time.Time
	UpdatedAt   time.Time
}

type Role struct {
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

type Invite struct {
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

type LimitRequest struct {
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

type Method struct {
	Key           string
	Service       string
	GroupKey      string
	Scope         AccessScope
	GroupPosition int32
	Position      int32
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type MethodGroup struct {
	Service   string
	Key       string
	Position  int32
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AccessCatalogRow struct {
	Service            string
	ServiceTitle       string
	ServiceDescription string
	ServicePosition    int32
	GroupKey           string
	GroupTitle         string
	GroupDescription   string
	GroupPosition      int32
	Key                string
	Scope              AccessScope
	Title              string
	Desc               string
	Position           int32
}

type AuditEvent struct {
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

type Cursor struct {
	Time time.Time
	ID   string
}

type TwoFactorSetup struct {
	Secret string
	URI    string
}
