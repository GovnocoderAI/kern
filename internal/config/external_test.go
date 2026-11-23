package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeAt writes content to path, creating parents.
func writeAt(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// isolateHome points HOME and the Kern dirs at a temp tree so discovery does
// not read the developer's real CLIs.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("KERN_HOME", filepath.Join(home, ".kern"))
	t.Setenv("KERN_CONFIG_DIR", "")
	t.Setenv("OPENCODE_CONFIG_DIR", filepath.Join(home, "no-oc"))
	t.Setenv("OPENCODE_DATA_DIR", filepath.Join(home, "no-oc-data"))
	t.Setenv("ProgramData", filepath.Join(home, "pd"))
	return home
}

func TestDiscoverDotEnv(t *testing.T) {
	home := isolateHome(t)
	project := filepath.Join(home, "project")
	writeAt(t, filepath.Join(project, ".env"),
		"# comment\nOPENAI_API_KEY=sk-openai\nDEEPSEEK_API_KEY='sk-deep'\nSSH_KEY=not-a-key\n\n")

	creds := DiscoverExternal(project)
	byID := map[string]string{}
	for _, c := range creds {
		byID[c.ProviderID] = c.Key
	}
	if byID["openai"] != "sk-openai" {
		t.Errorf("openai key = %q", byID["openai"])
	}
	if byID["deepseek"] != "sk-deep" {
		t.Errorf("deepseek key = %q", byID["deepseek"])
	}
	if _, ok := byID["ssh"]; ok {
		t.Error("SSH_KEY must not become a provider")
	}
}

func TestDiscoverCodex(t *testing.T) {
	home := isolateHome(t)
	writeAt(t, filepath.Join(home, ".codex", "auth.json"), `{"OPENAI_API_KEY":"sk-codex"}`)

	creds := DiscoverExternal(home)
	found := false
	for _, c := range creds {
		if c.ProviderID == "openai" && c.Key == "sk-codex" && c.Tool == "codex" {
			found = true
		}
	}
	if !found {
		t.Fatalf("codex key not discovered: %+v", creds)
	}
}

func TestDiscoverContinue(t *testing.T) {
	home := isolateHome(t)
	writeAt(t, filepath.Join(home, ".continue", "config.json"), `{
		"models": [
			{"provider":"anthropic","apiKey":"sk-ant","model":"claude"},
			{"provider":"openai","apiKey":"sk-oai","apiBase":"https://proxy.example/v1"}
		]
	}`)

	creds := DiscoverExternal(home)
	var anthropic, openai *ExternalCred
	for i := range creds {
		switch creds[i].ProviderID {
		case "anthropic":
			anthropic = &creds[i]
		case "openai":
			openai = &creds[i]
		}
	}
	if anthropic == nil || anthropic.Key != "sk-ant" || anthropic.Kind != "anthropic" {
		t.Fatalf("anthropic = %+v", anthropic)
	}
	if openai == nil || openai.BaseURL != "https://proxy.example/v1" {
		t.Fatalf("openai = %+v", openai)
	}
}

func TestDiscoverGeminiCLI(t *testing.T) {
	home := isolateHome(t)
	writeAt(t, filepath.Join(home, ".gemini", ".env"), "GEMINI_API_KEY=gm-key\n")

	creds := DiscoverExternal(home)
	for _, c := range creds {
		if c.ProviderID == "google" && c.Key == "gm-key" {
			return
		}
	}
	t.Fatalf("gemini key not discovered: %+v", creds)
}

func TestDiscoverAider(t *testing.T) {
	home := isolateHome(t)
	writeAt(t, filepath.Join(home, ".aider.conf.yml"),
		"# aider config\nopenai-api-key: sk-aider\nanthropic-api-key: sk-ant-aider\n")

	creds := DiscoverExternal(home)
	byID := map[string]string{}
	for _, c := range creds {
		byID[c.ProviderID] = c.Key
	}
	if byID["openai"] != "sk-aider" {
		t.Errorf("openai = %q", byID["openai"])
	}
	if byID["anthropic"] != "sk-ant-aider" {
		t.Errorf("anthropic = %q", byID["anthropic"])
	}
}

// TestExternalIsLowerPrecedence ensures an explicitly configured provider and
// key are not overridden by a discovered one.
func TestExternalIsLowerPrecedence(t *testing.T) {
	home := isolateHome(t)
	project := filepath.Join(home, "project")
	// Explicit Kern config: openai with an inline key.
	writeAt(t, filepath.Join(project, "kern.json"), `{
		"model": "openai/gpt-4o",
		"provider": {"openai": {"kind": "openai", "options": {"apiKey": "explicit-key"}}}
	}`)
	// A .env that would otherwise supply a different key.
	writeAt(t, filepath.Join(project, ".env"), "OPENAI_API_KEY=discovered-key\n")

	cfg, err := Load(project)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.APIKey("openai", nil); got != "explicit-key" {
		t.Fatalf("api key = %q, want explicit-key (config must win)", got)
	}
}

// TestExternalAddsProviderWhenMissing ensures a discovered key creates a usable
// provider when none is configured.
func TestExternalAddsProviderWhenMissing(t *testing.T) {
	home := isolateHome(t)
	project := filepath.Join(home, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeAt(t, filepath.Join(project, ".env"), "DEEPSEEK_API_KEY=sk-deep\n")

	cfg, err := Load(project)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.Providers["deepseek"]
	if !ok {
		t.Fatalf("deepseek provider not added: %v", cfg.ProviderIDs())
	}
	if p.Endpoint() != "https://api.deepseek.com/v1" {
		t.Fatalf("endpoint = %q", p.Endpoint())
	}
	if got := cfg.APIKey("deepseek", nil); got != "sk-deep" {
		t.Fatalf("key = %q", got)
	}
}
