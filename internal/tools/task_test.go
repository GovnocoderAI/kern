package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/GovnocoderAI/kern/internal/provider"
)

// fakeRunner records the request it received and returns a canned result.
type fakeRunner struct {
	got     SubagentRequest
	out     string
	usage   provider.Usage
	steps   int
	wantErr error
}

func (f *fakeRunner) run(ctx context.Context, req SubagentRequest) (*SubagentResult, error) {
	f.got = req
	if f.wantErr != nil {
		return nil, f.wantErr
	}
	return &SubagentResult{Output: f.out, Usage: f.usage, Steps: f.steps, Role: req.Role}, nil
}

func TestTaskToolDelegates(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir, "")
	fr := &fakeRunner{
		out:   "the plan is: read a.go then edit it",
		usage: provider.Usage{TotalTokens: 123},
		steps: 3,
	}
	r.Env().Subagent = fr.run

	raw := json.RawMessage(`{"role":"plan","prompt":"plan the change","max_steps":5}`)
	out, err := r.Run(context.Background(), "task", raw)
	if err != nil {
		t.Fatal(err)
	}
	if fr.got.Role != "plan" || fr.got.Prompt != "plan the change" {
		t.Fatalf("runner got %+v", fr.got)
	}
	if fr.got.Budget.MaxSteps != 5 {
		t.Fatalf("max_steps override not passed: %+v", fr.got.Budget)
	}
	if !strings.Contains(out, "the plan is") || !strings.Contains(out, "tokens=123") {
		t.Fatalf("task output = %q", out)
	}
}

func TestTaskToolRequiresRunner(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir, "")
	_, err := r.Run(context.Background(), "task", json.RawMessage(`{"role":"plan","prompt":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected unavailability error, got %v", err)
	}
}

func TestTaskToolValidatesArgs(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir, "")
	r.Env().Subagent = (&fakeRunner{}).run

	if _, err := r.Run(context.Background(), "task", json.RawMessage(`{"prompt":"x"}`)); err == nil {
		t.Fatal("expected error for missing role")
	}
	if _, err := r.Run(context.Background(), "task", json.RawMessage(`{"role":"plan"}`)); err == nil {
		t.Fatal("expected error for missing prompt")
	}
}

func TestTaskToolPropagatesRunnerError(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir, "")
	r.Env().Subagent = (&fakeRunner{wantErr: errors.New("budget exhausted")}).run

	_, err := r.Run(context.Background(), "task", json.RawMessage(`{"role":"build","prompt":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "budget exhausted") {
		t.Fatalf("error not propagated: %v", err)
	}
}

func TestRegistrySubset(t *testing.T) {
	dir := t.TempDir()
	full := NewRegistry(dir, "")
	sub := full.Subset([]string{"read", "grep", "nonexistent"})

	if !sub.Has("read") || !sub.Has("grep") {
		t.Fatal("subset missing expected tools")
	}
	if sub.Has("bash") || sub.Has("write") {
		t.Fatal("subset leaked excluded tools")
	}
	if sub.Has("nonexistent") {
		t.Fatal("subset kept an unknown tool")
	}
	// Subset shares the Env so processes/changes remain visible.
	if sub.Env() != full.Env() {
		t.Fatal("subset must share the parent Env")
	}
}

func TestBudgetPassedThrough(t *testing.T) {
	// A zero max_steps in the request means "use the role default"; the tool
	// must not invent one of its own.
	dir := t.TempDir()
	r := NewRegistry(dir, "")
	fr := &fakeRunner{out: "ok"}
	r.Env().Subagent = fr.run
	if _, err := r.Run(context.Background(), "task", json.RawMessage(`{"role":"test","prompt":"run"}`)); err != nil {
		t.Fatal(err)
	}
	if fr.got.Budget.MaxSteps != 0 {
		t.Fatalf("max_steps should be unset, got %d", fr.got.Budget.MaxSteps)
	}
}
