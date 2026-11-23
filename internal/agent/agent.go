// Package agent implements the tool-using conversation loop.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/GovnocoderAI/kern/internal/kerr"
	"github.com/GovnocoderAI/kern/internal/permission"
	"github.com/GovnocoderAI/kern/internal/provider"
	"github.com/GovnocoderAI/kern/internal/tools"
	"github.com/GovnocoderAI/kern/internal/ui"
	"github.com/GovnocoderAI/kern/internal/util"
)

// Observer receives structured agent events. When nil, Kern falls back to the
// plain console renderer.
type Observer interface {
	OnText(delta string)
	OnToolStart(name, summary string)
	OnToolResult(name string, ok bool, output string)
	OnUsage(usage provider.Usage)
	OnStepDone()
}

type Agent struct {
	Provider   provider.Provider
	ProviderID string
	Model      string
	System     string
	Tools      *tools.Registry
	Perms      *permission.Manager
	UI         *ui.Console
	Observer   Observer
	MaxSteps   int
	// TokenBudget, when > 0, stops the loop once total usage exceeds it.
	// Used to bound sub-agent runs.
	TokenBudget int
	History     []provider.Message
	Usage       provider.Usage
}

// RunInput is the input to one agent turn.
type RunInput struct {
	// Text is the user's message.
	Text string
	// Skill, when set, is the name of an active skill whose prompt is
	// appended to the system prompt for this turn only.
	Skill string
	// SkillPrompt is the skill's prompt body.
	SkillPrompt string
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
func (a *Agent) Run(ctx context.Context, in RunInput) error {
	a.History = append(a.History, provider.Text(provider.RoleUser, in.Text))
	system := a.System
	if in.Skill != "" && in.SkillPrompt != "" {
		system += "\n\n" + in.SkillPrompt
	}

	for step := 0; step < a.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return kerr.Cancelled()
		}
		if a.TokenBudget > 0 && a.Usage.TotalTokens >= a.TokenBudget {
			return kerr.User("token budget of %d exhausted after %d tokens", a.TokenBudget, a.Usage.TotalTokens)
		}
		req := provider.Request{
			Model:    a.Model,
			System:   system,
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
				a.emitText(ev.Text)
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
					a.emitUsage(*ev.Usage)
				}
			case provider.EventError:
				streamErr = ev.Err
			}
		}
		if ctx.Err() != nil {
			// Interruption mid-stream: record the partial text as context so
			// the next turn does not repeat it, then report the cancellation.
			if text.Len() > 0 {
				a.History = append(a.History, provider.Message{
					Role:  provider.RoleAssistant,
					Parts: []provider.Part{{Type: provider.PartText, Text: text.String()}},
				})
			}
			a.repairHistory()
			return kerr.Cancelled()
		}
		if text.Len() > 0 {
			a.emitStepDone()
		}
		if streamErr != nil {
			return kerr.Wrap(classify(streamErr), streamErr, "provider stream failed")
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
			a.emitToolStart(call.Name, summary)

			result, isErr := a.execute(ctx, call)
			if ctx.Err() != nil {
				a.History = append(a.History, toolResultMsg(call.ID, call.Name, "interrupted by user", true))
				a.repairHistory()
				return kerr.Cancelled()
			}
			ok := !isErr
			a.emitToolResult(call.Name, ok, summarizeResult(result))

			a.History = append(a.History, toolResultMsg(call.ID, call.Name, result, isErr))
		}
	}
	return kerr.User("stopped after %d steps without a final answer", a.MaxSteps)
}

// repairHistory guarantees that every assistant tool_call is followed by a
// matching tool result. Anthropic rejects requests that violate this
// invariant, and a cancelled turn must not leave the history broken.
func (a *Agent) repairHistory() {
	// Collect result call IDs that are already present.
	have := map[string]bool{}
	for i := len(a.History) - 1; i >= 0; i-- {
		m := a.History[i]
		if m.Role == provider.RoleTool {
			for _, p := range m.Parts {
				if p.Type == provider.PartToolResult && p.ToolResult != nil {
					have[p.ToolResult.CallID] = true
				}
			}
		}
	}
	// Patch in missing results, in reverse order of the assistant messages.
	var patch []provider.Message
	for i := len(a.History) - 1; i >= 0; i-- {
		m := a.History[i]
		if m.Role != provider.RoleAssistant {
			continue
		}
		for _, p := range m.Parts {
			if p.Type != provider.PartToolCall || p.ToolCall == nil {
				continue
			}
			id := p.ToolCall.ID
			if id == "" {
				id = fmt.Sprintf("kern-synth-%d", i)
			}
			if !have[id] {
				patch = append(patch, toolResultMsg(id, p.ToolCall.Name, "(no result: the turn was interrupted)", true))
				have[id] = true
			}
		}
	}
	if len(patch) > 0 {
		// Tool results must sit immediately after the assistant message that
		// called them; appending to the tail is correct because no later
		// user message has been added yet.
		a.History = append(a.History, patch...)
	}
}

func toolResultMsg(callID, name, content string, isErr bool) provider.Message {
	return provider.Message{
		Role: provider.RoleTool,
		Parts: []provider.Part{{
			Type: provider.PartToolResult,
			ToolResult: &provider.ToolResult{
				CallID:  callID,
				Name:    name,
				Content: content,
				IsError: isErr,
			},
		}},
	}
}

// classify maps a provider stream error to a kerr code.
func classify(err error) kerr.Code {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return kerr.CodeCancelled
	}
	if status, ok := provider.IsHTTPError(err); ok {
		if status >= 400 && status < 500 {
			return kerr.CodeProvider
		}
		return kerr.CodeProvider
	}
	return kerr.CodeProvider
}

func (a *Agent) emitText(delta string) {
	if a.Observer != nil {
		a.Observer.OnText(delta)
		return
	}
	if a.UI != nil {
		a.UI.AssistantDelta(delta)
	}
}

func (a *Agent) emitStepDone() {
	if a.Observer != nil {
		a.Observer.OnStepDone()
		return
	}
	if a.UI != nil {
		a.UI.AssistantEnd()
	}
}

func (a *Agent) emitToolStart(name, summary string) {
	if a.Observer != nil {
		a.Observer.OnToolStart(name, summary)
		return
	}
	if a.UI != nil {
		a.UI.ToolStart(name, summary)
	}
}

func (a *Agent) emitToolResult(name string, ok bool, output string) {
	if a.Observer != nil {
		a.Observer.OnToolResult(name, ok, output)
		return
	}
	if a.UI != nil {
		a.UI.ToolResult(name, ok, output)
	}
}

func (a *Agent) emitUsage(usage provider.Usage) {
	if a.Observer != nil {
		a.Observer.OnUsage(usage)
	}
}

// FinalText returns the text of the most recent assistant message, used to
// return a sub-agent's answer to its caller.
func (a *Agent) FinalText() string {
	for i := len(a.History) - 1; i >= 0; i-- {
		if a.History[i].Role == provider.RoleAssistant {
			return a.History[i].Text()
		}
	}
	return ""
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
	s = s[:n-1]
	for len(s) > 0 && (s[len(s)-1]&0xC0) == 0x80 {
		s = s[:len(s)-1]
	}
	return s + "…"
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
	b.WriteString("- When a task is complete, summarize what changed and how it was verified.\n")
	b.WriteString("- For non-trivial work, delegate with the task tool: \"plan\" to design, \"build\" to\n")
	b.WriteString("  implement, \"test\" to run checks, \"review\" to critique. Each sub-agent has an\n")
	b.WriteString("  isolated context, so give it everything it needs in the prompt.\n\n")
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
