package repository

import "testing"

func TestPayableAmountMinorRejectsInvalidAmounts(t *testing.T) {
	for _, value := range []struct {
		list     int64
		discount int64
		want     uint64
	}{
		{list: 100, discount: 25, want: 75},
		{list: 100, discount: 101, want: 0},
		{list: -1, discount: 0, want: 0},
		{list: 1, discount: -1, want: 0},
	} {
		if got := payableAmountMinor(
			value.list,
			value.discount,
		); got != value.want {
			t.Fatalf(
				"payableAmountMinor(%d, %d) = %d, want %d",
				value.list,
				value.discount,
				got,
				value.want,
			)
		}
	}
}
