// Package app builds a ready-to-run Kern session from configuration, shared by
// the console and TUI frontends.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GovnocoderAI/kern/internal/agent"
	"github.com/GovnocoderAI/kern/internal/agents"
	"github.com/GovnocoderAI/kern/internal/config"
	"github.com/GovnocoderAI/kern/internal/permission"
	"github.com/GovnocoderAI/kern/internal/provider"
	"github.com/GovnocoderAI/kern/internal/session"
	"github.com/GovnocoderAI/kern/internal/tools"
	"github.com/GovnocoderAI/kern/internal/ui"
)

type Options struct {
	Workdir     string
	Model       string
	Agent       string
	SessionID   string
	Continue    bool
	Fork        bool
	Auto        bool
	Interactive bool
	Prompt      permission.PromptFunc
	Observer    agent.Observer
	Console     *ui.Console
}

type App struct {
	Config     *config.Config
	Auth       *config.Auth
	Agent      *agent.Agent
	Session    *session.Session
	Store      *session.Store
	Console    *ui.Console
	ProviderID string
	ModelID    string
	Workdir    string
}

func New(opts Options) (*App, error) {
	abs, err := filepath.Abs(opts.Workdir)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(abs)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	auth := config.LoadAuth()
	store := session.NewStore()

	var sess *session.Session
	switch {
	case opts.SessionID != "":
		sess, err = store.Load(opts.SessionID)
		if err != nil {
			return nil, fmt.Errorf("session %s: %w", opts.SessionID, err)
		}
	case opts.Continue:
		sess, err = store.Latest(abs)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			sess = session.New(abs, "")
		}
	default:
		sess = session.New(abs, "")
	}
	if opts.Fork && opts.SessionID != "" {
		forked := session.New(abs, "")
		forked.Messages = append([]provider.Message(nil), sess.Messages...)
		forked.Title = sess.Title + " (fork)"
		sess = forked
	}

	modelRef := opts.Model
	if modelRef == "" && sess.Model != "" && (opts.Continue || opts.SessionID != "") {
		modelRef = sess.Model
	}
	pid, mid, err := cfg.ResolveModel(modelRef)
	if err != nil {
		return nil, err
	}
	prov, err := provider.FromConfig(pid, cfg.Providers[pid], cfg.APIKey(pid, auth))
	if err != nil {
		return nil, err
	}

	console := opts.Console
	if console == nil {
		console = ui.New()
	}
	prompt := opts.Prompt
	if prompt == nil && opts.Interactive {
		prompt = func(action, resource string) (bool, error) {
			return console.Ask(fmt.Sprintf("permission: %s %s", action, resource))
		}
	}
	perms := permission.New(cfg.Permissions, opts.Auto, prompt)
	registry := tools.NewRegistry(abs, cfg.Shell)
	system := agent.BuildSystemPrompt(abs, cfg.Instructions)

	// If a role was selected, run as that role instead of the orchestrator:
	// apply its system prompt, tool subset and read-only constraint.
	if opts.Agent != "" {
		role, ok := agents.Get(opts.Agent)
		if !ok {
			return nil, fmt.Errorf("unknown agent role %q (known: %s)", opts.Agent, strings.Join(agents.Names(), ", "))
		}
		system += "\n\n" + role.SystemPrompt
		var names []string
		for _, n := range registry.Names() {
			if n != "task" && role.AllowsTool(n) {
				names = append(names, n)
			}
		}
		registry = registry.Subset(names)
		if role.ReadOnly {
			rules := append([]config.PermissionRule(nil), cfg.Permissions...)
			rules = append(rules, config.PermissionRule{Action: "edit", Resource: "*", Effect: "deny"})
			perms = permission.New(rules, opts.Auto, prompt)
		}
	}

	ag := agent.New(prov, pid, mid, system, registry, perms, console)
	ag.Observer = opts.Observer
	ag.History = sess.Messages
	ag.Usage = sess.Usage

	sess.Model = pid + "/" + mid
	sess.Workdir = abs

	app := &App{
		Config:     cfg,
		Auth:       auth,
		Agent:      ag,
		Session:    sess,
		Store:      store,
		Console:    console,
		ProviderID: pid,
		ModelID:    mid,
		Workdir:    abs,
	}
	// Enable delegation: the orchestrator's task tool calls back into runSubagent.
	registry.Env().Subagent = app.runSubagent
	return app, nil
}

