// Package version reports the build identity of the running binary.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// These values can be injected at build time via -ldflags. When they are left
// at their defaults, Version falls back to the module build info that the Go
// toolchain records automatically for `go build` and `go install`.
var (
	Version = ""
	Commit  = ""
	Date    = ""
)

const devVersion = "0.0.0-dev"

// Info is the resolved build identity.
type Info struct {
	Version  string
	Commit   string
	Date     string
	GoVer    string
	Modified bool
}

// Resolve reads ldflags values first and falls back to build info.
func Resolve() Info {
	info := Info{Version: Version, Commit: Commit, Date: Date, GoVer: runtime.Version()}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		if info.Version == "" {
			info.Version = devVersion
		}
		return info
	}
	info.GoVer = bi.GoVersion
	if info.Version == "" {
		// Module versions look like v0.2.0 or v0.2.0-0.20240101...-abcdef.
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			info.Version = strings.TrimPrefix(v, "v")
		}
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if info.Commit == "" {
				info.Commit = truncateCommit(s.Value)
			}
		case "vcs.time":
			if info.Date == "" {
				info.Date = s.Value
			}
		case "vcs.modified":
			info.Modified = s.Value == "true"
		}
	}
	if info.Version == "" {
		info.Version = devVersion
	}
	return info
}

func truncateCommit(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// String returns a single-line build identifier.
func String() string {
	i := Resolve()
	var b strings.Builder
	b.WriteString(i.Version)
	if i.Commit != "" {
		b.WriteString(" (" + i.Commit)
		if i.Modified {
			b.WriteString("-dirty")
		}
		b.WriteString(")")
	} else if i.Modified {
		b.WriteString(" (dirty)")
	}
	return b.String()
}

// Detailed returns a multi-line build description for `kern version`.
func Detailed() string {
	i := Resolve()
	var b strings.Builder
	fmt.Fprintf(&b, "kern      %s\n", String())
	fmt.Fprintf(&b, "go        %s\n", i.GoVer)
	if i.Date != "" {
		fmt.Fprintf(&b, "built     %s\n", i.Date)
	}
	fmt.Fprintf(&b, "platform  %s/%s\n", runtime.GOOS, runtime.GOARCH)
	return b.String()
}
