// Package config loads Kern configuration, with drop-in compatibility for
// opencode.json/opencode.jsonc plus a higher-precedence kern.json overlay.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tailscale/hujson"

	"github.com/GovnocoderAI/kern/internal/util"
)

type Modalities struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
}

type Limit struct {
	Context int `json:"context,omitempty"`
	Output  int `json:"output,omitempty"`
}

type Model struct {
	ID         string      `json:"id,omitempty"`
	Name       string      `json:"name,omitempty"`
	Attachment bool        `json:"attachment,omitempty"`
	Modalities *Modalities `json:"modalities,omitempty"`
	Limit      *Limit      `json:"limit,omitempty"`
}

type ProviderOptions struct {
	BaseURL string            `json:"baseURL,omitempty"`
	APIKey  string            `json:"apiKey,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// ProviderKindField is the explicit kind override in config JSON.
// It is used for deserialization and validation.
type ProviderKindField string

// ValidKinds lists the wire protocols Kern supports.
var ValidKinds = []string{"openai", "anthropic", "google"}

type Provider struct {
	ID           string           `json:"-"`
	NPM          string           `json:"npm,omitempty"`
	Name         string           `json:"name,omitempty"`
	KindOverride string           `json:"kind,omitempty"`
	Options      ProviderOptions  `json:"options,omitempty"`
	Settings     ProviderOptions  `json:"settings,omitempty"`
	Models       map[string]Model `json:"models,omitempty"`
	Extra        map[string]any   `json:"-"`
}

type Agent struct {
	Description string `json:"description,omitempty"`
	Model       string `json:"model,omitempty"`
	Mode        string `json:"mode,omitempty"`
	System      string `json:"system,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

type PermissionRule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

type fileConfig struct {
	Schema       string              `json:"$schema"`
	Model        string              `json:"model"`
	SmallModel   string              `json:"small_model"`
	DefaultAgent string              `json:"default_agent"`
	Shell        string              `json:"shell"`
	Theme        string              `json:"theme"`
	Provider     map[string]Provider `json:"provider"`
	Providers    map[string]Provider `json:"providers"`
	Agent        map[string]Agent    `json:"agent"`
	Agents       map[string]Agent    `json:"agents"`
	Permissions  []PermissionRule    `json:"permissions"`
	Permission   json.RawMessage     `json:"permission"`
	Instructions []string            `json:"instructions"`
}

// Config is the merged, resolved configuration.
type Config struct {
	Workdir      string
	Model        string
	SmallModel   string
	DefaultAgent string
	Shell        string
	Theme        string
	Providers    map[string]*Provider
	Agents       map[string]*Agent
	Permissions  []PermissionRule
	Instructions []string
	Sources      []string
	// External lists credentials discovered from other tools (opencode is
	// read as a normal config source; this covers Codex, Continue, Gemini CLI,
	// Aider and .env files). It is lowest precedence.
	External []ExternalCred
}

// Load discovers and merges every configuration file relevant to workdir.
func Load(workdir string) (*Config, error) {
	abs, err := filepath.Abs(workdir)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Workdir:   abs,
		Providers: map[string]*Provider{},
		Agents:    map[string]*Agent{},
	}
	for _, path := range discover(abs) {
		fc, err := loadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		cfg.merge(fc)
		cfg.Sources = append(cfg.Sources, path)
	}

	// Lowest precedence: credentials discovered from other CLIs. A provider
	// is only added when the user has not configured one with that id, so an
	// explicit kern.json / opencode.json always wins.
	cfg.External = DiscoverExternal(abs)
	for _, ec := range cfg.External {
		if _, ok := cfg.Providers[ec.ProviderID]; ok {
			continue
		}
		p := &Provider{ID: ec.ProviderID, KindOverride: ec.Kind, Models: map[string]Model{}}
		p.Options.BaseURL = ec.BaseURL
		cfg.Providers[ec.ProviderID] = p
	}
	return cfg, nil
}

func discover(workdir string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		if util.Exists(p) {
			seen[p] = true
			out = append(out, p)
		}
	}

	// Lowest precedence: machine-wide.
	addPairs := func(dir string) {
		for _, name := range []string{"opencode.json", "opencode.jsonc", "kern.json", "kern.jsonc"} {
			add(filepath.Join(dir, name))
		}
	}
	addPairs(util.SystemOpencodeConfigDir())
	addPairs(util.SystemConfigDir())

	// Global user config. The legacy ~/.config/kern location is read first
	// (lower precedence) so v0.1 installs keep working after the ~/.kern move.
	addPairs(util.OpencodeConfigDir())
	addPairs(util.KernLegacyConfigDir())
	addPairs(util.KernConfigDir())

	// Project configs, from the filesystem root down to the working directory.
	var dirs []string
	for d := workdir; ; d = filepath.Dir(d) {
		dirs = append(dirs, d)
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		d := dirs[i]
		for _, name := range []string{"opencode.json", "opencode.jsonc", "kern.json", "kern.jsonc"} {
			add(filepath.Join(d, name))
		}
		for _, sub := range []string{".opencode", ".kern"} {
			for _, name := range []string{"opencode.json", "opencode.jsonc", "kern.json", "kern.jsonc"} {
				add(filepath.Join(d, sub, name))
			}
		}
	}
	return out
}

