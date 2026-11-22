// Package tools implements the agent's file, shell and search tools.
package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/GovnocoderAI/kern/internal/provider"
)

const (
	maxOutputBytes = 50 * 1024
	maxReadLines   = 2000
	maxGlobResults = 500
	maxGrepResults = 200
)

var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true,
	"vendor": true, "dist": true, "build": true, "target": true,
	".venv": true, "venv": true, "__pycache__": true, ".next": true,
	".cache": true, "coverage": true,
}

type Todo struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority,omitempty"`
}

// Change records a file mutation so the UI can show diffs.
type Change struct {
	Path   string    `json:"path"`
	Before string    `json:"before"`
	After  string    `json:"after"`
	At     time.Time `json:"at"`
}

// Process records a shell command executed by the agent.
type Process struct {
	ID       string    `json:"id"`
	Command  string    `json:"command"`
	Cwd      string    `json:"cwd"`
	Started  time.Time `json:"started"`
	Ended    time.Time `json:"ended"`
	ExitCode int       `json:"exit_code"`
	Output   string    `json:"output"`
	Running  bool      `json:"running"`

	cancel context.CancelFunc
}

var processSeq atomic.Uint64

type Env struct {
	Workdir string
	Shell   string

	mu        sync.Mutex
	Todos     []Todo
	Changes   []Change
	Processes []*Process
}

func (e *Env) SetTodos(todos []Todo) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Todos = append([]Todo(nil), todos...)
}

func (e *Env) TodosSnapshot() []Todo {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Todo(nil), e.Todos...)
}

func (e *Env) AddChange(c Change) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Changes = append(e.Changes, c)
}

func (e *Env) ChangesSnapshot() []Change {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Change(nil), e.Changes...)
}

func (e *Env) StartProcess(command, cwd string, cancel context.CancelFunc) *Process {
	p := &Process{
		ID:      fmt.Sprintf("p%d", processSeq.Add(1)),
		Command: command,
		Cwd:     cwd,
		Started: time.Now(),
		Running: true,
		cancel:  cancel,
	}
	e.mu.Lock()
	e.Processes = append(e.Processes, p)
	e.mu.Unlock()
	return p
}

func (e *Env) EndProcess(p *Process, exitCode int, output string) {
	if p == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	p.Ended = time.Now()
	p.ExitCode = exitCode
	p.Output = output
	p.Running = false
	p.cancel = nil
}

func (e *Env) ProcessesSnapshot() []Process {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Process, 0, len(e.Processes))
	for _, p := range e.Processes {
		out = append(out, *p)
	}
	return out
}

func (e *Env) KillProcess(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, p := range e.Processes {
		if p.ID == id && p.Running && p.cancel != nil {
			p.cancel()
			return true
		}
	}
	return false
}

type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	Run(ctx context.Context, args json.RawMessage, env *Env) (string, error)
}

type Registry struct {
	tools map[string]Tool
	order []string
	env   *Env
}

func NewRegistry(workdir, shell string) *Registry {
	r := &Registry{tools: map[string]Tool{}, env: &Env{Workdir: workdir, Shell: shell}}
	for _, t := range []Tool{
		&ReadTool{}, &WriteTool{}, &EditTool{}, &BashTool{},
		&GrepTool{}, &GlobTool{}, &LsTool{}, &TodoTool{}, &WebFetchTool{},
	} {
		r.tools[t.Name()] = t
		r.order = append(r.order, t.Name())
	}
	return r
}

func (r *Registry) Env() *Env { return r.env }

func (r *Registry) Names() []string { return append([]string(nil), r.order...) }

func (r *Registry) Defs() []provider.ToolDef {
	defs := make([]provider.ToolDef, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]
		defs = append(defs, provider.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return defs
}

func (r *Registry) Run(ctx context.Context, name string, args json.RawMessage) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	return t.Run(ctx, args, r.env)
}

// Action maps a tool name to its permission action.
func Action(name string) string {
	switch name {
	case "write", "edit":
		return "edit"
	case "bash":
		return "shell"
	case "webfetch":
		return "webfetch"
	default:
		return "read"
	}
}

