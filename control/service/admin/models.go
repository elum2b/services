package admin

import (
	json "github.com/goccy/go-json"
	"time"
)

type Page struct {
	Limit, Offset int32
}

type AccountModel struct {
	ID, DisplayName, Status string
	CreatedAt, UpdatedAt    time.Time
}

type IdentityModel struct {
	AccountID, Provider, Subject string
	CreatedAt, UpdatedAt         time.Time
}

type SessionModel struct {
	ID, AccountID, IP, UserAgent string
	BindToIP                     bool
	ExpiresAt                    time.Time
	RevokedAt                    *time.Time
	LastUsedAt, CreatedAt        time.Time
}

type AuthIdentityParams struct {
	Provider, Subject, DisplayName string
	Payload                        json.RawMessage
	IP, UserAgent                  string
	BindToIP                       bool
	ExpiresAt                      time.Time
}

type AuthResult struct {
	Account            AccountModel
	Session            SessionModel
	SessionToken       string
	TwoFactorRequired  bool
	TwoFactorChallenge string
	Created            bool
}

type TwoFactorSetupModel struct{ Secret, URI string }

type WorkspaceModel struct {
	ID, Slug, Title, Status, CreatedBy string
	CreatedAt, UpdatedAt               time.Time
}

type MemberModel struct {
	WorkspaceID, AccountID, DisplayName string
	Position                            int32
	JoinedAt, UpdatedAt                 time.Time
}

type InviteModel struct {
	ID, WorkspaceID, CreatedBy string
	MaxUses, UsedCount         *uint32
	ExpiresAt, RevokedAt       *time.Time
	CreatedAt                  time.Time
	RoleIDs                    []string
}

type AuditEventModel struct {
	ID, WorkspaceID, ActorID, MethodKey, TargetType, TargetID, Result, RequestID string
	BeforeData, AfterData                                                        json.RawMessage
	OccurredAt                                                                   time.Time
}

type RoleModel struct {
	ID, WorkspaceID, Code, Title, Description string
	Position                                  int32
	IsOwner                                   bool
	MemberCount                               int64
	CreatedAt, UpdatedAt                      time.Time
}

type MethodModel struct {
	Key, Service, GroupKey string
	CreatedAt, UpdatedAt   time.Time
}

type AccessModel struct {
	Key, Title, Desc string
}

type AccessGroups struct {
	Key, Title, Description string
	Accesses                []AccessModel
}

type AccessGroupModel struct {
	Service, Title, Description string
	Groups                      []AccessGroups
}

type CreateWorkspaceParams struct {
	ActorID, ID, Slug, Title string
}

type UpdateWorkspaceParams struct {
	ActorID, WorkspaceID, Slug, Title, Status string
}

type CreateRoleParams struct {
	ActorID, ID, WorkspaceID, Code, Title, Description string
	Position                                           int32
}

type UpdateRoleParams struct {
	ActorID, ID, WorkspaceID, Title, Description string
	Position                                     int32
}

type SetRoleMemberParams struct {
	ActorID, WorkspaceID, AccountID, RoleID string
}

type CreateInviteParams struct {
	ActorID, WorkspaceID string
	RoleIDs              []string
	ExpiresAt            *time.Time
	MaxUses              *uint32
}

type SetRolePermissionParams struct {
	ActorID, WorkspaceID, RoleID, MethodKey string
	Enabled                                 bool
}
