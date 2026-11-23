// Package setup implements the first-run wizard for Kern.
// It writes the initial global config, theme, and provider credentials
// into $KERN_HOME so subsequent launches skip the wizard.
package setup

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GovnocoderAI/kern/internal/config"
	"github.com/GovnocoderAI/kern/internal/tui"
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

// providerPreset describes a built-in provider offered by the wizard.
type providerPreset struct {
	ID           string
	Label        string
	Kind         string
	BaseURL      string
	DefaultModel string
	KeyHint      string
	// ModelsEndpoint is appended to BaseURL for a connectivity check.
	ModelsPath string
}

var presets = []providerPreset{
	{
		ID:           "plusvibe",
		Label:        "PlusVibe API",
		Kind:         "openai",
		BaseURL:      "https://plusvibeapi.ru/v1",
		DefaultModel: "gemini-4-argon",
		KeyHint:      "",
		ModelsPath:   "/models",
	},
	{
		ID:           "openai",
		Label:        "OpenAI",
		Kind:         "openai",
		BaseURL:      "https://api.openai.com/v1",
		DefaultModel: "gpt-4o",
		KeyHint:      "sk-...",
		ModelsPath:   "/models",
	},
	{
		ID:           "anthropic",
		Label:        "Anthropic",
		Kind:         "anthropic",
		BaseURL:      "https://api.anthropic.com",
		DefaultModel: "claude-opus-4-5",
		KeyHint:      "",
		ModelsPath:   "/v1/models",
	},
	{
		ID:           "google",
		Label:        "Google Gemini",
		Kind:         "google",
		BaseURL:      "https://generativelanguage.googleapis.com/v1beta",
		DefaultModel: "gemini-2.5-pro",
		KeyHint:      "AIza...",
		ModelsPath:   "",
	},
}

// IsInitialized reports whether Kern has been set up before.
func IsInitialized() bool {
	return util.Exists(filepath.Join(util.KernConfigDir(), "kern.json"))
}

// Run executes the first-launch wizard on stdin/stdout.
func Run() (*Result, error) {
	return RunWith(os.Stdin, os.Stdout, false)
}

// RunWith runs the wizard against explicit streams. When checkKey is true the
// API key is verified with a live request before the config is written.
func RunWith(in io.Reader, out io.Writer, checkKey bool) (*Result, error) {
	cfgDir := util.KernConfigDir()
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", cfgDir, err)
	}
	if err := os.MkdirAll(util.KernDataDir(), 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", util.KernDataDir(), err)
	}

	r := &Result{CreatedDir: true}
	reader := bufio.NewReader(in)

	fmt.Fprintln(out, "Kern setup")
	fmt.Fprintln(out, "==========")
	fmt.Fprintln(out)

	// --- Theme ---
	fmt.Fprintf(out, "Theme %s [amber]: ", strings.Join(tui.AvailableThemes(), " / "))
	r.Theme = normalizeTheme(readLine(reader, "amber"))

	// --- Provider ---
	fmt.Fprintln(out)
	for i, p := range presets {
		fmt.Fprintf(out, "  %d) %-12s %s\n", i+1, p.ID, p.Label)
	}
	fmt.Fprint(out, "Provider [plusvibe]: ")
	r.Provider = normalizeProvider(readLine(reader, "plusvibe"))
	p := presetFor(r.Provider)

	// --- API key ---
	fmt.Fprintln(out)
	fmt.Fprintf(out, "API key for %s (%s), or empty to set later: ", r.Provider, p.KeyHint)
	key := readLine(reader, "")
	if key == "" {
		fmt.Fprintf(out, "  skipped — add it later with: kern auth set %s <key>\n", r.Provider)
	} else {
		r.APIKey = key
		if checkKey {
			if err := verifyKey(p, key); err != nil {
				fmt.Fprintf(out, "  warning: key check failed: %v\n", err)
				fmt.Fprint(out, "  keep it anyway? [Y/n]: ")
				if strings.EqualFold(strings.TrimSpace(readLine(reader, "y")), "n") {
					r.APIKey = ""
				}
			} else {
				fmt.Fprintln(out, "  key verified")
			}
		}
	}

	// --- Model ---
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Model [%s]: ", p.DefaultModel)
	r.Model = r.Provider + "/" + normalizeModel(readLine(reader, p.DefaultModel))

	if err := Write(r); err != nil {
		return nil, err
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Config: %s\n", filepath.Join(cfgDir, "kern.json"))
	if r.APIKey != "" {
		fmt.Fprintf(out, "Key:    %s\n", filepath.Join(cfgDir, "auth.json"))
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Ready. Run `kern` to start.")
	return r, nil
}

// Write persists the wizard result: kern.json plus auth.json (merged).
func Write(r *Result) error {
	if r.Provider == "" {
		return fmt.Errorf("setup: no provider selected")
	}
	p := presetFor(r.Provider)

	// A struct keeps the JSON key order stable and readable.
	type options struct {
		BaseURL string `json:"baseURL"`
	}
	type providerCfg struct {
		Name    string  `json:"name"`
		Kind    string  `json:"kind"`
		Options options `json:"options"`
	}
	type fileCfg struct {
		Model    string                 `json:"model"`
		Theme    string                 `json:"theme"`
		Provider map[string]providerCfg `json:"provider"`
	}
	cfg := fileCfg{
		Model: r.Model,
		Theme: r.Theme,
		Provider: map[string]providerCfg{
			p.ID: {Name: p.Label, Kind: p.Kind, Options: options{BaseURL: p.BaseURL}},
		},
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(util.KernConfigDir(), "kern.json")
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	if r.APIKey != "" {
		if err := config.SetKey(p.ID, r.APIKey); err != nil {
			return fmt.Errorf("write auth: %w", err)
		}
	}
	return nil
}

// verifyKey issues a lightweight authenticated request to the provider.
func verifyKey(p providerPreset, key string) error {
	if p.ModelsPath == "" {
		return nil // provider has no models endpoint we can probe
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	url := strings.TrimRight(p.BaseURL, "/") + p.ModelsPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	switch p.Kind {
	case "anthropic":
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	case "google":
		req.Header.Set("x-goog-api-key", key)
	default:
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func readLine(r *bufio.Reader, def string) string {
	line, err := r.ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	if err != nil && line == "" {
		return def
	}
	if line == "" {
		return def
	}
	return line
}

func normalizeTheme(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, name := range tui.AvailableThemes() {
		if s == name {
			return name
		}
	}
	return "amber"
}

func normalizeProvider(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "gemini" {
		return "google"
	}
	for _, p := range presets {
		if s == p.ID {
			return p.ID
		}
	}
	// Numeric selection, e.g. "2".
	if n := len(s); n == 1 && s[0] >= '1' && s[0] <= '9' {
		idx := int(s[0] - '1')
		if idx < len(presets) {
			return presets[idx].ID
		}
	}
	return "plusvibe"
}

func normalizeModel(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "/"); i > 0 {
		return s[i+1:]
	}
	return s
}

func presetFor(id string) providerPreset {
	for _, p := range presets {
		if p.ID == id {
			return p
		}
	}
	return presets[0]
}
