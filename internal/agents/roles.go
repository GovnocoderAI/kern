// Package agents defines the role presets used by Kern's multi-agent mode.
//
// A role is a named bundle of: a system-prompt addendum, the set of tools the
// role may use, whether it is read-only, and how many steps it may take. The
// orchestrator (the top-level agent) delegates to roles through the task tool,
// and each delegated run gets an isolated context and a bounded budget.
package agents

import (
	"fmt"
	"sort"
	"strings"
)

// Role is a presettable sub-agent profile.
type Role struct {
	// Name is the identifier used by --agent and the task tool, e.g. "plan".
	Name string
	// Description is shown by `kern agent` and in the task tool docs.
	Description string
	// SystemPrompt is appended to the base system prompt for this role.
	SystemPrompt string
	// Tools lists the tool names this role may call. Nil means every tool.
	Tools []string
	// ReadOnly forbids mutating tools (write, edit) and shell commands that
	// could write, regardless of the permission policy.
	ReadOnly bool
	// MaxSteps caps the role's tool-calling loop.
	MaxSteps int
	// MaxTokens caps total tokens for the role's run. Zero means no explicit
	// cap beyond the provider's own limits.
	MaxTokens int
}

// Defaults are the built-in roles. They are intentionally small in number and
// sharp in purpose — a handful of clear roles beats a wall of presets.
var Defaults = []Role{
	{
		Name:        "plan",
		Description: "Read-only planner: inspects the repo and proposes a step-by-step plan.",
		SystemPrompt: "You are the planning role. You cannot modify files or run commands.\n" +
			"Inspect the repository with read-only tools, then produce a concrete, ordered plan\n" +
			"with the files to touch and the checks to run. Do not write code.",
		Tools:     []string{"read", "grep", "glob", "ls", "webfetch"},
		ReadOnly:  true,
		MaxSteps:  15,
		MaxTokens: 40000,
	},
	{
		Name:        "build",
		Description: "Implementer: makes the smallest correct change and verifies it builds.",
		SystemPrompt: "You are the build role. You implement changes directly.\n" +
			"Read before editing, keep diffs minimal, and verify the result compiles or passes\n" +
			"the project's checks. Report what you changed and how you verified it.",
		Tools:     nil, // all tools
		ReadOnly:  false,
		MaxSteps:  40,
		MaxTokens: 80000,
	},
	{
		Name:        "test",
		Description: "Verifier: runs the test suite and reports failures, without editing source.",
		SystemPrompt: "You are the test role. Run the project's tests and linters and report\n" +
			"results precisely: what passed, what failed, and the exact error output. Do not modify\n" +
			"source files; you may run commands.",
		Tools:     []string{"read", "grep", "glob", "ls", "bash", "process"},
		ReadOnly:  false,
		MaxSteps:  20,
		MaxTokens: 40000,
	},
	{
		Name:        "review",
		Description: "Reviewer: critiques a diff for bugs, edge cases and style, read-only.",
		SystemPrompt: "You are the review role. Review code and diffs for correctness, edge cases,\n" +
			"security and style. Point out concrete problems with file:line references. Be terse and\n" +
			"specific. You cannot modify files.",
		Tools:     []string{"read", "grep", "glob", "ls", "bash"},
		ReadOnly:  true,
		MaxSteps:  15,
		MaxTokens: 30000,
	},
}

// Get returns the built-in role by name.
func Get(name string) (Role, bool) {
	for _, r := range Defaults {
		if r.Name == name {
			return r, true
		}
	}
	return Role{}, false
}

// MustGet returns the role or panics; intended for known names only.
func MustGet(name string) Role {
	r, ok := Get(name)
	if !ok {
		panic(fmt.Sprintf("agents: unknown role %q", name))
	}
	return r
}

// Names returns the built-in role names, sorted.
func Names() []string {
	out := make([]string, 0, len(Defaults))
	for _, r := range Defaults {
		out = append(out, r.Name)
	}
	sort.Strings(out)
	return out
}

// AllowsTool reports whether the role may use the named tool.
func (r Role) AllowsTool(name string) bool {
	if r.Tools == nil {
		return true
	}
	for _, t := range r.Tools {
		if t == name {
			return true
		}
	}
	return false
}

// Describe renders a one-line summary for `kern agent`.
func (r Role) Describe() string {
	access := "read-write"
	if r.ReadOnly {
		access = "read-only"
	}
	tools := "all"
	if r.Tools != nil {
		tools = strings.Join(r.Tools, ",")
	}
	return fmt.Sprintf("%-8s %-11s steps=%-2d tools=%s  %s",
		r.Name, access, r.MaxSteps, tools, r.Description)
}
