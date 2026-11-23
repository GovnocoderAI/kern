package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// TestViewFitsTerminal guards against View() emitting more lines than the
// terminal height (which makes the UI scroll) and wider than the terminal
// width (which wraps and corrupts the layout).
func TestViewFitsTerminal(t *testing.T) {
	sizes := []struct{ w, h int }{
		{80, 24}, {100, 30}, {120, 40}, {40, 16}, {200, 60},
	}
	for _, sz := range sizes {
		m := newTestModel(t)
		m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		out := m.View()
		lines := strings.Split(out, "\n")
		if len(lines) > sz.h {
			t.Errorf("%dx%d: View() emitted %d lines, want <= %d", sz.w, sz.h, len(lines), sz.h)
		}
		for i, line := range lines {
			if w := ansi.StringWidth(line); w > sz.w {
				t.Errorf("%dx%d: line %d width %d exceeds %d: %q",
					sz.w, sz.h, i, w, sz.w, ansi.Strip(line))
				break
			}
		}
	}
}

// TestViewBeforeWindowSize ensures the loading placeholder is shown rather
// than a half-built layout.
func TestViewBeforeWindowSize(t *testing.T) {
	m := newTestModel(t)
	if got := m.View(); got == "" {
		t.Fatal("View() before WindowSizeMsg returned empty string")
	}
}

// TestTabsSwitch verifies every tab renders and the tab name tracks state.
func TestTabsSwitch(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	want := []string{"chat", "files", "diff", "processes", "skills"}
	for i, name := range want {
		m.tab = i
		if got := m.tabName(); got != name {
			t.Errorf("tab %d name = %q, want %q", i, got, name)
		}
		if out := m.View(); out == "" {
			t.Errorf("tab %d rendered empty", i)
		}
	}
}
