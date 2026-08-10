package repository

import (
	"testing"
	"time"
)

func TestPeriodForFixedDurationWindows(t *testing.T) {
	t.Parallel()

	now := periodAnchor.Add(2*time.Hour + 17*time.Minute + 43*time.Second)
	tests := []struct {
		name       string
		resetUnit  string
		resetEvery uint32
		wantStart  time.Time
		wantEnd    time.Time
	}{
		{
			name:       "seconds",
			resetUnit:  ResetSecond,
			resetEvery: 30,
			wantStart: periodAnchor.Add(
				2*time.Hour + 17*time.Minute + 30*time.Second,
			),
			wantEnd: periodAnchor.Add(2*time.Hour + 18*time.Minute),
		},
		{
			name:       "minutes",
			resetUnit:  ResetMinute,
			resetEvery: 15,
			wantStart:  periodAnchor.Add(2*time.Hour + 15*time.Minute),
			wantEnd:    periodAnchor.Add(2*time.Hour + 30*time.Minute),
		},
		{
			name:       "hours",
			resetUnit:  ResetHour,
			resetEvery: 2,
			wantStart:  periodAnchor.Add(2 * time.Hour),
			wantEnd:    periodAnchor.Add(4 * time.Hour),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			start, end := periodFor(Task{
				ResetUnit:  test.resetUnit,
				ResetEvery: test.resetEvery,
			}, now)
			if !start.Equal(test.wantStart) {
				t.Fatalf("start = %s, want %s", start, test.wantStart)
			}

			if !end.Equal(test.wantEnd) {
				t.Fatalf("end = %s, want %s", end, test.wantEnd)
			}
		})
	}
}
