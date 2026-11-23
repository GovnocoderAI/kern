package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/GovnocoderAI/kern/internal/util"
)

// Auth stores provider credentials. Kern's own auth.json takes precedence over
// opencode's auth.json so existing opencode logins keep working.
type Auth struct {
	keys map[string]string
}

type authEntry struct {
	Key  string `json:"key"`
	Type string `json:"type"`
}

// LoadAuth reads Kern and opencode credential stores.
func LoadAuth() *Auth {
	a := &Auth{keys: map[string]string{}}
	// Lowest precedence first: opencode, then the v0.1 Kern location, then
	// the current one so the newest key wins.
	a.readInto(filepath.Join(util.OpencodeDataDir(), "auth.json"))
	a.readInto(filepath.Join(util.KernLegacyConfigDir(), "auth.json"))
	a.readInto(filepath.Join(util.KernConfigDir(), "auth.json"))
	return a
}

func (a *Auth) readInto(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	// Tolerate a UTF-8 BOM, which Windows editors add.
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return
	}
	for id, raw := range entries {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil && s != "" {
			a.keys[id] = s
			continue
		}
		var entry authEntry
		if err := json.Unmarshal(raw, &entry); err == nil && entry.Key != "" {
			a.keys[id] = entry.Key
		}
	}
}

// Get returns a stored key.
func (a *Auth) Get(providerID string) (string, bool) {
	if a == nil {
		return "", false
	}
	v, ok := a.keys[providerID]
	return v, ok
}

// List returns the providers with stored credentials.
func (a *Auth) List() []string {
	var out []string
	for id := range a.keys {
		out = append(out, id)
	}
	return out
}

func authPath() string {
	return filepath.Join(util.KernConfigDir(), "auth.json")
}

func readKernAuth() map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	raw, err := os.ReadFile(authPath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

func writeKernAuth(entries map[string]json.RawMessage) error {
	if err := os.MkdirAll(util.KernConfigDir(), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := authPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, authPath())
}

// Set stores a provider key in Kern's auth.json.
func SetKey(providerID, key string) error {
	entries := readKernAuth()
	raw, err := json.Marshal(key)
	if err != nil {
		return err
	}
	entries[providerID] = raw
	return writeKernAuth(entries)
}

// RemoveKey deletes a provider key from Kern's auth.json.
func RemoveKey(providerID string) error {
	entries := readKernAuth()
	delete(entries, providerID)
	return writeKernAuth(entries)
}
