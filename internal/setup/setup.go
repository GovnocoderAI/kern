// Package setup implements the first-run wizard for Kern.
// It writes the initial global config, theme, and provider credentials
// into $KERN_HOME so subsequent launches skip the wizard.
package setup

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GovnocoderAI/kern/internal/util"
)

// Result is what the wizard produced.
type Result struct {
	Theme      string
	Provider   string
	APIKey     string
	Model      string
	CreatedDir bool
}

// IsInitialized reports whether Kern has been set up before.
// It returns true if $KERN_HOME/config/kern.json exists.
func IsInitialized() bool {
	path := filepath.Join(util.KernConfigDir(), "kern.json")
	return util.Exists(path)
}

// Run executes the first-launch wizard on stdin/stdout.
// It returns the resolved configuration and whether new files were created.
func Run() (*Result, error) {
	if IsInitialized() {
		return &Result{}, nil
	}

	cfgDir := util.KernConfigDir()
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", cfgDir, err)
	}
	if err := os.MkdirAll(util.KernDataDir(), 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", util.KernDataDir(), err)
	}

	r := &Result{CreatedDir: true}
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Kern first run")
	fmt.Println("==============")
	fmt.Println()

	// --- Theme ---
	fmt.Print("Pick a theme [amber] dark / light / amber / blue: ")
	theme := readLine(reader, "amber")
	r.Theme = normalizeTheme(theme)

	// --- Provider ---
	fmt.Println()
	fmt.Print("Pick a provider [plusvibe] plusvibe / openai / anthropic / google: ")
	prov := readLine(reader, "plusvibe")
	r.Provider = normalizeProvider(prov)

	// --- API key ---
	fmt.Println()
	fmt.Printf("API key for %s (paste, or leave empty to set later): ", r.Provider)
	key := readLine(reader, "")
	if key != "" {
		r.APIKey = key
	} else {
		// No key yet — note it for later.
		fmt.Printf("OK, you can add it later with: kern auth set %s <key>\n", r.Provider)
	}

	// --- Model ---
	fmt.Println()
	defaultModel := defaultModelFor(r.Provider)
	fmt.Printf("Model [%s]: ", defaultModel)
	model := readLine(reader, defaultModel)
	r.Model = model

	// --- Write config ---
	configPath := filepath.Join(cfgDir, "kern.json")
	content := fmt.Sprintf(`{
  "model": %q,
  "theme": %q,
  "provider": {
    %q: {
      "kind": %q
    }
  }
}
`, r.Model, r.Theme, r.Provider, providerKind(r.Provider))
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", configPath, err)
	}

	// --- Write auth (if key provided) ---
	if r.APIKey != "" {
		if err := writeAuthKey(r.Provider, r.APIKey); err != nil {
			return nil, fmt.Errorf("write auth.json: %w", err)
		}
	}

	fmt.Println()
	fmt.Printf("Config:  %s\n", configPath)
	fmt.Println()
	fmt.Println("Ready. Run kern to start.")
	return r, nil
}

// RunInteractive is a variant that accepts a provider for the wizard step,
// useful for testing.
func RunInteractive(provider string) (*Result, error) {
	return Run()
}

func readLine(r *bufio.Reader, def string) string {
	line, _ := r.ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return def
	}
	return line
}

func normalizeTheme(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "dark", "light", "amber", "blue", "":
		if strings.TrimSpace(s) == "" {
			return "amber"
		}
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return "amber"
	}
}

func normalizeProvider(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "openai", "anthropic", "google", "gemini", "":
		if strings.TrimSpace(s) == "" {
			return "plusvibe"
		}
		if strings.ToLower(strings.TrimSpace(s)) == "gemini" {
			return "google"
		}
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return "plusvibe"
	}
}

func providerKind(p string) string {
	switch p {
	case "openai":
		return "openai"
	case "anthropic":
		return "anthropic"
	case "google":
		return "google"
	default:
		return "openai"
	}
}

func defaultModelFor(p string) string {
	switch p {
	case "openai":
		return "gpt-4o"
	case "anthropic":
		return "[REDACTED]"
	case "google":
		return "gemini-4-argon"
	default:
		return "gemini-4-argon"
	}
}

// writeAuthKey stores the key in $KERN_HOME/config/auth.json.
func writeAuthKey(providerID, key string) error {
	authPath := filepath.Join(util.KernConfigDir(), "auth.json")
	// Simple: write a minimal JSON object.
	content := fmt.Sprintf(`{
  %q: %q
}
`, providerID, key)
	if err := os.WriteFile(authPath, []byte(content), 0o600); err != nil {
		return err
	}
	return nil
}
