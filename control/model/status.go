package model

type AccountStatus string

const (
	AccountStatusActive   AccountStatus = "active"
	AccountStatusDisabled AccountStatus = "disabled"
)

type MembershipStatus string

const (
	MembershipStatusActive  MembershipStatus = "active"
	MembershipStatusRemoved MembershipStatus = "removed"
)

type WorkspaceStatus string

const (
	WorkspaceStatusActive   WorkspaceStatus = "active"
	WorkspaceStatusArchived WorkspaceStatus = "archived"
)

type LimitRequestStatus string

const (
	LimitRequestStatusPending   LimitRequestStatus = "pending"
	LimitRequestStatusApproved  LimitRequestStatus = "approved"
	LimitRequestStatusRejected  LimitRequestStatus = "rejected"
	LimitRequestStatusCancelled LimitRequestStatus = "cancelled"
)

type AuditResult string

const (
	AuditResultSucceeded AuditResult = "succeeded"
	AuditResultFailed    AuditResult = "failed"
)
