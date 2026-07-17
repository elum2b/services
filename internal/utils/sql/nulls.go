package sql

import (
	"database/sql"
	"time"
)

func ValueFromPtr[T any](value *T) T {
	var zero T
	if value == nil {
		return zero
	}
	return *value
}

func NullFromPtr[T any, N any](value *T, mapFn func(T) N) N {
	var zero N
	if value == nil {
		return zero
	}
	return mapFn(*value)
}

func NullTimePtr(t sql.NullTime) *time.Time {
	if t.Valid {
		v := t.Time
		return &v
	}
	return nil
}

func NullStringPtr(s sql.NullString) *string {
	if s.Valid {
		v := s.String
		return &v
	}
	return nil
}

func NullInt64Ptr(v sql.NullInt64) *uint64 {
	if v.Valid {
		vv := uint64(v.Int64)
		return &vv
	}
	return nil
}

func NullInt32Ptr(v sql.NullInt32) *uint64 {
	if v.Valid {
		vv := uint64(v.Int32)
		return &vv
	}
	return nil
}

func NullTimeFromPtr(t *time.Time) sql.NullTime {
	if t != nil && !t.IsZero() {
		return sql.NullTime{Time: *t, Valid: true}
	}
	return sql.NullTime{Valid: false}
}

func NullBoolToInt(v sql.NullBool) int {
	if v.Valid && v.Bool {
		return 1
	}
	return 0
}

func NullInt64FromUint64(v uint64, valid bool) sql.NullInt64 {
	if !valid {
		return sql.NullInt64{Valid: false}
	}
	return sql.NullInt64{Int64: int64(v), Valid: true}
}
