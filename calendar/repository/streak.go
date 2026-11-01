package repository

import "time"

// The continuation window follows the calendar's availability interval,
// independently of step resets and of restarting a completed reward sequence.
func streakWindow(calendar Calendar, progress Progress) (time.Time, time.Time, error) {

	next, err := nextAvailableAt(calendar, *progress.LastClaimAt)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if calendar.Mode == ModeInterval && progress.NextClaimAt != nil {
		next = *progress.NextClaimAt
	}
	end := next
	if calendar.IntervalType == IntervalCalendar {
		location, err := time.LoadLocation(calendar.Timezone)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		end = end.In(location)
	}
	end = addInterval(end, calendar.IntervalUnit, calendar.IntervalCount)
	return next, end.UTC(), nil

}

func currentIntervalStreak(progress Progress, calendar Calendar, now time.Time) (uint64, error) {

	if progress.LastClaimAt == nil {
		return 0, nil
	}
	_, end, err := streakWindow(calendar, progress)
	if err != nil {
		return 0, err
	}
	if !now.Before(end) {
		return 0, nil
	}
	return progress.StreakCount, nil

}

func grantedIntervalStreak(progress Progress, calendar Calendar, now time.Time) (uint64, error) {

	if progress.LastClaimAt == nil || progress.StreakCount == 0 {
		return 1, nil
	}
	next, end, err := streakWindow(calendar, progress)
	if err != nil {
		return 0, err
	}
	if now.Before(next) {
		return progress.StreakCount, nil
	}
	if now.Before(end) {
		return progress.StreakCount + 1, nil
	}
	return 1, nil

}
