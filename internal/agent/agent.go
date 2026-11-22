// Package agent implements the tool-using conversation loop.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/GovnocoderAI/kern/internal/permission"
	"github.com/GovnocoderAI/kern/internal/provider"
	"github.com/GovnocoderAI/kern/internal/tools"
	"github.com/GovnocoderAI/kern/internal/ui"
	"github.com/GovnocoderAI/kern/internal/util"
)

type Agent struct {
	Provider   provider.Provider
	ProviderID string
	Model      string
	System     string
	Tools      *tools.Registry
	Perms      *permission.Manager
	UI         *ui.Console
	MaxSteps   int
	History    []provider.Message
	Usage      provider.Usage
}

func New(p provider.Provider, providerID, model, system string, registry *tools.Registry, perms *permission.Manager, console *ui.Console) *Agent {
	return &Agent{
		Provider:   p,
		ProviderID: providerID,
		Model:      model,
		System:     system,
		Tools:      registry,
		Perms:      perms,
		UI:         console,
		MaxSteps:   40,
	}
}

func (a *Agent) Reset() {
	a.History = nil
	a.Usage = provider.Usage{}
}

// Run executes one user turn, including any tool calls, until the model stops.
func (a *Agent) Run(ctx context.Context, userText string) error {
	a.History = append(a.History, provider.Text(provider.RoleUser, userText))

	for step := 0; step < a.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		req := provider.Request{
			Model:    a.Model,
			System:   a.System,
			Messages: a.History,
			Tools:    a.Tools.Defs(),
		}
		events, err := a.Provider.Stream(ctx, req)
		if err != nil {
			return err
		}

		var text strings.Builder
		var calls []provider.ToolCall
		var streamErr error
		for ev := range events {
			switch ev.Type {
			case provider.EventText:
				text.WriteString(ev.Text)
				a.UI.AssistantDelta(ev.Text)
			case provider.EventReasoning:
				// Reasoning is intentionally hidden in the console.
			case provider.EventToolCall:
				if ev.ToolCall != nil {
					calls = append(calls, *ev.ToolCall)
				}
			case provider.EventUsage:
				if ev.Usage != nil {
					a.Usage.InputTokens += ev.Usage.InputTokens
					a.Usage.OutputTokens += ev.Usage.OutputTokens
					a.Usage.TotalTokens += ev.Usage.TotalTokens
				}
			case provider.EventError:
				streamErr = ev.Err
			}
		}
		if text.Len() > 0 {
			a.UI.AssistantEnd()
		}
		if streamErr != nil {
			return streamErr
		}

		assistant := provider.Message{Role: provider.RoleAssistant}
		if text.Len() > 0 {
			assistant.Parts = append(assistant.Parts, provider.Part{Type: provider.PartText, Text: text.String()})
		}
		for i := range calls {
			assistant.Parts = append(assistant.Parts, provider.Part{Type: provider.PartToolCall, ToolCall: &calls[i]})
		}
		if len(assistant.Parts) > 0 {
			a.History = append(a.History, assistant)
		}

		if len(calls) == 0 {
			return nil
		}

		for i := range calls {
			call := calls[i]
			summary := summarizeCall(call)
			a.UI.ToolStart(call.Name, summary)

			result, isErr := a.execute(ctx, call)
			ok := !isErr
			a.UI.ToolResult(call.Name, ok, summarizeResult(result))

			a.History = append(a.History, provider.Message{
				Role: provider.RoleTool,
				Parts: []provider.Part{{
					Type: provider.PartToolResult,
					ToolResult: &provider.ToolResult{
						CallID:  call.ID,
						Name:    call.Name,
						Content: result,
						IsError: isErr,
					},
				}},
			})
		}
	}
	return fmt.Errorf("stopped after %d steps without a final answer", a.MaxSteps)
}

