package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/GovnocoderAI/kern/internal/util"
)

// ExternalCred is one credential discovered outside Kern's own config: a key
// plus enough metadata to build a provider. Discovered keys are lower
// precedence than kern.json, opencode.json and Kern's own auth.json.
type ExternalCred struct {
	// Tool names the source, e.g. "opencode", "codex", "continue", ".env".
	Tool string
	// Path is where it was found (for doctor output).
	Path string
	// ProviderID is the Kern provider id, e.g. "openai", "yesapi".
	ProviderID string
	// Kind is the wire protocol: openai, anthropic or google.
	Kind string
	// BaseURL overrides the provider endpoint when known.
	BaseURL string
	// Key is the API key.
	Key string
}

// knownProviders maps common provider IDs to their wire protocol and endpoint,
// so a bare "<NAME>_API_KEY" can be turned into a working provider.
var knownProviders = map[string]struct {
	Kind    string
	BaseURL string
}{
	"openai":     {"openai", "https://api.openai.com/v1"},
	"anthropic":  {"anthropic", "https://api.anthropic.com"},
	"google":     {"google", "https://generativelanguage.googleapis.com/v1beta"},
	"gemini":     {"google", "https://generativelanguage.googleapis.com/v1beta"},
	"deepseek":   {"openai", "https://api.deepseek.com/v1"},
	"groq":       {"openai", "https://api.groq.com/openai/v1"},
	"openrouter": {"openai", "https://openrouter.ai/api/v1"},
	"mistral":    {"openai", "https://api.mistral.ai/v1"},
	"xai":        {"openai", "https://api.x.ai/v1"},
	"grok":       {"openai", "https://api.x.ai/v1"},
	"together":   {"openai", "https://api.together.xyz/v1"},
	"fireworks":  {"openai", "https://api.fireworks.ai/inference/v1"},
	"perplexity": {"openai", "https://api.perplexity.ai"},
	"cohere":     {"openai", "https://api.cohere.ai/compatibility/v1"},
	"cerebras":   {"openai", "https://api.cerebras.ai/v1"},
	"hyperbolic": {"openai", "https://api.hyperbolic.xyz/v1"},
	"dashscope":  {"openai", "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"},
	"moonshot":   {"openai", "https://api.moonshot.cn/v1"},
	"zhipu":      {"openai", "https://open.bigmodel.cn/api/paas/v4"},
	"ollama":     {"openai", "http://localhost:11434/v1"},
	"lmstudio":   {"openai", "http://localhost:1234/v1"},
	"plusvibe":   {"openai", "https://plusvibeapi.ru/v1"},
	"yesapi":     {"openai", "https://yesapi.farted.net/v1"},
}

// canonicalProvider normalises a provider hint to a known id, or returns the
// hint unchanged when unknown.
func canonicalProvider(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, "-", "")
	n = strings.ReplaceAll(n, "_", "")
	switch n {
	case "googleai", "googlegemini", "gemini", "google":
		return "google"
	case "openai", "":
		return "openai"
	case "anthropic", "claude":
		return "anthropic"
	}
	return n
}

// DiscoverExternal scans environment files and the config stores of other
// coding CLIs for API keys. The workdir is searched for a project .env.
func DiscoverExternal(workdir string) []ExternalCred {
	var out []ExternalCred
	seen := map[string]bool{}
	add := func(c ExternalCred) {
		if c.Key == "" || c.ProviderID == "" {
			return
		}
		// Keep the first (highest-precedence) key per provider.
		if seen[c.ProviderID] {
			return
		}
		seen[c.ProviderID] = true
		out = append(out, c)
	}

	// 1. .env files: project first, then home-level.
	envPaths := []string{
		filepath.Join(workdir, ".env"),
		filepath.Join(util.Home(), ".env"),
		filepath.Join(util.KernConfigDir(), ".env"),
		filepath.Join(util.Home(), ".config", "opencode", ".env"),
	}
	for _, p := range envPaths {
		for id, key := range parseDotEnv(p) {
			c := credForEnvName(id)
			if c.ProviderID == "" {
				continue // unknown provider; do not invent one from a random *_KEY
			}
			c.Key = key
			c.Tool = ".env"
			c.Path = p
			add(c)
		}
	}

	// 2. Gemini CLI: ~/.gemini/settings.json and ~/.gemini/.env
	for _, c := range parseGeminiCLI() {
		add(c)
	}

	// 3. OpenAI Codex CLI: ~/.codex/auth.json
	for _, c := range parseCodex() {
		add(c)
	}

	// 4. Continue: ~/.continue/config.json
	for _, c := range parseContinue() {
		add(c)
	}

	// 5. Aider: ~/.aider.conf.yml
	for _, c := range parseAider() {
		add(c)
	}

	return out
}

