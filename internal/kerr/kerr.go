// Package kerr classifies errors so frontends can map them to stable exit
// codes and user-friendly messages. The underlying state is a small Code;
// concrete errors are constructed with the typed helpers below.
package kerr

import (
	"context"
	"errors"
	"fmt"
)

// Code is a stable, machine-readable error class.
type Code int

const (
	// CodeNone is not an error.
	CodeNone Code = 0
	// CodeInternal is a bug or unexpected state.
	CodeInternal Code = 1
	// CodeUser is bad input or misuse (missing flags, invalid config, no model).
	CodeUser Code = 2
	// CodeProvider is a failure from the model provider (auth, quota, HTTP).
	CodeProvider Code = 3
	// CodeCancelled is a clean interruption (Ctrl-C, context deadline).
	CodeCancelled Code = 4
	// CodePermission is a denial by the permission policy.
	CodePermission Code = 5
)

// Error is a classified error.
type Error struct {
	Code    Code
	Message string
	Err     error
}

// Unwrap supports errors.Is/As through the underlying cause.
func (e *Error) Unwrap() error { return e.Err }

// Error implements fmt.Stringer.
func (e *Error) Error() string {
	if e.Err != nil && e.Message == "" {
		return e.Err.Error()
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// CodeOf extracts the classification from an error, defaulting to CodeInternal.
func CodeOf(err error) Code {
	if err == nil {
		return CodeNone
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	if errors.Is(err, context.Canceled) {
		return CodeCancelled
	}
	return CodeInternal
}

// contextCancelled is a shared sentinel so callers can mark interruptions
// without importing internal details.
var contextCancelled = errors.New("canceled")

// ExitCode returns the process exit code for an error.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	return int(CodeOf(err))
}

// Newf builds a classified error.
func Newf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap classifies an existing error.
func Wrap(code Code, err error, msg string) *Error {
	return &Error{Code: code, Message: msg, Err: err}
}

// Internal formats a bug.
func Internal(format string, args ...any) *Error { return Newf(CodeInternal, format, args...) }

// User formats a misuse.
func User(format string, args ...any) *Error { return Newf(CodeUser, format, args...) }

// Provider formats a provider failure.
func Provider(format string, args ...any) *Error { return Newf(CodeProvider, format, args...) }

// Permission formats a policy denial.
func Permission(format string, args ...any) *Error { return Newf(CodePermission, format, args...) }

// Cancelled marks a clean interruption.
func Cancelled() *Error {
	return &Error{Code: CodeCancelled, Message: "canceled", Err: contextCancelled}
}

// IsCancelled reports whether err represents a clean cancellation.
func IsCancelled(err error) bool {
	return errors.Is(err, contextCancelled)
}
