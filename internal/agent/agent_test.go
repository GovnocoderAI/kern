package agent

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kern-ai/kern/internal/config"
	"github.com/kern-ai/kern/internal/permission"
	"github.com/kern-ai/kern/internal/provider"
	"github.com/kern-ai/kern/internal/tools"
	"github.com/kern-ai/kern/internal/ui"
)

type scripted struct {
	mu    sync.Mutex
	calls int
	tool  provider.ToolCall
}

func (s *scripted) ID() string   { return "scripted" }
func (s *scripted) Kind() string { return "openai" }
func (s *scripted) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	return nil, nil
}

func (s *scripted) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()

	ch := make(chan provider.Event, 4)
	go func() {
		defer close(ch)
		if n == 1 {
			call := s.tool
			ch <- provider.Event{Type: provider.EventToolCall, ToolCall: &call}
		} else {
			ch <- provider.Event{Type: provider.EventText, Text: "done"}
			ch <- provider.Event{Type: provider.EventUsage, Usage: &provider.Usage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3}}
		}
		ch <- provider.Event{Type: provider.EventDone}
	}()
	return ch, nil
}

func newConsole() *ui.Console {
	return &ui.Console{Out: io.Discard, ErrOut: io.Discard}
}

func TestToolLoop(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	prov := &scripted{tool: provider.ToolCall{ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}}
	ag := New(prov, "scripted", "m", "sys", tools.NewRegistry(dir, ""), permission.New(nil, true, nil), newConsole())

	if err := ag.Run(context.Background(), "read it"); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 2 {
		t.Fatalf("provider calls = %d", prov.calls)
	}
	if len(ag.History) != 4 {
		t.Fatalf("history = %d", len(ag.History))
	}
	toolMsg := ag.History[2]
	if toolMsg.Role != provider.RoleTool || toolMsg.Parts[0].ToolResult.Content != "hello\n" {
		t.Fatalf("tool result = %+v", toolMsg.Parts[0])
	}
	last := ag.History[3]
	if last.Text() != "done" {
		t.Fatalf("final text = %q", last.Text())
	}
	if ag.Usage.TotalTokens != 3 {
		t.Fatalf("usage = %+v", ag.Usage)
	}
}

func TestPermissionDenied(t *testing.T) {
	dir := t.TempDir()
	prov := &scripted{tool: provider.ToolCall{ID: "c1", Name: "write", Arguments: json.RawMessage(`{"path":"x.txt","content":"x"}`)}}
	perms := permission.New([]config.PermissionRule{{Action: "edit", Resource: "*", Effect: "deny"}}, false, nil)
	ag := New(prov, "scripted", "m", "sys", tools.NewRegistry(dir, ""), perms, newConsole())

	if err := ag.Run(context.Background(), "write it"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ag.History[2].Parts[0].ToolResult.Content, "permission denied") {
		t.Fatalf("result = %q", ag.History[2].Parts[0].ToolResult.Content)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.txt")); err == nil {
		t.Fatal("file should not exist")
	}
}
