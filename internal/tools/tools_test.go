package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, r *Registry, name, args string) string {
	t.Helper()
	out, err := r.Run(context.Background(), name, json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return out
}

func TestWriteReadEdit(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir, "")

	if out := run(t, r, "write", `{"path":"a/b.txt","content":"hello world"}`); !strings.Contains(out, "wrote") {
		t.Fatalf("write: %s", out)
	}
	if out := run(t, r, "read", `{"path":"a/b.txt"}`); out != "hello world\n" {
		t.Fatalf("read: %q", out)
	}
	if out := run(t, r, "edit", `{"path":"a/b.txt","oldString":"world","newString":"kern"}`); !strings.Contains(out, "1 replacement") {
		t.Fatalf("edit: %s", out)
	}
	if out := run(t, r, "read", `{"path":"a/b.txt"}`); out != "hello kern\n" {
		t.Fatalf("read after edit: %q", out)
	}
}

func TestPathSandbox(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir, "")
	_, err := r.Run(context.Background(), "read", json.RawMessage(`{"path":"../escape.txt"}`))
	if err == nil {
		t.Fatal("expected sandbox error")
	}
}

func TestGlobAndGrep(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir, "")
	run(t, r, "write", `{"path":"src/a.go","content":"package main\nfunc Hello() {}\n"}`)
	run(t, r, "write", `{"path":"src/b.txt","content":"nothing here\n"}`)

	glob := run(t, r, "glob", `{"pattern":"**/*.go"}`)
	if !strings.Contains(glob, "src/a.go") {
		t.Fatalf("glob: %s", glob)
	}
	grep := run(t, r, "grep", `{"pattern":"func Hello","include":"*.go"}`)
	if !strings.Contains(grep, "src/a.go:2:") {
		t.Fatalf("grep: %s", grep)
	}
}

func TestTodo(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir, "")
	out := run(t, r, "todo", `{"todos":[{"content":"do it","status":"pending","priority":"high"}]}`)
	if !strings.Contains(out, "do it") {
		t.Fatalf("todo: %s", out)
	}
	if len(r.Env().TodosSnapshot()) != 1 {
		t.Fatalf("todos = %d", len(r.Env().TodosSnapshot()))
	}
}

func TestChangeTracking(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir, "")
	run(t, r, "write", `{"path":"a.txt","content":"one"}`)
	run(t, r, "edit", `{"path":"a.txt","oldString":"one","newString":"two"}`)
	changes := r.Env().ChangesSnapshot()
	if len(changes) != 2 {
		t.Fatalf("changes = %d", len(changes))
	}
	if changes[1].Before != "one" || changes[1].After != "two" {
		t.Fatalf("change = %+v", changes[1])
	}
}

func TestProcessTracking(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir, "")
	run(t, r, "bash", `{"command":"echo hi"}`)
	procs := r.Env().ProcessesSnapshot()
	if len(procs) != 1 {
		t.Fatalf("processes = %d", len(procs))
	}
	if procs[0].Running || procs[0].ExitCode != 0 || !strings.Contains(procs[0].Output, "hi") {
		t.Fatalf("process = %+v", procs[0])
	}
}

func TestBash(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir, "")
	out := run(t, r, "bash", `{"command":"echo hi"}`)
	if !strings.Contains(out, "hi") || !strings.Contains(out, "exit code 0") {
		t.Fatalf("bash: %s", out)
	}
}

func TestReadTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	content := strings.Repeat("line\n", 10)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(dir, "")
	out := run(t, r, "read", `{"path":"big.txt","limit":3}`)
	if strings.Count(out, "line\n") != 3 {
		t.Fatalf("expected 3 lines, got %q", out)
	}
}
