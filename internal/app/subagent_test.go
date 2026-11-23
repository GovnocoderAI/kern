package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GovnocoderAI/kern/internal/agent"
	"github.com/GovnocoderAI/kern/internal/config"
	"github.com/GovnocoderAI/kern/internal/permission"
	"github.com/GovnocoderAI/kern/internal/provider"
	"github.com/GovnocoderAI/kern/internal/tools"
)

func newTestPerms() *permission.Manager {
	return permission.New(nil, true, nil)
}

func newAgent(p provider.Provider, reg *tools.Registry, perms *permission.Manager) *agent.Agent {
	return agent.New(p, "scripted", "model", "system", reg, perms, nil)
}

// scriptedProvider drives the agent loop with a fixed sequence of events so
// sub-agent behavior can be tested without a network.
type scriptedProvider struct {
	// replies is consumed one per Stream call.
	replies []provider.Message
	calls   int
}

func (s *scriptedProvider) ID() string   { return "scripted" }
func (s *scriptedProvider) Kind() string { return "openai" }
func (s *scriptedProvider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	return nil, nil
}

func (s *scriptedProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	ch := make(chan provider.Event, 8)
	go func() {
		defer close(ch)
		var msg provider.Message
		if s.calls < len(s.replies) {
			msg = s.replies[s.calls]
		}
		s.calls++
		for _, p := range msg.Parts {
			switch p.Type {
			case provider.PartText:
				ch <- provider.Event{Type: provider.EventText, Text: p.Text}
			case provider.PartToolCall:
				ch <- provider.Event{Type: provider.EventToolCall, ToolCall: p.ToolCall}
			}
		}
		ch <- provider.Event{Type: provider.EventUsage, Usage: &provider.Usage{TotalTokens: 10}}
		ch <- provider.Event{Type: provider.EventDone}
	}()
	return ch, nil
}

func newTestApp(t *testing.T, prov provider.Provider) *App {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Workdir:   dir,
		Model:     "scripted/model",
		Providers: map[string]*config.Provider{},
		Agents:    map[string]*config.Agent{},
	}
	_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello\n"), 0o644)

	registry := tools.NewRegistry(dir, "")
	perms := newTestPerms()
	ag := newAgent(prov, registry, perms)
	app := &App{
		Config:     cfg,
		Agent:      ag,
		Workdir:    dir,
		ProviderID: "scripted",
		ModelID:    "model",
	}
	registry.Env().Subagent = app.runSubagent
	return app
}

// TestRunSubagentReturnsCondensedOutput verifies the runner builds an isolated
// agent, runs the prompt, and returns only the final text.
func TestRunSubagentReturnsCondensedOutput(t *testing.T) {
	prov := &scriptedProvider{replies: []provider.Message{
		{Role: provider.RoleAssistant, Parts: []provider.Part{{Type: provider.PartText, Text: "the answer is 42"}}},
	}}
	a := newTestApp(t, prov)

	res, err := a.runSubagent(context.Background(), tools.SubagentRequest{
		Role:   "plan",
		Prompt: "what is the answer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "the answer is 42") {
		t.Fatalf("output = %q", res.Output)
	}
	if res.Role != "plan" {
		t.Fatalf("role = %q", res.Role)
	}
	if res.Usage.TotalTokens == 0 {
		t.Fatal("usage not accumulated")
	}
}

// TestRunSubagentReadOnlyBlocksWrites verifies a read-only role cannot write,
// even though sub-agents auto-approve asks.
func TestRunSubagentReadOnlyBlocksWrites(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Workdir: dir, Providers: map[string]*config.Provider{}, Agents: map[string]*config.Agent{}}
	registry := tools.NewRegistry(dir, "")

	// The review role tries to write, which must be denied by policy.
	prov := &scriptedProvider{replies: []provider.Message{
		{Role: provider.RoleAssistant, Parts: []provider.Part{{Type: provider.PartToolCall, ToolCall: &provider.ToolCall{
			ID: "c1", Name: "write", Arguments: json.RawMessage(`{"path":"x.txt","content":"hi"}`),
		}}}},
		{Role: provider.RoleAssistant, Parts: []provider.Part{{Type: provider.PartText, Text: "done"}}},
	}}
	a := &App{Config: cfg, ProviderID: "scripted", ModelID: "model", Workdir: dir}
	a.Agent = newAgent(prov, registry, newTestPerms())
	registry.Env().Subagent = a.runSubagent

	if _, err := a.runSubagent(context.Background(), tools.SubagentRequest{Role: "review", Prompt: "write x"}); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "x.txt")); statErr == nil {
		t.Fatal("read-only role wrote a file")
	}
}

// TestRunSubagentUnknownRole verifies a clear error for a bad role name.
func TestRunSubagentUnknownRole(t *testing.T) {
	a := newTestApp(t, &scriptedProvider{})
	_, err := a.runSubagent(context.Background(), tools.SubagentRequest{Role: "wizard", Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "unknown role") {
		t.Fatalf("expected unknown-role error, got %v", err)
	}
}
