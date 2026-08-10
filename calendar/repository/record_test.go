package repository

import (
	"database/sql"
	"testing"
)

func TestNonNegativeProgressPosition(t *testing.T) {

	if got := nonNegativeUint32(
		sql.NullInt32{Int32: -1, Valid: true},
	); got != 0 {
		t.Fatalf("negative current position = %d, want 0", got)
	}
	if got := sqlNullUint32Ptr(
		sql.NullInt32{Int32: -1, Valid: true},
	); got != nil {
		t.Fatalf("negative optional position = %v, want nil", *got)
	}
	if got := nonNegativeUint32(
		sql.NullInt32{Int32: 3, Valid: true},
	); got != 3 {
		t.Fatalf("current position = %d, want 3", got)
	}

}
