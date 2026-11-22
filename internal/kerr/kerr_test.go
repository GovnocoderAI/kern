package kerr

import (
	"context"
	"errors"
	"testing"
)

func TestCodeOf(t *testing.T) {
	if got := CodeOf(nil); got != CodeNone {
		t.Fatalf("nil = %v", got)
	}
	if got := CodeOf(User("bad")); got != CodeUser {
		t.Fatalf("User = %v", got)
	}
	if got := CodeOf(Provider("down")); got != CodeProvider {
		t.Fatalf("Provider = %v", got)
	}
	if got := CodeOf(Permission("denied")); got != CodePermission {
		t.Fatalf("Permission = %v", got)
	}
	if got := CodeOf(Cancelled()); got != CodeCancelled {
		t.Fatalf("Cancelled = %v", got)
	}
	if got := CodeOf(context.Canceled); got != CodeCancelled {
		t.Fatalf("context.Canceled = %v", got)
	}
	if got := CodeOf(errors.New("mystery")); got != CodeInternal {
		t.Fatalf("plain = %v", got)
	}
}

func TestUnwrap(t *testing.T) {
	cause := errors.New("root")
	w := Wrap(CodeProvider, cause, "upstream failed")
	if !errors.Is(w, cause) {
		t.Fatal("expected Unwrap to reach the cause")
	}
	if !errors.As(w, new(*Error)) {
		t.Fatal("expected errors.As to find *Error")
	}
}

func TestExitCode(t *testing.T) {
	if got := ExitCode(nil); got != 0 {
		t.Fatalf("nil = %d", got)
	}
	if got := ExitCode(User("x")); got != 2 {
		t.Fatalf("User = %d", got)
	}
	if got := ExitCode(Cancelled()); got != 4 {
		t.Fatalf("Cancelled = %d", got)
	}
}
