package calendar

import "github.com/elum2b/services/calendar/repository"

const (
	ModeInterval        = repository.ModeInterval
	ModeSequential      = repository.ModeSequential
	ModeSequentialReset = repository.ModeSequentialReset

	IntervalCalendar = repository.IntervalCalendar
	IntervalFloating = repository.IntervalFloating

	EndRestart    = repository.EndRestart
	EndRepeatLast = repository.EndRepeatLast
	EndStop       = repository.EndStop

	StatusGranted      = repository.StatusGranted
	StatusNotFound     = repository.StatusNotFound
	StatusInactive     = repository.StatusInactive
	StatusNotStarted   = repository.StatusNotStarted
	StatusExpired      = repository.StatusExpired
	StatusNotAvailable = repository.StatusNotAvailable
	StatusCompleted    = repository.StatusCompleted
	StatusNoSteps      = repository.StatusNoSteps
)
