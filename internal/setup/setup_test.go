package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GovnocoderAI/kern/internal/config"
)

// TestWizardFlow drives the wizard with scripted answers: dark theme,
// the second provider (openai), a key, and the default model.
func TestWizardFlow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KERN_HOME", home)
	t.Setenv("KERN_CONFIG_DIR", "")

	in := strings.NewReader("dark\n2\nsk-test-key\n\n")
	var out bytes.Buffer
	r, err := RunWith(in, &out, false)
	if err != nil {
		t.Fatalf("RunWith: %v\n%s", err, out.String())
	}
	if r.Theme != "dark" {
		t.Fatalf("theme = %q, want dark", r.Theme)
	}
	if r.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", r.Provider)
	}
	if r.Model != "openai/gpt-4o" {
		t.Fatalf("model = %q, want openai/gpt-4o", r.Model)
	}

	cfgPath := filepath.Join(home, "config", "kern.json")
	if _, statErr := os.Stat(cfgPath); statErr != nil {
		t.Fatalf("config not written: %v", statErr)
	}
	authPath := filepath.Join(home, "config", "auth.json")
	if _, statErr := os.Stat(authPath); statErr != nil {
		t.Fatalf("auth not written: %v", statErr)
	}
}

// TestWizardMergesAuth ensures a second provider does not wipe the first key.
func TestWizardMergesAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KERN_HOME", home)
	t.Setenv("KERN_CONFIG_DIR", "")

	var out bytes.Buffer
	if _, err := RunWith(strings.NewReader("amber\n1\nfirst-key\n\n"), &out, false); err != nil {
		t.Fatal(err)
	}
	if _, err := RunWith(strings.NewReader("amber\n2\nsecond-key\n\n"), &out, false); err != nil {
		t.Fatal(err)
	}

	if got := readKey(t, home, "plusvibe"); got != "first-key" {
		t.Fatalf("plusvibe key = %q, want first-key", got)
	}
	if got := readKey(t, home, "openai"); got != "second-key" {
		t.Fatalf("openai key = %q, want second-key", got)
	}
}

func TestNormalizeProviderAndTheme(t *testing.T) {
	cases := map[string]string{
		"":          "plusvibe",
		"gemini":    "google",
		"3":         "anthropic",
		"ANTHROPIC": "anthropic",
		"nonsense":  "plusvibe",
	}
	for in, want := range cases {
		if got := normalizeProvider(in); got != want {
			t.Errorf("normalizeProvider(%q) = %q, want %q", in, got, want)
		}
	}
	if got := normalizeTheme("LIGHT"); got != "light" {
		t.Errorf("normalizeTheme(LIGHT) = %q", got)
	}
	if got := normalizeTheme("purple"); got != "amber" {
		t.Errorf("normalizeTheme(purple) = %q, want amber", got)
	}
}

func TestNormalizeModel(t *testing.T) {
	if got := normalizeModel("plusvibe/gemini-4-argon"); got != "gemini-4-argon" {
		t.Errorf("got %q", got)
	}
	if got := normalizeModel("gpt-4o"); got != "gpt-4o" {
		t.Errorf("got %q", got)
	}
}

// readKey reads a provider key back from the auth store under home.
func readKey(t *testing.T, home, provider string) string {
	t.Helper()
	t.Setenv("KERN_HOME", home)
	auth := config.LoadAuth()
	key, _ := auth.Get(provider)
	return key
}
