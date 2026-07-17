package importexport

import (
	"errors"
	"reflect"
	"testing"
)

func TestBatchSize(t *testing.T) {
	tests := []struct {
		name             string
		parametersPerRow int
		limits           BatchLimits
		want             int
	}{
		{
			name:             "row limit",
			parametersPerRow: 12,
			limits:           DefaultBatchLimits,
			want:             1000,
		},
		{
			name:             "parameter limit",
			parametersPerRow: 500,
			limits: BatchLimits{
				MaxRows:       1000,
				MaxParameters: 60000,
			},
			want: 120,
		},
		{
			name:             "at least one row",
			parametersPerRow: 70000,
			limits:           DefaultBatchLimits,
			want:             1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := BatchSize(test.parametersPerRow, test.limits); got != test.want {
				t.Fatalf("BatchSize() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestForEachBatch(t *testing.T) {
	var got [][2]int
	err := ForEachBatch(2501, 12, DefaultBatchLimits, func(start, end int) error {
		got = append(got, [2]int{start, end})
		return nil
	})
	if err != nil {
		t.Fatalf("iterate batches: %v", err)
	}
	want := [][2]int{{0, 1000}, {1000, 2000}, {2000, 2501}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ranges = %v, want %v", got, want)
	}
}

func TestForEachBatchStopsOnError(t *testing.T) {
	want := errors.New("stop")
	err := ForEachBatch(1001, 12, DefaultBatchLimits, func(start, end int) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("ForEachBatch() error = %v, want %v", err, want)
	}
}

func TestForEachBatchRejectsNilCallback(t *testing.T) {
	err := ForEachBatch(1, 1, DefaultBatchLimits, nil)
	if !errors.Is(err, ErrBatchCallbackRequired) {
		t.Fatalf("ForEachBatch() error = %v, want %v", err, ErrBatchCallbackRequired)
	}
}
