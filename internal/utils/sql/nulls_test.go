package sql

import (
	"database/sql"
	"testing"
	"time"
)

func TestNullHelpers(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	if got := ValueFromPtr(&now); !got.Equal(now) {
		t.Fatalf("expected %v from ValueFromPtr, got %v", now, got)
	}
	if got := ValueFromPtr[*time.Time](nil); got != nil {
		t.Fatalf("expected nil pointer from ValueFromPtr for nil input, got %v", got)
	}
	if got := ValueFromPtr[int64](nil); got != 0 {
		t.Fatalf("expected zero int64 from ValueFromPtr for nil input, got %d", got)
	}

	nString := NullFromPtr(stringPtr("x"), func(v string) sql.NullString {
		return sql.NullString{String: v, Valid: true}
	})
	if !nString.Valid || nString.String != "x" {
		t.Fatalf("expected valid null string 'x', got %+v", nString)
	}
	if nString := NullFromPtr[string, sql.NullString](nil, func(v string) sql.NullString {
		return sql.NullString{String: v, Valid: true}
	}); nString.Valid {
		t.Fatalf("expected zero null string for nil input, got %+v", nString)
	}

	if got := NullTimePtr(sql.NullTime{Valid: false}); got != nil {
		t.Fatalf("expected nil time pointer, got %v", got)
	}

	ts := NullTimePtr(sql.NullTime{Time: now, Valid: true})
	if ts == nil || !ts.Equal(now) {
		t.Fatalf("expected %v, got %v", now, ts)
	}

	s := NullStringPtr(sql.NullString{String: "x", Valid: true})
	if s == nil || *s != "x" {
		t.Fatalf("expected string pointer 'x', got %v", s)
	}
	if s := NullStringPtr(sql.NullString{Valid: false}); s != nil {
		t.Fatalf("expected nil string pointer, got %v", s)
	}

	i64 := NullInt64Ptr(sql.NullInt64{Int64: 7, Valid: true})
	if i64 == nil || *i64 != 7 {
		t.Fatalf("expected uint64 pointer 7, got %v", i64)
	}
	if i64 := NullInt64Ptr(sql.NullInt64{Valid: false}); i64 != nil {
		t.Fatalf("expected nil int64 pointer, got %v", i64)
	}

	i32 := NullInt32Ptr(sql.NullInt32{Int32: 3, Valid: true})
	if i32 == nil || *i32 != 3 {
		t.Fatalf("expected uint64 pointer 3, got %v", i32)
	}
	if i32 := NullInt32Ptr(sql.NullInt32{Valid: false}); i32 != nil {
		t.Fatalf("expected nil int32 pointer, got %v", i32)
	}

	if v := NullBoolToInt(sql.NullBool{Bool: true, Valid: true}); v != 1 {
		t.Fatalf("expected 1, got %d", v)
	}
	if v := NullBoolToInt(sql.NullBool{Bool: false, Valid: true}); v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}

	nTime := NullTimeFromPtr(&now)
	if !nTime.Valid || !nTime.Time.Equal(now) {
		t.Fatalf("expected valid null time %v, got %+v", now, nTime)
	}
	if nTime := NullTimeFromPtr(nil); nTime.Valid {
		t.Fatalf("expected invalid null time for nil ptr, got %+v", nTime)
	}
	zero := time.Time{}
	if nTime := NullTimeFromPtr(&zero); nTime.Valid {
		t.Fatalf("expected invalid null time for zero ptr, got %+v", nTime)
	}

	nInt := NullInt64FromUint64(9, true)
	if !nInt.Valid || nInt.Int64 != 9 {
		t.Fatalf("expected valid null int64 9, got %+v", nInt)
	}
	if nInt := NullInt64FromUint64(9, false); nInt.Valid {
		t.Fatalf("expected invalid null int64, got %+v", nInt)
	}
}

func stringPtr(v string) *string {
	return &v
}
