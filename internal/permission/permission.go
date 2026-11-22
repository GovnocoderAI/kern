// Package permission implements allow/ask/deny rules for tool execution.
package permission

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/GovnocoderAI/kern/internal/config"
)

type Effect string

const (
	Allow Effect = "allow"
	Ask   Effect = "ask"
	Deny  Effect = "deny"
)

// PromptFunc asks the user to approve an action. Returning true allows it.
type PromptFunc func(action, resource string) (bool, error)

type Manager struct {
	Rules  []Rule
	Auto   bool
	Prompt PromptFunc
}

type Rule struct {
	Action   string
	Resource string
	Effect   Effect
}

func New(rules []config.PermissionRule, auto bool, prompt PromptFunc) *Manager {
	m := &Manager{Auto: auto, Prompt: prompt}
	for _, r := range rules {
		effect := Effect(strings.ToLower(strings.TrimSpace(r.Effect)))
		switch effect {
		case Allow, Ask, Deny:
		default:
			continue
		}
		m.Rules = append(m.Rules, Rule{
			Action:   strings.ToLower(strings.TrimSpace(r.Action)),
			Resource: strings.TrimSpace(r.Resource),
			Effect:   effect,
		})
	}
	return m
}

// Check resolves the effective permission for an action/resource pair.
func (m *Manager) Check(action, resource string) Effect {
	for i := len(m.Rules) - 1; i >= 0; i-- {
		r := m.Rules[i]
		if r.Action != "*" && r.Action != action {
			continue
		}
		if r.Resource == "" || r.Resource == "*" || r.Resource == resource {
			return r.Effect
		}
		if action == "shell" {
			if matchCommand(r.Resource, resource) {
				return r.Effect
			}
			continue
		}
		if ok, _ := doublestar.Match(r.Resource, resource); ok {
			return r.Effect
		}
	}
	switch action {
	case "read":
		return Allow
	default:
		return Ask
	}
}

// Allow appends an allow rule at runtime, used by the "always allow" action.
func (m *Manager) Allow(action, resource string) {
	m.Rules = append(m.Rules, Rule{Action: action, Resource: resource, Effect: Allow})
}

// matchCommand matches shell patterns where "*" spans spaces and slashes.
func matchCommand(pattern, command string) bool {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(command)
}

// Request checks a permission and prompts when required.
func (m *Manager) Request(action, resource string) (bool, error) {
	effect := m.Check(action, resource)
	if m.Auto && effect != Deny {
		return true, nil
	}
	switch effect {
	case Allow:
		return true, nil
	case Deny:
		return false, nil
	}
	if m.Prompt == nil {
		return false, fmt.Errorf("%s %q requires approval; re-run with --auto or in an interactive terminal", action, resource)
	}
	return m.Prompt(action, resource)
}