func loadFile(path string) (*fileConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Editors on Windows (Notepad, PowerShell) often prepend a UTF-8 BOM,
	// which JSON parsers reject. Strip it before parsing.
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	standard, err := hujson.Standardize(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid JSONC: %w", err)
	}
	var tree any
	if err := json.Unmarshal(standard, &tree); err != nil {
		return nil, err
	}
	base := filepath.Dir(path)
	tree = substituteValue(tree, base)
	normalized, err := json.Marshal(tree)
	if err != nil {
		return nil, err
	}
	var fc fileConfig
	if err := json.Unmarshal(normalized, &fc); err != nil {
		return nil, err
	}
	for id, p := range fc.Provider {
		p.ID = id
		fc.Provider[id] = p
	}
	for id, p := range fc.Providers {
		p.ID = id
		fc.Providers[id] = p
	}
	return &fc, nil
}

var (
	envPattern  = regexp.MustCompile(`\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)
	filePattern = regexp.MustCompile(`\{file:([^}]+)\}`)
)

func substituteValue(v any, base string) any {
	switch x := v.(type) {
	case string:
		return substituteString(x, base)
	case map[string]any:
		for k, val := range x {
			x[k] = substituteValue(val, base)
		}
		return x
	case []any:
		for i, val := range x {
			x[i] = substituteValue(val, base)
		}
		return x
	default:
		return v
	}
}

func substituteString(s, base string) string {
	s = envPattern.ReplaceAllStringFunc(s, func(m string) string {
		name := envPattern.FindStringSubmatch(m)[1]
		return os.Getenv(name)
	})
	s = filePattern.ReplaceAllStringFunc(s, func(m string) string {
		p := strings.TrimSpace(filePattern.FindStringSubmatch(m)[1])
		if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
			p = filepath.Join(util.Home(), strings.TrimLeft(p[1:], `/\`))
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(base, p)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return ""
		}
		return string(b)
	})
	return s
}

func (c *Config) merge(fc *fileConfig) {
	if fc.Model != "" {
		c.Model = fc.Model
	}
	if fc.SmallModel != "" {
		c.SmallModel = fc.SmallModel
	}
	if fc.DefaultAgent != "" {
		c.DefaultAgent = fc.DefaultAgent
	}
	if fc.Shell != "" {
		c.Shell = fc.Shell
	}
	if fc.Theme != "" {
		c.Theme = fc.Theme
	}
	for id, p := range fc.Provider {
		c.mergeProvider(id, p)
	}
	for id, p := range fc.Providers {
		c.mergeProvider(id, p)
	}
	for id, a := range fc.Agent {
		ac := a
		c.Agents[id] = &ac
	}
	for id, a := range fc.Agents {
		ac := a
		c.Agents[id] = &ac
	}
	c.Permissions = append(c.Permissions, fc.Permissions...)
	c.Instructions = append(c.Instructions, fc.Instructions...)
}

func (c *Config) mergeProvider(id string, p Provider) {
	existing := c.Providers[id]
	if existing == nil {
		existing = &Provider{ID: id, Models: map[string]Model{}}
		c.Providers[id] = existing
	}
	existing.ID = id
	if p.NPM != "" {
		existing.NPM = p.NPM
	}
	if p.Name != "" {
		existing.Name = p.Name
	}
	mergeOptions(&existing.Options, p.Options)
	mergeOptions(&existing.Settings, p.Settings)
	if existing.Models == nil {
		existing.Models = map[string]Model{}
	}
	for mid, m := range p.Models {
		existing.Models[mid] = m
	}
}

func mergeOptions(dst *ProviderOptions, src ProviderOptions) {
	if src.BaseURL != "" {
		dst.BaseURL = src.BaseURL
	}
	if src.APIKey != "" {
		dst.APIKey = src.APIKey
	}
	if len(src.Headers) > 0 {
		if dst.Headers == nil {
			dst.Headers = map[string]string{}
		}
		for k, v := range src.Headers {
			dst.Headers[k] = v
		}
	}
}

// Kind maps a provider to the wire protocol implementation.
func (p *Provider) Kind() string {
	// An explicit kind in the config wins over inference.
	if p.KindOverride != "" {
		return p.KindOverride
	}
	s := strings.ToLower(p.NPM + " " + p.ID + " " + p.Name)
	switch {
	case strings.Contains(s, "anthropic"):
		return "anthropic"
	case strings.Contains(s, "google") || strings.Contains(s, "gemini"):
		return "google"
	default:
		return "openai"
	}
}

// Endpoint returns the provider base URL, including any configured override.
// Provider IDs containing "plusvibe" get the plusvibe endpoint by default.
func (p *Provider) Endpoint() string {
	if p.Options.BaseURL != "" {
		return strings.TrimRight(p.Options.BaseURL, "/")
	}
	if p.Settings.BaseURL != "" {
		return strings.TrimRight(p.Settings.BaseURL, "/")
	}
	switch p.Kind() {
	case "anthropic":
		return "https://api.anthropic.com"
	case "google":
		return "https://generativelanguage.googleapis.com/v1beta"
	case "openai":
		if strings.Contains(strings.ToLower(p.ID), "plusvibe") {
			return "https://plusvibeapi.ru/v1"
		}
		return "https://api.openai.com/v1"
	}
	return "https://api.openai.com/v1"
}

// Headers merges configured and static headers.
func (p *Provider) Headers() map[string]string {
	out := map[string]string{}
	for k, v := range p.Options.Headers {
		out[k] = v
	}
	for k, v := range p.Settings.Headers {
		out[k] = v
	}
	return out
}

// ProviderIDs returns the configured provider IDs, sorted.
func (c *Config) ProviderIDs() []string {
	ids := make([]string, 0, len(c.Providers))
	for id := range c.Providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ResolveModel expands "provider/model" references and bare model IDs.
func (c *Config) ResolveModel(ref string) (providerID, modelID string, err error) {
	if ref == "" {
		ref = c.Model
	}
	if ref == "" {
		return "", "", errors.New("no model configured; pass --model provider/model")
	}
	if i := strings.Index(ref, "/"); i > 0 {
		pid, mid := ref[:i], ref[i+1:]
		if _, ok := c.Providers[pid]; !ok {
			return "", "", fmt.Errorf("provider %q not found (known: %s)", pid, strings.Join(c.ProviderIDs(), ", "))
		}
		if mid == "" {
			return "", "", fmt.Errorf("model reference %q is missing a model id", ref)
		}
		return pid, mid, nil
	}
	for pid, p := range c.Providers {
		if _, ok := p.Models[ref]; ok {
			return pid, ref, nil
		}
	}
	if len(c.Providers) == 1 {
		for pid := range c.Providers {
			return pid, ref, nil
		}
	}
	return "", "", fmt.Errorf("model %q not found; use provider/model", ref)
}

// APIKey resolves a provider key from config, Kern auth, opencode auth, or env.
func (c *Config) APIKey(providerID string, auth *Auth) string {
	p := c.Providers[providerID]
	if p != nil {
		if p.Options.APIKey != "" {
			return p.Options.APIKey
		}
		if p.Settings.APIKey != "" {
			return p.Settings.APIKey
		}
	}
	if auth != nil {
		if k, ok := auth.Get(providerID); ok && k != "" {
			return k
		}
	}
	// Keys discovered from other CLIs (Codex, Continue, Gemini CLI, Aider, .env).
	for _, ec := range c.External {
		if ec.ProviderID == providerID && ec.Key != "" {
			return ec.Key
		}
	}
	kind := "openai"
	if p != nil {
		kind = p.Kind()
	}
	names := []string{strings.ToUpper(providerID) + "_API_KEY"}
	switch kind {
	case "anthropic":
		names = append(names, "ANTHROPIC_API_KEY")
	case "google":
		names = append(names, "GOOGLE_API_KEY", "GEMINI_API_KEY")
	default:
		names = append(names, "OPENAI_API_KEY")
	}
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return ""
}

// SortedModels returns a provider's model IDs, sorted.
func (p *Provider) SortedModels() []string {
	ids := make([]string, 0, len(p.Models))
	for id := range p.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ModelLabel returns a human friendly model label.
func (p *Provider) ModelLabel(id string) string {
	if m, ok := p.Models[id]; ok && m.Name != "" {
		return m.Name
	}
	return id
}

// Redacted returns a JSON-friendly view with API keys masked.
func (c *Config) Redacted() map[string]any {
	providers := map[string]any{}
	for id, p := range c.Providers {
		models := map[string]any{}
		for mid, m := range p.Models {
			models[mid] = m
		}
		providers[id] = map[string]any{
			"npm":     p.NPM,
			"name":    p.Name,
			"kind":    p.Kind(),
			"baseURL": p.Endpoint(),
			"apiKey":  mask(p.Options.APIKey, p.Settings.APIKey),
			"models":  models,
		}
	}
	return map[string]any{
		"workdir":       c.Workdir,
		"model":         c.Model,
		"small_model":   c.SmallModel,
		"default_agent": c.DefaultAgent,
		"providers":     providers,
		"sources":       c.Sources,
	}
}

func mask(values ...string) string {
	for _, v := range values {
		if v != "" {
			if len(v) <= 8 {
				return "****"
			}
			return v[:4] + "…" + v[len(v)-4:]
		}
	}
	return ""
}
