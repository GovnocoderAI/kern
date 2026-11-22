package permission

import (
	"testing"

	"github.com/kern-ai/kern/internal/config"
)

func TestDefaults(t *testing.T) {
	m := New(nil, false, nil)
	if got := m.Check("read", "any"); got != Allow {
		t.Fatalf("read = %v", got)
	}
	if got := m.Check("shell", "ls"); got != Ask {
		t.Fatalf("shell = %v", got)
	}
	if got := m.Check("edit", "a.go"); got != Ask {
		t.Fatalf("edit = %v", got)
	}
}

func TestRulesAndGlobs(t *testing.T) {
	m := New([]config.PermissionRule{
		{Action: "shell", Resource: "git push *", Effect: "deny"},
		{Action: "shell", Resource: "git status", Effect: "allow"},
	}, false, nil)
	if got := m.Check("shell", "git push origin main"); got != Deny {
		t.Fatalf("push = %v", got)
	}
	if got := m.Check("shell", "git status"); got != Allow {
		t.Fatalf("status = %v", got)
	}
	if got := m.Check("shell", "rm -rf /"); got != Ask {
		t.Fatalf("rm = %v", got)
	}
}

func TestAutoAndDeny(t *testing.T) {
	m := New([]config.PermissionRule{{Action: "shell", Resource: "rm *", Effect: "deny"}}, true, nil)
	if ok, _ := m.Request("shell", "ls"); !ok {
		t.Fatal("auto should allow ask")
	}
	if ok, _ := m.Request("shell", "rm -rf /"); ok {
		t.Fatal("explicit deny must win")
	}
}

func TestNonInteractiveDeniesAsk(t *testing.T) {
	m := New(nil, false, nil)
	if ok, err := m.Request("shell", "ls"); ok || err == nil {
		t.Fatalf("expected denial with error, got ok=%v err=%v", ok, err)
	}
}
