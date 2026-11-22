package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessToolList(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir, "")
	env := r.Env()
	env.StartProcess("sleep 100", dir, nil)
	out, err := r.Run(context.Background(), "process", json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 || out == "(no processes yet)" {
		t.Fatalf("list output = %q", out)
	}
}

func TestJokeTool(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir, "")
	out, err := r.Run(context.Background(), "joke", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("empty joke")
	}
	// Pick a specific one by index.
	out2, err := r.Run(context.Background(), "joke", json.RawMessage(`{"count":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if out2 == "" {
		t.Fatal("empty joke at count=1")
	}
}

func TestReadOffsetLimit(t *testing.T) {
	dir := t.TempDir()
	// Write a file with 10 lines.
	lines := []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "lines.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	// Read lines 3-5.
	r := NewRegistry(dir, "")
	out, err := r.Run(context.Background(), "read", json.RawMessage(`{"path":"lines.txt","offset":3,"limit":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "three") || !strings.Contains(out, "four") || !strings.Contains(out, "five") {
		t.Fatalf("read offset/limit output = %q", out)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

func indexOf(s, sub string) int {
	return strings.Index(s, sub)
}
