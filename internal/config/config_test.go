package config

import (
	"os"
	"path/filepath"
	"testing"
)

func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("KERN_CONFIG_DIR", filepath.Join(home, "kern"))
	t.Setenv("OPENCODE_CONFIG_DIR", filepath.Join(home, "opencode"))
	t.Setenv("KERN_DATA_DIR", filepath.Join(home, "data"))
	t.Setenv("OPENCODE_DATA_DIR", filepath.Join(home, "odata"))
	t.Setenv("ProgramData", filepath.Join(home, "programdata"))
	return home
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOpencodeCompatibility(t *testing.T) {
	home := isolate(t)
	t.Setenv("PV_KEY", "secret-key")
	write(t, filepath.Join(home, "opencode", "opencode.json"), `{
  // JSONC comment and trailing comma
  "model": "plusvibe/claude-opus-5.5",
  "provider": {
    "plusvibe": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "PlusVibe",
      "options": {
        "baseURL": "https://example.test/v1",
        "apiKey": "{env:PV_KEY}",
      },
      "models": {
        "claude-opus-5.5": {"name": "Opus", "attachment": true},
      },
    },
  },
}`)

	cfg, err := Load(home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model != "plusvibe/claude-opus-5.5" {
		t.Fatalf("model = %q", cfg.Model)
	}
	pid, mid, err := cfg.ResolveModel("")
	if err != nil || pid != "plusvibe" || mid != "claude-opus-5.5" {
		t.Fatalf("ResolveModel = %q %q %v", pid, mid, err)
	}
	p := cfg.Providers["plusvibe"]
	if p == nil || p.Kind() != "openai" || p.Endpoint() != "https://example.test/v1" {
		t.Fatalf("provider = %+v", p)
	}
	if key := cfg.APIKey("plusvibe", nil); key != "secret-key" {
		t.Fatalf("api key = %q", key)
	}
	if p.SortedModels()[0] != "claude-opus-5.5" {
		t.Fatalf("models = %v", p.SortedModels())
	}
}

func TestOverlayPrecedence(t *testing.T) {
	home := isolate(t)
	write(t, filepath.Join(home, "opencode", "opencode.json"), `{
  "model": "a/one",
  "provider": {"a": {"npm": "@ai-sdk/openai-compatible", "models": {"one": {}}}}
}`)
	write(t, filepath.Join(home, "kern", "kern.json"), `{
  "model": "a/two",
  "provider": {"a": {"models": {"two": {}}}}
}`)
	cfg, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "a/two" {
		t.Fatalf("model = %q", cfg.Model)
	}
	p := cfg.Providers["a"]
	if len(p.Models) != 2 {
		t.Fatalf("models merged = %v", p.SortedModels())
	}
}

func TestProjectConfigOverridesGlobal(t *testing.T) {
	home := isolate(t)
	write(t, filepath.Join(home, "opencode", "opencode.json"), `{"model":"p/global"}`)
	project := filepath.Join(home, "project")
	write(t, filepath.Join(project, "kern.json"), `{"model":"p/project"}`)
	cfg, err := Load(project)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "p/project" {
		t.Fatalf("model = %q", cfg.Model)
	}
}

func TestResolveModelErrors(t *testing.T) {
	home := isolate(t)
	cfg, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := cfg.ResolveModel(""); err == nil {
		t.Fatal("expected error for empty model")
	}
	if _, _, err := cfg.ResolveModel("nope/x"); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
