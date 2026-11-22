package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ProcessTool exposes live shell process state to the model so it can watch
// long-running commands instead of re-running them.
type ProcessTool struct{}

func (t *ProcessTool) Name() string { return "process" }
func (t *ProcessTool) Description() string {
	return "Inspect running or finished shell processes: list them, read their output, or kill one by id."
}
func (t *ProcessTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["list","output","kill"],"description":"list = all processes; output = last N lines of a process; kill = stop a running process"},"id":{"type":"string","description":"process id, e.g. p3"},"lines":{"type":"integer","description":"for action=output: how many trailing lines to return (default 30, max 200)"}},"required":["action"]}`)
}

func (t *ProcessTool) Run(ctx context.Context, raw json.RawMessage, env *Env) (string, error) {
	var args struct {
		Action string `json:"action"`
		ID     string `json:"id"`
		Lines  int    `json:"lines"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	switch args.Action {
	case "list":
		return t.list(env)
	case "output":
		return t.output(env, args.ID, args.Lines)
	case "kill":
		if args.ID == "" {
			return "", fmt.Errorf("kill requires an id, e.g. \"p3\"")
		}
		if env.KillProcess(args.ID) {
			return "killed " + args.ID, nil
		}
		return "process " + args.ID + " is not running", nil
	default:
		return "", fmt.Errorf("unknown action %q", args.Action)
	}
}

func (t *ProcessTool) list(env *Env) (string, error) {
	procs := env.ProcessesSnapshot()
	if len(procs) == 0 {
		return "(no processes yet)", nil
	}
	var b strings.Builder
	for i := range procs {
		p := &procs[i]
		state := "finished"
		if p.Running {
			state = "running"
		}
		b.WriteString(fmt.Sprintf("%s %s [%s]\n", p.ID, p.Command, state))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (t *ProcessTool) output(env *Env, id string, lines int) (string, error) {
	if id == "" {
		procs := env.ProcessesSnapshot()
		if len(procs) == 0 {
			return "(no processes yet)", nil
		}
		// Default to the last process in the log.
		id = procs[len(procs)-1].ID
	}
	procs := env.ProcessesSnapshot()
	for i := range procs {
		if procs[i].ID != id {
			continue
		}
		out := procs[i].Output
		if lines <= 0 || lines > 200 {
			lines = 30
		}
		outLines := strings.Split(out, "\n")
		if len(outLines) > lines {
			outLines = outLines[len(outLines)-lines:]
		}
		return fmt.Sprintf("%s (last %d lines)\n%s", id, lines, strings.Join(outLines, "\n")), nil
	}
	return "process " + id + " not found", nil
}

// JokeTool returns a short, harmless joke. It is a read-only, permission-free
// tool that demonstrates non-work tools can be registered alongside the
// coding ones.
type JokeTool struct{}

func (t *JokeTool) Name() string { return "joke" }
func (t *JokeTool) Description() string {
	return "Tell a short, inoffensive joke. No arguments needed."
}
func (t *JokeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *JokeTool) Run(ctx context.Context, raw json.RawMessage, env *Env) (string, error) {
	jokes := []string{
		`Go's nil is the only value that is a pointer, an error, and nothing at the same time.`,
		`A C programmer walks into a bar, orders 9 drinks, and says: "That's all, and by the way I'm going to segfault."`,
		`Why do Go programmers make great comedians? Because they know when to return early and when to panic().`,
		`There are 10 kinds of people in this world: those who understand binary and those who don't.`,
		`A SQL query walks into a bar, sees two tables, and asks: "Can I JOIN you?"`,
		`I told the compiler a joke. It said: "That's not a valid statement."`,
		`An infinite loop walks into a bar. The bartender says: "Usual?" The loop says: "Yes, please."`,
		`Why was the function late? It kept getting called before it was ready.`,
		`I'd tell you a UDP joke, but you might not get it.`,
		`Two bits walk into a bar. The bartender asks: "What'll you have?" The bits say: "A double, please."`,
	}
	var args struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(raw, &args)
	if args.Count > 0 && args.Count < len(jokes) {
		return jokes[args.Count], nil
	}
	// Default: cycle through jokes based on the call count (not available
	// here, so just pick the first one).
	return jokes[0], nil
}
