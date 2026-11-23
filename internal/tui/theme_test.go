package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestInitThemePopulatesStyles guards against the package-level style vars
// staying zero-valued, which renders as invisible/unstyled output.
func TestInitThemePopulatesStyles(t *testing.T) {
	for _, name := range AvailableThemes() {
		initTheme(name)
		if sTitle.GetForeground() == (lipgloss.NoColor{}) {
			t.Errorf("theme %q: title style has no foreground", name)
		}
		if sDim.GetForeground() == (lipgloss.NoColor{}) {
			t.Errorf("theme %q: dim style has no foreground", name)
		}
		if sErr.GetForeground() == (lipgloss.NoColor{}) {
			t.Errorf("theme %q: error style has no foreground", name)
		}
		// Rendering must produce non-empty output.
		if sTitle.Render("KERN") == "" {
			t.Errorf("theme %q: title renders empty", name)
		}
	}
}

// TestThemeNames verifies the advertised themes resolve to themselves.
func TestThemeNames(t *testing.T) {
	for _, name := range AvailableThemes() {
		if got := Get(name).Name; got != name {
			t.Errorf("Get(%q).Name = %q", name, got)
		}
	}
}

// TestUnknownThemeFallsBack verifies the amber default.
func TestUnknownThemeFallsBack(t *testing.T) {
	if got := Get("does-not-exist").Name; got != "amber" {
		t.Fatalf("fallback = %q, want amber", got)
	}
}

// TestBuildStylesDistinct ensures each theme actually differs in color.
func TestBuildStylesDistinct(t *testing.T) {
	amber := BuildStyles(Get("amber").Colors)
	light := BuildStyles(Get("light").Colors)
	if amber.Title.GetForeground() == light.Title.GetForeground() {
		t.Fatal("amber and light themes share the same title color")
	}
}
