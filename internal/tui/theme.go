// Package tui implements the full-screen terminal interface.
package tui

import "github.com/charmbracelet/lipgloss"

// Theme is a set of lipgloss colors and styles used throughout the TUI.
// Themes are chosen at first launch via `kern init` or written in kern.json.
type Theme struct {
	Name   string
	Colors lipglossColors
}

// lipglossColors are the semantic color roles used by Kern's UI.
type lipglossColors struct {
	Amber   lipgloss.Color
	Ember   lipgloss.Color
	Green   lipgloss.Color
	Red     lipgloss.Color
	Cyan    lipgloss.Color
	Gray    lipgloss.Color
	Text    lipgloss.Color
	Surface lipgloss.Color
	Border  lipgloss.Color
}

// lipglossStyles are the semantic style roles.
type lipglossStyles struct {
	Title     lipgloss.Style
	Accent    lipgloss.Style
	Dim       lipgloss.Style
	Text      lipgloss.Style
	OK        lipgloss.Style
	Err       lipgloss.Style
	Info      lipgloss.Style
	User      lipgloss.Style
	Tool      lipgloss.Style
	Selected  lipgloss.Style
	Panel     lipgloss.Style
	Input     lipgloss.Style
	Status    lipgloss.Style
	TabActive lipgloss.Style
	Tab       lipgloss.Style
}

// AvailableThemes returns the built-in theme names.
func AvailableThemes() []string {
	return []string{"amber", "dark", "light", "blue"}
}

// Get returns the theme by name. Unknown names fall back to "amber".
func Get(name string) Theme {
	switch name {
	case "dark":
		return darkTheme
	case "light":
		return lightTheme
	case "blue":
		return blueTheme
	default:
		return amberTheme
	}
}

// amberTheme is the default: warm amber on dark gray.
var amberTheme = Theme{
	Name: "amber",
	Colors: lipglossColors{
		Amber:   lipgloss.Color("#FFB000"),
		Ember:   lipgloss.Color("#FF6B35"),
		Green:   lipgloss.Color("#4ADE80"),
		Red:     lipgloss.Color("#F87171"),
		Cyan:    lipgloss.Color("#22D3EE"),
		Gray:    lipgloss.Color("#8A8A93"),
		Text:    lipgloss.Color("#E6E6E6"),
		Surface: lipgloss.Color("#1F1F26"),
		Border:  lipgloss.Color("#3A3A44"),
	},
}

// darkTheme: monochrome, high contrast.
var darkTheme = Theme{
	Name: "dark",
	Colors: lipglossColors{
		Amber:   lipgloss.Color("#E6E6E6"),
		Ember:   lipgloss.Color("#9CA3AF"),
		Green:   lipgloss.Color("#4ADE80"),
		Red:     lipgloss.Color("#F87171"),
		Cyan:    lipgloss.Color("#22D3EE"),
		Gray:    lipgloss.Color("#4B5563"),
		Text:    lipgloss.Color("#FFFFFF"),
		Surface: lipgloss.Color("#111111"),
		Border:  lipgloss.Color("#374151"),
	},
}

// lightTheme: for light terminals.
var lightTheme = Theme{
	Name: "light",
	Colors: lipglossColors{
		Amber:   lipgloss.Color("#B45309"),
		Ember:   lipgloss.Color("#C2410C"),
		Green:   lipgloss.Color("#15803D"),
		Red:     lipgloss.Color("#B91C1C"),
		Cyan:    lipgloss.Color("#0E7490"),
		Gray:    lipgloss.Color("#6B7280"),
		Text:    lipgloss.Color("#1F2937"),
		Surface: lipgloss.Color("#F3F4F6"),
		Border:  lipgloss.Color("#D1D5DB"),
	},
}

// blueTheme: cool blue palette.
var blueTheme = Theme{
	Name: "blue",
	Colors: lipglossColors{
		Amber:   lipgloss.Color("#60A5FA"),
		Ember:   lipgloss.Color("#818CF8"),
		Green:   lipgloss.Color("#34D399"),
		Red:     lipgloss.Color("#F87171"),
		Cyan:    lipgloss.Color("#22D3EE"),
		Gray:    lipgloss.Color("#94A3B8"),
		Text:    lipgloss.Color("#F1F5F9"),
		Surface: lipgloss.Color("#0F172A"),
		Border:  lipgloss.Color("#334155"),
	},
}

// BuildStyles creates the lipgloss style set for a color palette.
func BuildStyles(c lipglossColors) lipglossStyles {
	return lipglossStyles{
		Title:     lipgloss.NewStyle().Foreground(c.Amber).Bold(true),
		Accent:    lipgloss.NewStyle().Foreground(c.Ember),
		Dim:       lipgloss.NewStyle().Foreground(c.Gray),
		Text:      lipgloss.NewStyle().Foreground(c.Text),
		OK:        lipgloss.NewStyle().Foreground(c.Green),
		Err:       lipgloss.NewStyle().Foreground(c.Red),
		Info:      lipgloss.NewStyle().Foreground(c.Cyan),
		User:      lipgloss.NewStyle().Foreground(c.Amber).Bold(true),
		Tool:      lipgloss.NewStyle().Foreground(c.Ember),
		Selected:  lipgloss.NewStyle().Foreground(c.Amber).Bold(true),
		Panel:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(c.Border),
		Input:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(c.Amber),
		Status:    lipgloss.NewStyle().Foreground(c.Gray).Background(c.Surface).Padding(0, 1),
		TabActive: lipgloss.NewStyle().Foreground(c.Amber).Bold(true).Underline(true),
		Tab:       lipgloss.NewStyle().Foreground(c.Gray),
	}
}

// initTheme loads the named theme and populates the package-level style vars.
// It is called by tui.Run() before the model starts.
func initTheme(themeName string) {
	t := Get(themeName)
	s := BuildStyles(t.Colors)
	sTitle = s.Title
	sAccent = s.Accent
	sDim = s.Dim
	sText = s.Text
	sOK = s.OK
	sErr = s.Err
	sInfo = s.Info
	sUser = s.User
	sTool = s.Tool
	sSelected = s.Selected
	sPanel = s.Panel
	sInput = s.Input
	sStatus = s.Status
	sTabActive = s.TabActive
	sTab = s.Tab
}

// Package-level style vars. Populated by initTheme.
var (
	sTitle     lipgloss.Style
	sAccent    lipgloss.Style
	sDim       lipgloss.Style
	sText      lipgloss.Style
	sOK        lipgloss.Style
	sErr       lipgloss.Style
	sInfo      lipgloss.Style
	sUser      lipgloss.Style
	sTool      lipgloss.Style
	sSelected  lipgloss.Style
	sPanel     lipgloss.Style
	sInput     lipgloss.Style
	sStatus    lipgloss.Style
	sTabActive lipgloss.Style
	sTab       lipgloss.Style
)
