package cpa

import "github.com/elum2b/services/cpa/model"

type AssignmentStatus = model.AssignmentStatus

const (
	AssignmentStatusIssued    = model.AssignmentStatusIssued
	AssignmentStatusCompleted = model.AssignmentStatusCompleted
)

type CodeStatus = model.CodeStatus

const (
	CodeStatusAvailable = model.CodeStatusAvailable
	CodeStatusIssued    = model.CodeStatusIssued
	CodeStatusCompleted = model.CodeStatusCompleted
	CodeStatusDeleted   = model.CodeStatusDeleted
)

type AssignmentEventType = model.AssignmentEventType

const (
	AssignmentEventTypeIssued    = model.AssignmentEventTypeIssued
	AssignmentEventTypeCompleted = model.AssignmentEventTypeCompleted
)
