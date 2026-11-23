package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/GovnocoderAI/kern/internal/provider"
)

// Budget bounds a sub-agent run so delegation cannot run away with the
// user's tokens or time.
type Budget struct {
	MaxSteps  int
	MaxTokens int
	Timeout   time.Duration
}

// SubagentRequest is one delegation from the orchestrator to a role.
type SubagentRequest struct {
	// Role is the role name: plan, build, test or review.
	Role string
	// Prompt is the task text handed to the sub-agent.
	Prompt string
	// Budget caps the run.
	Budget Budget
}

// SubagentResult is the condensed outcome returned to the orchestrator.
type SubagentResult struct {
	Output string
	Usage  provider.Usage
	Steps  int
	Role   string
}

// SubagentRunner runs an isolated sub-agent for the task tool. The app package
// supplies the implementation; tools only depend on this interface.
type SubagentRunner func(ctx context.Context, req SubagentRequest) (*SubagentResult, error)

// TaskTool lets the orchestrator delegate a self-contained job to a sub-agent
// role. The sub-agent runs with its own context and budget, and only its final
// text comes back, keeping the orchestrator's history small.
type TaskTool struct{}

func (t *TaskTool) Name() string { return "task" }

func (t *TaskTool) Description() string {
	return "Delegate a self-contained job to a sub-agent role with an isolated context.\n" +
		"Roles: plan (read-only planning), build (implement), test (run checks), review (critique).\n" +
		"The sub-agent gets only the prompt you pass and returns a condensed result."
}

func (t *TaskTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
		"role":{"type":"string","enum":["plan","build","test","review"],"description":"sub-agent role"},
		"prompt":{"type":"string","description":"the task, with all context the sub-agent needs"},
		"max_steps":{"type":"integer","description":"override the role's step budget (optional)"}
	},"required":["role","prompt"]}`)
}

func (t *TaskTool) Run(ctx context.Context, raw json.RawMessage, env *Env) (string, error) {
	if env.Subagent == nil {
		return "", fmt.Errorf("sub-agents are not available in this session")
	}
	var args struct {
		Role     string `json:"role"`
		Prompt   string `json:"prompt"`
		MaxSteps int    `json:"max_steps"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	args.Role = strings.ToLower(strings.TrimSpace(args.Role))
	if args.Role == "" {
		return "", fmt.Errorf("role is required")
	}
	if strings.TrimSpace(args.Prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}

	res, err := env.Subagent(ctx, SubagentRequest{
		Role:   args.Role,
		Prompt: args.Prompt,
		Budget: Budget{MaxSteps: args.MaxSteps},
	})
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("[%s] %s\n", res.Role, strings.TrimSpace(res.Output)))
	b.WriteString(fmt.Sprintf("(steps=%d tokens=%d)", res.Steps, res.Usage.TotalTokens))
	return b.String(), nil
}
