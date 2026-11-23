package util

import (
	"os"
	"path/filepath"
	"runtime"
)

// Home returns the current user's home directory.
func Home() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return "."
}

// KernHome is the root of all Kern state: config, auth, data, sessions.
// It is $KERN_HOME, or ~/.kern by default.
func KernHome() string {
	if v := os.Getenv("KERN_HOME"); v != "" {
		return v
	}
	return filepath.Join(Home(), ".kern")
}

// KernConfigDir is where Kern writes its own configuration.
// It is $KERN_CONFIG_DIR, or $KERN_HOME/config, or ~/.kern/config.
func KernConfigDir() string {
	if v := os.Getenv("KERN_CONFIG_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("KERN_HOME"); v != "" {
		return filepath.Join(v, "config")
	}
	return filepath.Join(KernHome(), "config")
}

// KernLegacyConfigDir is the v0.1 location (~/.config/kern). It is still read
// as a lower-precedence fallback so existing installs keep working.
func KernLegacyConfigDir() string {
	if v := os.Getenv("KERN_LEGACY_CONFIG_DIR"); v != "" {
		return v
	}
	return filepath.Join(Home(), ".config", "kern")
}

// OpencodeConfigDir is the global opencode configuration directory.
func OpencodeConfigDir() string {
	if v := os.Getenv("OPENCODE_CONFIG_DIR"); v != "" {
		return v
	}
	return filepath.Join(Home(), ".config", "opencode")
}

// KernDataDir is where sessions and caches are stored.
// KERN_HOME/data wins over platform-specific locations.
func KernDataDir() string {
	if v := os.Getenv("KERN_DATA_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("KERN_HOME"); v != "" {
		return filepath.Join(v, "data")
	}
	switch runtime.GOOS {
	case "windows":
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "kern")
		}
	case "darwin":
		return filepath.Join(Home(), "Library", "Application Support", "kern")
	}
	return filepath.Join(Home(), ".local", "share", "kern")
}

// OpencodeDataDir is opencode's data directory, used for auth drop-in.
func OpencodeDataDir() string {
	if v := os.Getenv("OPENCODE_DATA_DIR"); v != "" {
		return v
	}
	return filepath.Join(Home(), ".local", "share", "opencode")
}

// SessionsDir is the directory holding persisted sessions.
func SessionsDir() string {
	return filepath.Join(KernDataDir(), "sessions")
}

// SystemConfigDir is the machine-wide configuration directory.
func SystemConfigDir() string {
	if runtime.GOOS == "windows" {
		if v := os.Getenv("ProgramData"); v != "" {
			return filepath.Join(v, "kern")
		}
	}
	return "/etc/kern"
}

// SystemOpencodeConfigDir is the machine-wide opencode configuration directory.
func SystemOpencodeConfigDir() string {
	if runtime.GOOS == "windows" {
		if v := os.Getenv("ProgramData"); v != "" {
			return filepath.Join(v, "opencode")
		}
	}
	return "/etc/opencode"
}

// Exists reports whether a path exists.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
