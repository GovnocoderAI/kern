package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseValid(t *testing.T) {
	raw := `---
name: code-review
description: Reviews diffs
model: plusvibe/m
---
You are a careful reviewer.`
	s, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "code-review" || s.Description != "Reviews diffs" || s.Model != "plusvibe/m" {
		t.Fatalf("parsed = %+v", s)
	}
	if s.Prompt != "You are a careful reviewer." {
		t.Fatalf("prompt = %q", s.Prompt)
	}
}

func TestParseNoFrontmatter(t *testing.T) {
	_, err := Parse("no frontmatter here")
	if err == nil {
		t.Fatal("expected an error for missing frontmatter")
	}
}

func TestParseEmptyBody(t *testing.T) {
	_, err := Parse("---\nname: x\n---\n")
	if err == nil {
		t.Fatal("expected an error for empty body")
	}
}

func TestParseMissingName(t *testing.T) {
	_, err := Parse("---\ndescription: x\n---\nbody")
	if err == nil {
		t.Fatal("expected an error for missing name")
	}
}

func TestLoadDirAndOverride(t *testing.T) {
	dir := t.TempDir()
	// Project skill that overrides a global one.
	if err := os.MkdirAll(filepath.Join(dir, ".kern", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".kern", "skills", "hello.md"),
		[]byte("---\nname: hello\ndescription: project\n---\nproject prompt"), 0o644); err != nil {
		t.Fatal(err)
	}
	globalDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(globalDir, "hello.md"),
		[]byte("---\nname: hello\ndescription: global\n---\nglobal prompt"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Set KERN_CONFIG_DIR so LoadGlobal uses our temp dir.
	old := os.Getenv("KERN_CONFIG_DIR")
	t.Setenv("KERN_CONFIG_DIR", globalDir)
	defer os.Setenv("KERN_CONFIG_DIR", old)

	all := Load(dir)
	if len(all) != 1 {
		t.Fatalf("skills = %d", len(all))
	}
	if all[0].Description != "project" {
		t.Fatalf("expected project override, got %+v", all[0])
	}
}

func TestLoadProjectOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".kern", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".kern", "skills", "x.md"),
		[]byte("---\nname: x\n---\nprompt x"), 0o644); err != nil {
		t.Fatal(err)
	}
	skills := LoadProject(dir)
	if len(skills) != 1 || skills[0].Name != "x" {
		t.Fatalf("skills = %+v", skills)
	}
}
