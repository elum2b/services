package errors

import "errors"

const (
	CodeInvalidFields      = "INVALID_FIELDS"
	CodeUnauthorized       = "UNAUTHORIZED"
	CodeForbidden          = "FORBIDDEN"
	CodeNotFound           = "NOT_FOUND"
	CodeConflict           = "CONFLICT"
	CodeInternalError      = "INTERNAL_ERROR"
	CodeNotReady           = "NOT_READY"
	CodeTimeout            = "TIMEOUT"
	CodeUnavailable        = "UNAVAILABLE"
	CodeUnsupported        = "UNSUPPORTED"
	CodeRateLimit          = "RATE_LIMIT"
	CodeDuplicate          = "DUPLICATE"
	CodeFailedPrecondition = "FAILED_PRECONDITION"
)

type Coded interface {
	error
	Code() string
	Message() string
}

type Error struct {
	code    string
	message string
	err     error
}

func New(code, message string) *Error {
	return &Error{code: code, message: message}
}

func Wrap(code, message string, err error) *Error {
	if err == nil {
		return New(code, message)
	}
	var coded Coded
	if errors.As(err, &coded) && coded.Code() == code && coded.Message() == message {
		if structured, ok := err.(*Error); ok {
			return structured
		}
		return &Error{code: code, message: message, err: err}
	}
	return &Error{code: code, message: message, err: err}
}

func CodeOf(err error) string {
	var coded Coded
	if errors.As(err, &coded) {
		return coded.Code()
	}
	return ""
}

func MessageOf(err error) string {
	var coded Coded
	if errors.As(err, &coded) {
		return coded.Message()
	}
	return ""
}

func IsStructured(err error) bool {
	var coded Coded
	return errors.As(err, &coded)
}

func Normalize(err error, code, message string) error {
	if err == nil {
		return nil
	}
	if IsStructured(err) {
		return err
	}
	return Wrap(code, message, err)
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.err == nil {
		return e.message
	}
	if e.message == "" {
		return e.err.Error()
	}
	return e.message + ": " + e.err.Error()
}

func (e *Error) Code() string {
	if e == nil {
		return ""
	}
	return e.code
}

func (e *Error) Message() string {
	if e == nil {
		return ""
	}
	return e.message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *Error) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}
	var coded Coded
	if errors.As(target, &coded) {
		return e.code != "" && e.code == coded.Code()
	}
	return false
}

var (
	ErrInvalidFields      = New(CodeInvalidFields, "invalid fields")
	ErrUnauthorized       = New(CodeUnauthorized, "unauthorized")
	ErrForbidden          = New(CodeForbidden, "forbidden")
	ErrNotFound           = New(CodeNotFound, "not found")
	ErrConflict           = New(CodeConflict, "conflict")
	ErrInternalError      = New(CodeInternalError, "internal error")
	ErrNotReady           = New(CodeNotReady, "service is not ready")
	ErrTimeout            = New(CodeTimeout, "timeout")
	ErrUnavailable        = New(CodeUnavailable, "service is unavailable")
	ErrUnsupported        = New(CodeUnsupported, "unsupported operation")
	ErrRateLimit          = New(CodeRateLimit, "rate limit exceeded")
	ErrDuplicate          = New(CodeDuplicate, "duplicate")
	ErrFailedPrecondition = New(CodeFailedPrecondition, "failed precondition")
)
