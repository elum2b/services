package model

type AssignmentStatus string

const (
	AssignmentStatusIssued    AssignmentStatus = "issued"
	AssignmentStatusCompleted AssignmentStatus = "completed"
)

type CodeStatus string

const (
	CodeStatusAvailable CodeStatus = "available"
	CodeStatusIssued    CodeStatus = "issued"
	CodeStatusCompleted CodeStatus = "completed"
	CodeStatusDeleted   CodeStatus = "deleted"
)

type AssignmentEventType string

const (
	AssignmentEventTypeIssued    AssignmentEventType = "issued"
	AssignmentEventTypeCompleted AssignmentEventType = "completed"
)