func resolve(workdir, p string) (string, error) {
	if p == "" {
		p = "."
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(workdir, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(workdir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q is outside the workspace", p)
	}
	return abs, nil
}

func truncate(s string, limit int) (string, bool) {
	// Never split a UTF-8 sequence: if the cut lands inside a character,
	// roll back to the last complete character boundary.
	if len(s) <= limit {
		return s, false
	}
	cut := limit
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	s = s[:cut]
	return s, true
}

// ---- read ----

type ReadTool struct{}

func (t *ReadTool) Name() string { return "read" }
func (t *ReadTool) Description() string {
	return "Read a text file from the workspace. Returns raw contents with an optional line range."
}
func (t *ReadTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path relative to the workspace"},"offset":{"type":"integer","description":"1-based starting line"},"limit":{"type":"integer","description":"Maximum number of lines"}},"required":["path"]}`)
}

func (t *ReadTool) Run(ctx context.Context, raw json.RawMessage, env *Env) (string, error) {
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	path, err := resolve(env.Workdir, args.Path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory; use ls", args.Path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	head := make([]byte, 8192)
	n, _ := f.Read(head)
	if bytes.IndexByte(head[:n], 0) >= 0 {
		return fmt.Sprintf("binary file (%d bytes) at %s", info.Size(), args.Path), nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}

	limit := args.Limit
	if limit <= 0 || limit > maxReadLines {
		limit = maxReadLines
	}
	offset := args.Offset
	if offset < 1 {
		offset = 1
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out strings.Builder
	line := 0
	collected := 0
	truncated := false
	for scanner.Scan() {
		line++
		if line < offset {
			continue
		}
		if collected >= limit {
			truncated = true
			break
		}
		if out.Len()+len(scanner.Text()) > maxOutputBytes {
			truncated = true
			break
		}
		out.WriteString(scanner.Text())
		out.WriteByte('\n')
		collected++
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if out.Len() == 0 {
		return "(empty)", nil
	}
	result := out.String()
	if truncated {
		result += fmt.Sprintf("\n[... truncated at %d lines; use offset/limit to read more]", collected)
	}
	return result, nil
}

// ---- write ----

type WriteTool struct{}

func (t *WriteTool) Name() string { return "write" }
func (t *WriteTool) Description() string {
	return "Create or overwrite a file in the workspace with the given content."
}
func (t *WriteTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path relative to the workspace"},"content":{"type":"string","description":"Full file content"}},"required":["path","content"]}`)
}

func (t *WriteTool) Run(ctx context.Context, raw json.RawMessage, env *Env) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	path, err := resolve(env.Workdir, args.Path)
	if err != nil {
		return "", err
	}
	before, _ := os.ReadFile(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(args.Content), 0o644); err != nil {
		return "", err
	}
	env.AddChange(Change{Path: args.Path, Before: string(before), After: args.Content, At: time.Now()})
	return fmt.Sprintf("wrote %d bytes to %s", len(args.Content), args.Path), nil
}

// ---- edit ----

type EditTool struct{}

func (t *EditTool) Name() string { return "edit" }
func (t *EditTool) Description() string {
	return "Replace an exact string in a workspace file. Fails if the old string is missing or ambiguous."
}
func (t *EditTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"oldString":{"type":"string"},"newString":{"type":"string"},"replaceAll":{"type":"boolean"}},"required":["path","oldString","newString"]}`)
}

func (t *EditTool) Run(ctx context.Context, raw json.RawMessage, env *Env) (string, error) {
	var args struct {
		Path       string `json:"path"`
		OldString  string `json:"oldString"`
		NewString  string `json:"newString"`
		ReplaceAll bool   `json:"replaceAll"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if args.OldString == "" {
		return "", fmt.Errorf("oldString must not be empty")
	}
	path, err := resolve(env.Workdir, args.Path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	before := string(data)
	content := before
	count := strings.Count(content, args.OldString)
	if count == 0 {
		return "", fmt.Errorf("oldString not found in %s", args.Path)
	}
	if count > 1 && !args.ReplaceAll {
		return "", fmt.Errorf("oldString occurs %d times in %s; set replaceAll or provide more context", count, args.Path)
	}
	if args.ReplaceAll {
		content = strings.ReplaceAll(content, args.OldString, args.NewString)
	} else {
		content = strings.Replace(content, args.OldString, args.NewString, 1)
		count = 1
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	env.AddChange(Change{Path: args.Path, Before: before, After: content, At: time.Now()})
	return fmt.Sprintf("edited %s (%d replacement(s))", args.Path, count), nil
}

// ---- bash ----

type BashTool struct{}

func (t *BashTool) Name() string { return "bash" }
func (t *BashTool) Description() string {
	return "Run a shell command in the workspace and return its combined output and exit code."
}
func (t *BashTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"cwd":{"type":"string"},"timeout":{"type":"integer","description":"Timeout in seconds (default 120)"}},"required":["command"]}`)
}

func (t *BashTool) Run(ctx context.Context, raw json.RawMessage, env *Env) (string, error) {
	var args struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Command) == "" {
		return "", fmt.Errorf("command must not be empty")
	}
	cwd, err := resolve(env.Workdir, args.Cwd)
	if err != nil {
		return "", err
	}
	timeout := args.Timeout
	if timeout <= 0 {
		timeout = 120
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := shellCommand(runCtx, env.Shell, args.Command)
	cmd.Dir = cwd
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	proc := env.StartProcess(args.Command, cwd, cancel)
	runErr := cmd.Run()

	out, truncated := truncate(buf.String(), maxOutputBytes)
	if truncated {
		out += "\n[... output truncated]"
	}
	if strings.TrimSpace(out) == "" {
		out = "(no output)"
	}
	timedOut := runCtx.Err() == context.DeadlineExceeded
	exitCode := 0
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else if !timedOut {
			env.EndProcess(proc, -1, out)
			return "", runErr
		}
	}
	if timedOut {
		exitCode = -1
	}
	env.EndProcess(proc, exitCode, out)
	if timedOut {
		return fmt.Sprintf("command timed out after %ds (exit code %d)\n%s", timeout, exitCode, out), nil
	}
	return fmt.Sprintf("exit code %d\n%s", exitCode, out), nil
}

func shellCommand(ctx context.Context, shell, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		if shell == "" {
			shell = "cmd.exe"
		}
		lower := strings.ToLower(shell)
		if strings.HasSuffix(lower, "powershell.exe") || strings.HasSuffix(lower, "pwsh.exe") {
			return exec.CommandContext(ctx, shell, "-NoProfile", "-Command", command)
		}
		return exec.CommandContext(ctx, shell, "/c", command)
	}
	if shell == "" {
		shell = "/bin/sh"
	}
	return exec.CommandContext(ctx, shell, "-c", command)
}

// ---- grep ----

type GrepTool struct{}

func (t *GrepTool) Name() string { return "grep" }
func (t *GrepTool) Description() string {
	return "Search file contents with a regular expression. Returns path:line: text matches."
}
func (t *GrepTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Go regular expression"},"path":{"type":"string","description":"Directory or file to search (default .)"},"include":{"type":"string","description":"Glob to filter file names, e.g. *.go"},"maxResults":{"type":"integer"}},"required":["pattern"]}`)
}

func (t *GrepTool) Run(ctx context.Context, raw json.RawMessage, env *Env) (string, error) {
	var args struct {
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		Include    string `json:"include"`
		MaxResults int    `json:"maxResults"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}
	root, err := resolve(env.Workdir, args.Path)
	if err != nil {
		return "", err
	}
	max := args.MaxResults
	if max <= 0 || max > maxGrepResults {
		max = maxGrepResults
	}
	var results []string
	walk := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if skipDirs[d.Name()] && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if args.Include != "" {
			if ok, _ := doublestar.Match(args.Include, d.Name()); !ok {
				return nil
			}
		}
		if len(results) >= max {
			return filepath.SkipAll
		}
		data, err := os.ReadFile(path)
		if err != nil || bytes.IndexByte(data[:min(len(data), 8192)], 0) >= 0 {
			return nil
		}
		rel, _ := filepath.Rel(env.Workdir, path)
		scanner := bufio.NewScanner(bytes.NewReader(data))
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(rel), lineNo, strings.TrimSpace(line)))
				if len(results) >= max {
					return filepath.SkipAll
				}
			}
		}
		return nil
	}
	if err := filepath.WalkDir(root, walk); err != nil && err != filepath.SkipAll && err != context.Canceled {
		return "", err
	}
	if len(results) == 0 {
		return "no matches", nil
	}
	out := strings.Join(results, "\n")
	if len(results) >= max {
		out += fmt.Sprintf("\n[... showing first %d matches]", max)
	}
	return out, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---- glob ----

type GlobTool struct{}

func (t *GlobTool) Name() string { return "glob" }
func (t *GlobTool) Description() string {
	return "Find files matching a doublestar glob pattern, e.g. **/*.go."
}
func (t *GlobTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string","description":"Base directory (default .)"}},"required":["pattern"]}`)
}

func (t *GlobTool) Run(ctx context.Context, raw json.RawMessage, env *Env) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	base, err := resolve(env.Workdir, args.Path)
	if err != nil {
		return "", err
	}
	matches, err := doublestar.Glob(os.DirFS(base), args.Pattern, doublestar.WithFilesOnly())
	if err != nil {
		return "", err
	}
	sort.Strings(matches)
	if len(matches) > maxGlobResults {
		matches = matches[:maxGlobResults]
	}
	if len(matches) == 0 {
		return "no matches", nil
	}
	return strings.Join(matches, "\n"), nil
}

