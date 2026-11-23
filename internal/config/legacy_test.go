package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLegacyConfigDirIsRead verifies that a config written to the v0.1
// location (~/.config/kern) is still discovered after the move to ~/.kern.
func TestLegacyConfigDirIsRead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KERN_HOME", filepath.Join(home, "kernhome"))
	t.Setenv("KERN_CONFIG_DIR", "")
	t.Setenv("KERN_LEGACY_CONFIG_DIR", filepath.Join(home, "legacy"))
	t.Setenv("OPENCODE_CONFIG_DIR", filepath.Join(home, "no-opencode"))
	t.Setenv("ProgramData", filepath.Join(home, "programdata"))

	legacy := filepath.Join(home, "legacy", "kern.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"model":"legacy/model"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	project := filepath.Join(home, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(project)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "legacy/model" {
		t.Fatalf("legacy config not read: model = %q", cfg.Model)
	}
}

// TestCurrentConfigBeatsLegacy verifies precedence: ~/.kern wins.
func TestCurrentConfigBeatsLegacy(t *testing.T) {
	home := t.TempDir()
	current := filepath.Join(home, "kernhome")
	t.Setenv("KERN_HOME", current)
	t.Setenv("KERN_CONFIG_DIR", "")
	t.Setenv("KERN_LEGACY_CONFIG_DIR", filepath.Join(home, "legacy"))
	t.Setenv("OPENCODE_CONFIG_DIR", filepath.Join(home, "no-opencode"))
	t.Setenv("ProgramData", filepath.Join(home, "programdata"))

	writeJSON := func(dir, model string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "kern.json"),
			[]byte(`{"model":"`+model+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON(filepath.Join(home, "legacy"), "legacy/model")
	writeJSON(filepath.Join(current, "config"), "current/model")

	project := filepath.Join(home, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(project)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "current/model" {
		t.Fatalf("model = %q, want current/model", cfg.Model)
	}
}

// TestConfigWithBOM verifies that a config file saved with a UTF-8 BOM (as
// Windows Notepad and PowerShell do) still loads.
func TestConfigWithBOM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kern.json")
	body := []byte(`{"model":"x/y"}`)
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, body...)
	if err := os.WriteFile(path, withBOM, 0o644); err != nil {
		t.Fatal(err)
	}
	fc, err := loadFile(path)
	if err != nil {
		t.Fatalf("loadFile with BOM: %v", err)
	}
	if fc.Model != "x/y" {
		t.Fatalf("model = %q, want x/y", fc.Model)
	}
}
