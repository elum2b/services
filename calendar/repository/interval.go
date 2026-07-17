package repository

import (
	"fmt"
	"time"
)

func addInterval(value time.Time, unit string, count uint32) time.Time {
	if count == 0 {
		count = 1
	}
	n := int(count)
	switch unit {
	case "second":
		return value.Add(time.Duration(n) * time.Second)
	case "minute":
		return value.Add(time.Duration(n) * time.Minute)
	case "hour":
		return value.Add(time.Duration(n) * time.Hour)
	case "day":
		return value.AddDate(0, 0, n)
	case "week":
		return value.AddDate(0, 0, 7*n)
	case "month":
		return value.AddDate(0, n, 0)
	default:
		return value
	}
}

func nextAvailableAt(calendar Calendar, lastClaim time.Time) (time.Time, error) {
	if calendar.IntervalType == IntervalFloating {
		return addInterval(lastClaim, calendar.IntervalUnit, calendar.IntervalCount), nil
	}
	boundary, err := calendarBoundary(calendar, lastClaim)
	if err != nil {
		return time.Time{}, err
	}
	return addInterval(boundary, calendar.IntervalUnit, calendar.IntervalCount).UTC(), nil
}

func calendarBoundary(calendar Calendar, value time.Time) (time.Time, error) {
	location, err := time.LoadLocation(calendar.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("calendar: invalid timezone %q: %w", calendar.Timezone, err)
	}
	local := value.In(location)
	var boundary time.Time
	switch calendar.IntervalUnit {
	case "second":
		boundary = time.Date(
			local.Year(),
			local.Month(),
			local.Day(),
			local.Hour(),
			local.Minute(),
			local.Second(),
			0,
			location,
		)
	case "minute":
		boundary = time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), 0, 0, location)
	case "hour":
		boundary = time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), 0, 0, 0, location)
	case "day":
		boundary = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	case "week":
		dayOffset := (int(local.Weekday()) + 6) % 7
		start := local.AddDate(0, 0, -dayOffset)
		boundary = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, location)
	case "month":
		boundary = time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
	default:
		return time.Time{}, fmt.Errorf("calendar: unsupported interval unit %q", calendar.IntervalUnit)
	}
	return boundary.UTC(), nil
}

func intervalIndex(calendar Calendar, now time.Time) (uint64, time.Time, error) {
	anchor := calendar.CreatedAt
	if calendar.StartAt != nil {
		anchor = *calendar.StartAt
	}
	if now.Before(anchor) {
		return 0, anchor, nil
	}
	if calendar.IntervalType == IntervalCalendar {
		var err error
		anchor, err = calendarBoundary(calendar, anchor)
		if err != nil {
			return 0, time.Time{}, err
		}
	}
	if calendar.IntervalUnit != "month" {
		next := addInterval(anchor, calendar.IntervalUnit, calendar.IntervalCount)
		duration := next.Sub(anchor)
		if duration > 0 {
			index := uint64(now.Sub(anchor)/duration) + 1
			return index, anchor.Add(time.Duration(index) * duration), nil
		}
	}
	index := uint64(1)
	next := addInterval(anchor, calendar.IntervalUnit, calendar.IntervalCount)
	for !now.Before(next) {
		index++
		next = addInterval(next, calendar.IntervalUnit, calendar.IntervalCount)
	}
	return index, next, nil
}