// credForEnvName maps an environment variable name (OPENAI_API_KEY,
// DEEPSEEK_API_KEY, …) to an ExternalCred skeleton. It returns an empty
// ProviderID for names that do not correspond to a known provider, so random
// *_KEY variables are ignored.
func credForEnvName(envName string) ExternalCred {
	up := strings.ToUpper(envName)
	name := strings.TrimSuffix(up, "_API_KEY")
	if name == up {
		// Only accept a bare "_KEY" suffix when the prefix is a known provider.
		name = strings.TrimSuffix(up, "_KEY")
	}
	id := canonicalProvider(strings.ToLower(name))
	kp, ok := knownProviders[id]
	if !ok {
		return ExternalCred{}
	}
	return ExternalCred{ProviderID: id, Kind: kp.Kind, BaseURL: kp.BaseURL}
}

// parseDotEnv returns KEY=VALUE pairs from a .env file, ignoring comments and
// blank lines. Missing files yield nil.
func parseDotEnv(path string) map[string]string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		v = strings.Trim(v, `"'`)
		up := strings.ToUpper(k)
		if !strings.HasSuffix(up, "_API_KEY") && !strings.HasSuffix(up, "_KEY") {
			continue
		}
		out[k] = v
	}
	return out
}

func parseGeminiCLI() []ExternalCred {
	var out []ExternalCred
	// ~/.gemini/.env (GEMINI_API_KEY=...)
	for name, key := range parseDotEnv(filepath.Join(util.Home(), ".gemini", ".env")) {
		c := credForEnvName(name)
		c.Key = key
		c.Tool = "gemini-cli"
		c.Path = filepath.Join(util.Home(), ".gemini", ".env")
		out = append(out, c)
	}
	// ~/.gemini/settings.json may hold a top-level apiKey.
	path := filepath.Join(util.Home(), ".gemini", "settings.json")
	if raw, err := os.ReadFile(path); err == nil {
		var s struct {
			APIKey string `json:"apiKey"`
		}
		if json.Unmarshal(raw, &s) == nil && s.APIKey != "" {
			out = append(out, ExternalCred{
				Tool: "gemini-cli", Path: path, ProviderID: "google",
				Kind: "google", BaseURL: knownProviders["google"].BaseURL, Key: s.APIKey,
			})
		}
	}
	return out
}

func parseCodex() []ExternalCred {
	path := filepath.Join(util.Home(), ".codex", "auth.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var s struct {
		OpenAIAPIKey string `json:"OPENAI_API_KEY"`
	}
	if json.Unmarshal(raw, &s) != nil || s.OpenAIAPIKey == "" {
		return nil
	}
	return []ExternalCred{{
		Tool: "codex", Path: path, ProviderID: "openai",
		Kind: "openai", BaseURL: knownProviders["openai"].BaseURL, Key: s.OpenAIAPIKey,
	}}
}

func parseContinue() []ExternalCred {
	path := filepath.Join(util.Home(), ".continue", "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var s struct {
		Models []struct {
			Provider string `json:"provider"`
			ApiKey   string `json:"apiKey"`
			ApiBase  string `json:"apiBase"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	var out []ExternalCred
	for _, m := range s.Models {
		if m.ApiKey == "" {
			continue
		}
		id := canonicalProvider(m.Provider)
		c := ExternalCred{Tool: "continue", Path: path, ProviderID: id, Kind: "openai", Key: m.ApiKey}
		if kp, ok := knownProviders[id]; ok {
			c.Kind = kp.Kind
			c.BaseURL = kp.BaseURL
		}
		if m.ApiBase != "" {
			c.BaseURL = m.ApiBase
		}
		out = append(out, c)
	}
	return out
}

// parseAider reads ~/.aider.conf.yml looking for "<provider>-api-key: value"
// lines. It is a deliberately small parser: no YAML dependency and only the
// fields we need.
func parseAider() []ExternalCred {
	path := filepath.Join(util.Home(), ".aider.conf.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []ExternalCred
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "api-key") && !strings.Contains(lower, "api_key") {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			continue
		}
		field := strings.TrimSpace(lower[:colon])
		val := strings.Trim(strings.TrimSpace(line[colon+1:]), `"'`)
		if val == "" {
			continue
		}
		// e.g. "openai-api-key" -> "openai"
		field = strings.TrimSuffix(field, "-api-key")
		field = strings.TrimSuffix(field, "_api_key")
		id := canonicalProvider(field)
		c := ExternalCred{Tool: "aider", Path: path, ProviderID: id, Kind: "openai", Key: val}
		if kp, ok := knownProviders[id]; ok {
			c.Kind = kp.Kind
			c.BaseURL = kp.BaseURL
		}
		out = append(out, c)
	}
	return out
}
