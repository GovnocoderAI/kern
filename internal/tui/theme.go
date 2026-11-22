package tui

import "github.com/charmbracelet/lipgloss"

var (
	cAmber   = lipgloss.Color("#FFB000")
	cEmber   = lipgloss.Color("#FF6B35")
	cGreen   = lipgloss.Color("#4ADE80")
	cRed     = lipgloss.Color("#F87171")
	cCyan    = lipgloss.Color("#22D3EE")
	cGray    = lipgloss.Color("#8A8A93")
	cText    = lipgloss.Color("#E6E6E6")
	cSurface = lipgloss.Color("#1F1F26")
	cBorder  = lipgloss.Color("#3A3A44")

	sTitle     = lipgloss.NewStyle().Foreground(cAmber).Bold(true)
	sAccent    = lipgloss.NewStyle().Foreground(cEmber)
	sDim       = lipgloss.NewStyle().Foreground(cGray)
	sText      = lipgloss.NewStyle().Foreground(cText)
	sOK        = lipgloss.NewStyle().Foreground(cGreen)
	sErr       = lipgloss.NewStyle().Foreground(cRed)
	sInfo      = lipgloss.NewStyle().Foreground(cCyan)
	sUser      = lipgloss.NewStyle().Foreground(cAmber).Bold(true)
	sTool      = lipgloss.NewStyle().Foreground(cEmber)
	sSelected  = lipgloss.NewStyle().Foreground(cAmber).Bold(true)
	sPanel     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBorder)
	sInput     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAmber)
	sStatus    = lipgloss.NewStyle().Foreground(cGray).Background(cSurface).Padding(0, 1)
	sTabActive = lipgloss.NewStyle().Foreground(cAmber).Bold(true).Underline(true)
	sTab       = lipgloss.NewStyle().Foreground(cGray)
)