func (a *Agent) execute(ctx context.Context, call provider.ToolCall) (string, bool) {
	action := tools.Action(call.Name)
	resource := resourceFor(call)
	allowed, err := a.Perms.Request(action, resource)
	if err != nil {
		return "permission error: " + err.Error(), true
	}
	if !allowed {
		return "permission denied by user", true
	}
	result, err := a.Tools.Run(ctx, call.Name, call.Arguments)
	if err != nil {
		return "error: " + err.Error(), true
	}
	return result, false
}

func resourceFor(call provider.ToolCall) string {
	var args map[string]any
	_ = json.Unmarshal(call.Arguments, &args)
	for _, key := range []string{"command", "path", "url", "pattern"} {
		if v, ok := args[key].(string); ok && v != "" {
			return v
		}
	}
	return call.Name
}

func summarizeCall(call provider.ToolCall) string {
	var args map[string]any
	_ = json.Unmarshal(call.Arguments, &args)
	for _, key := range []string{"path", "command", "url", "pattern"} {
		if v, ok := args[key].(string); ok && v != "" {
			return shorten(strings.ReplaceAll(v, "\n", " "), 90)
		}
	}
	return ""
}

func summarizeResult(result string) string {
	result = strings.TrimSpace(result)
	if i := strings.IndexByte(result, '\n'); i >= 0 {
		result = result[:i]
	}
	if result == "" {
		result = "(done)"
	}
	return shorten(result, 90)
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// BuildSystemPrompt assembles the base prompt, environment and AGENTS.md files.
func BuildSystemPrompt(workdir string, extraInstructions []string) string {
	var b strings.Builder
	b.WriteString("You are Kern, a fast and precise terminal coding agent.\n")
	b.WriteString("You help with software engineering tasks by reading, editing and running code in the user's workspace.\n\n")
	b.WriteString("Guidelines:\n")
	b.WriteString("- Be concise. Prefer action over explanation.\n")
	b.WriteString("- Inspect the repository before changing it. Follow existing conventions.\n")
	b.WriteString("- Use the provided tools to read, edit, search and run commands.\n")
	b.WriteString("- Never invent file contents; always read first.\n")
	b.WriteString("- Keep changes minimal and focused. Do not add comments unless asked.\n")
	b.WriteString("- When a task is complete, summarize what changed and how it was verified.\n\n")
	b.WriteString(fmt.Sprintf("Environment:\n- OS: %s/%s\n- Workspace: %s\n- Shell: %s\n- Date: %s\n\n",
		runtime.GOOS, runtime.GOARCH, workdir, shellName(), time.Now().Format("2006-01-02")))

	if agents := loadAgentsFiles(workdir); agents != "" {
		b.WriteString("Project instructions (AGENTS.md):\n")
		b.WriteString(agents)
		b.WriteString("\n")
	}
	for _, inst := range extraInstructions {
		path := inst
		if !filepath.IsAbs(path) {
			path = filepath.Join(workdir, path)
		}
		if data, err := os.ReadFile(path); err == nil {
			b.WriteString(fmt.Sprintf("\nInstructions from %s:\n%s\n", inst, strings.TrimSpace(string(data))))
		}
	}
	return b.String()
}

func loadAgentsFiles(workdir string) string {
	var paths []string
	global := filepath.Join(util.KernConfigDir(), "AGENTS.md")
	if util.Exists(global) {
		paths = append(paths, global)
	}
	var chain []string
	for d := workdir; ; d = filepath.Dir(d) {
		chain = append(chain, d)
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		p := filepath.Join(chain[i], "AGENTS.md")
		if util.Exists(p) {
			paths = append(paths, p)
		}
	}
	var b strings.Builder
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		b.WriteString(fmt.Sprintf("\n<!-- %s -->\n%s\n", p, strings.TrimSpace(string(data))))
	}
	return b.String()
}

func shellName() string {
	if runtime.GOOS == "windows" {
		return "cmd.exe"
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/sh"
}
