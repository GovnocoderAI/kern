package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestDigitsSwitchTabsFromEmptyInput verifies that 1..5 switch panels while
// the prompt is empty, and are typed normally once text is present.
func TestDigitsSwitchTabsFromEmptyInput(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.focus = focusInput

	for i := 1; i <= 5; i++ {
		key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune('0' + i)}}
		m.Update(key)
		if m.tab != i-1 {
			t.Fatalf("pressing %d: tab = %d, want %d", i, m.tab, i-1)
		}
		if m.input.Value() != "" {
			t.Fatalf("pressing %d leaked into the prompt: %q", i, m.input.Value())
		}
	}

	// With text present digits must be typed, not interpreted.
	m.input.SetValue("hello")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if m.input.Value() != "hello3" {
		t.Fatalf("digit not typed into prompt: %q", m.input.Value())
	}
}

// TestTabStripNotTruncated ensures all five tab labels fit at 100 columns.
func TestTabStripNotTruncated(t *testing.T) {
	m := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	got := m.tabsView()
	for _, want := range []string{"Chat", "Files", "Diff", "Procs", "Skills"} {
		if !strings.Contains(got, want) {
			t.Errorf("tab strip missing %q: %q", want, got)
		}
	}
}