// ---- ls ----

type LsTool struct{}

func (t *LsTool) Name() string        { return "ls" }
func (t *LsTool) Description() string { return "List files and directories." }
func (t *LsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Directory (default .)"}}}`)
}

func (t *LsTool) Run(ctx context.Context, raw json.RawMessage, env *Env) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	path, err := resolve(env.Workdir, args.Path)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			lines = append(lines, name+"/")
			continue
		}
		if info, err := e.Info(); err == nil {
			lines = append(lines, fmt.Sprintf("%s\t%d", name, info.Size()))
		} else {
			lines = append(lines, name)
		}
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return "(empty directory)", nil
	}
	return strings.Join(lines, "\n"), nil
}

// ---- todo ----

type TodoTool struct{}

func (t *TodoTool) Name() string { return "todo" }
func (t *TodoTool) Description() string {
	return "Replace the current todo list. Use for multi-step tasks."
}
func (t *TodoTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"todos":{"type":"array","items":{"type":"object","properties":{"content":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","completed","cancelled"]},"priority":{"type":"string","enum":["high","medium","low"]}},"required":["content","status"]}}},"required":["todos"]}`)
}

func (t *TodoTool) Run(ctx context.Context, raw json.RawMessage, env *Env) (string, error) {
	var args struct {
		Todos []Todo `json:"todos"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	env.SetTodos(args.Todos)
	todos := env.TodosSnapshot()
	if len(todos) == 0 {
		return "(todo list cleared)", nil
	}
	var b strings.Builder
	for _, todo := range todos {
		b.WriteString(fmt.Sprintf("[%s] %s\n", todo.Status, todo.Content))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// ---- webfetch ----

type WebFetchTool struct{}

func (t *WebFetchTool) Name() string { return "webfetch" }
func (t *WebFetchTool) Description() string {
	return "Fetch a URL and return its text content (HTML tags stripped)."
}
func (t *WebFetchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"},"maxChars":{"type":"integer"}},"required":["url"]}`)
}

var (
	scriptRe = regexp.MustCompile(`(?is)<script.*?</script>`)
	styleRe  = regexp.MustCompile(`(?is)<style.*?</style>`)
	tagRe    = regexp.MustCompile(`(?s)<[^>]+>`)
	spaceRe  = regexp.MustCompile(`[ \t]+`)
)

func (t *WebFetchTool) Run(ctx context.Context, raw json.RawMessage, env *Env) (string, error) {
	var args struct {
		URL      string `json:"url"`
		MaxChars int    `json:"maxChars"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if !strings.HasPrefix(args.URL, "http://") && !strings.HasPrefix(args.URL, "https://") {
		return "", fmt.Errorf("url must start with http:// or https://")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, args.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "kern-cli")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", err
	}
	text := string(body)
	if strings.Contains(resp.Header.Get("Content-Type"), "html") {
		text = scriptRe.ReplaceAllString(text, " ")
		text = styleRe.ReplaceAllString(text, " ")
		text = tagRe.ReplaceAllString(text, " ")
		text = strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`).Replace(text)
		text = spaceRe.ReplaceAllString(text, " ")
	}
	text = strings.TrimSpace(text)
	max := args.MaxChars
	if max <= 0 {
		max = maxOutputBytes
	}
	if len(text) > max {
		text = text[:max] + "\n[... truncated]"
	}
	return text, nil
}
