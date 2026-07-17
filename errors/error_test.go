package errors

import (
	"errors"
	"testing"
)

func TestErrorCodeAndMessage(t *testing.T) {
	err := New(CodeInvalidFields, "workspace is required")
	if err.Code() != CodeInvalidFields {
		t.Fatalf("unexpected code: %s", err.Code())
	}
	if err.Message() != "workspace is required" {
		t.Fatalf("unexpected message: %s", err.Message())
	}
	if err.Error() != "workspace is required" {
		t.Fatalf("unexpected error text: %s", err.Error())
	}
}

func TestErrorUnwrap(t *testing.T) {
	base := errors.New("sql failed")
	err := Wrap(CodeInternalError, "query failed", base)
	if !errors.Is(err, base) {
		t.Fatal("wrapped error must unwrap to base error")
	}
	if err.Unwrap() != base {
		t.Fatal("unexpected unwrap result")
	}
}

func TestErrorIsByCode(t *testing.T) {
	err := Wrap(CodeNotFound, "product not found", errors.New("sql: no rows"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("errors.Is must match by code")
	}
	if errors.Is(err, ErrConflict) {
		t.Fatal("errors.Is must not match different code")
	}
}

func TestErrorAs(t *testing.T) {
	err := Wrap(CodeConflict, "already exists", errors.New("duplicate key"))
	var target *Error
	if !errors.As(err, &target) {
		t.Fatal("errors.As must expose structured error")
	}
	if target.Code() != CodeConflict {
		t.Fatalf("unexpected code from As: %s", target.Code())
	}
	if target.Message() != "already exists" {
		t.Fatalf("unexpected message from As: %s", target.Message())
	}
}

func TestCodeAndMessageOf(t *testing.T) {
	err := Wrap(CodeTimeout, "request timed out", errors.New("context deadline exceeded"))
	if CodeOf(err) != CodeTimeout {
		t.Fatalf("unexpected code: %s", CodeOf(err))
	}
	if MessageOf(err) != "request timed out" {
		t.Fatalf("unexpected message: %s", MessageOf(err))
	}
}

func TestNormalizeKeepsStructuredErrors(t *testing.T) {
	base := New(CodeForbidden, "forbidden")
	err := Normalize(base, CodeInternalError, "wrapped")
	if !errors.Is(err, ErrForbidden) {
		t.Fatal("normalize must preserve structured errors")
	}
	if CodeOf(err) != CodeForbidden {
		t.Fatalf("unexpected code: %s", CodeOf(err))
	}
}