func (a *App) Sync() {
	a.Session.Messages = a.Agent.History
	a.Session.Usage = a.Agent.Usage
	a.Session.UpdatedAt = time.Now()
}

func (a *App) Save() error {
	a.Sync()
	return a.Store.Save(a.Session)
}

func (a *App) RebuildModel(ref string) error {
	pid, mid, err := a.Config.ResolveModel(ref)
	if err != nil {
		return err
	}
	prov, err := provider.FromConfig(pid, a.Config.Providers[pid], a.Config.APIKey(pid, a.Auth))
	if err != nil {
		return err
	}
	a.Agent.Provider = prov
	a.Agent.ProviderID = pid
	a.Agent.Model = mid
	a.ProviderID = pid
	a.ModelID = mid
	a.Session.Model = pid + "/" + mid
	return nil
}

// Tools returns the session's tool registry, which also holds file changes and
// process history for the UI.
func (a *App) Tools() *tools.Registry {
	return a.Agent.Tools
}

// runSubagent runs an isolated role with a bounded budget and returns its
// condensed answer. It is wired into the task tool so the orchestrator can
// delegate without carrying the sub-agent's whole transcript.
func (a *App) runSubagent(ctx context.Context, req tools.SubagentRequest) (*tools.SubagentResult, error) {
	role, ok := agents.Get(req.Role)
	if !ok {
		return nil, fmt.Errorf("unknown role %q (known: %s)", req.Role, strings.Join(agents.Names(), ", "))
	}

	maxSteps := role.MaxSteps
	if req.Budget.MaxSteps > 0 {
		maxSteps = req.Budget.MaxSteps
	}
	maxTokens := req.Budget.MaxTokens
	if maxTokens <= 0 {
		maxTokens = role.MaxTokens
	}

	// Tools: a subset for the role, always excluding task itself so a
	// sub-agent cannot recurse without bound.
	var names []string
	for _, n := range a.Agent.Tools.Names() {
		if n == "task" {
			continue
		}
		if role.AllowsTool(n) {
			names = append(names, n)
		}
	}
	reg := a.Agent.Tools.Subset(names)

	// Permissions: a read-only role gets an explicit deny for edits. Sub-agents
	// run non-interactively, so asks auto-approve; explicit deny still wins.
	rules := append([]config.PermissionRule(nil), a.Config.Permissions...)
	if role.ReadOnly {
		rules = append(rules, config.PermissionRule{Action: "edit", Resource: "*", Effect: "deny"})
	}
	perms := permission.New(rules, true, nil)

	system := agent.BuildSystemPrompt(a.Workdir, a.Config.Instructions)
	system += "\n\n" + role.SystemPrompt

	sub := agent.New(a.Agent.Provider, a.ProviderID, a.ModelID, system, reg, perms, nil)
	sub.MaxSteps = maxSteps
	sub.TokenBudget = maxTokens

	runCtx := ctx
	timeout := req.Budget.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	runErr := sub.Run(runCtx, agent.RunInput{Text: req.Prompt})

	res := &tools.SubagentResult{
		Output: sub.FinalText(),
		Usage:  sub.Usage,
		Role:   role.Name,
	}
	for _, m := range sub.History {
		if m.Role == provider.RoleAssistant {
			res.Steps++
		}
	}
	if strings.TrimSpace(res.Output) == "" {
		res.Output = "(no output)"
	}
	if runErr != nil {
		return res, fmt.Errorf("%s role stopped: %w", role.Name, runErr)
	}
	return res, nil
}
