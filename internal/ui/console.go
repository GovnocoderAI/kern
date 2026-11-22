// Package ui provides the terminal presentation layer.
package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

var (
	amber = lipgloss.Color("#FFB000")
	ember = lipgloss.Color("#FF6B35")
	green = lipgloss.Color("#4ADE80")
	red   = lipgloss.Color("#F87171")
	cyan  = lipgloss.Color("#22D3EE")
	gray  = lipgloss.Color("#8A8A93")
	text  = lipgloss.Color("#E6E6E6")

	styleBrand  = lipgloss.NewStyle().Foreground(amber).Bold(true)
	styleAccent = lipgloss.NewStyle().Foreground(ember)
	styleDim    = lipgloss.NewStyle().Foreground(gray)
	styleOK     = lipgloss.NewStyle().Foreground(green)
	styleErr    = lipgloss.NewStyle().Foreground(red)
	styleInfo   = lipgloss.NewStyle().Foreground(cyan)
	styleText   = lipgloss.NewStyle().Foreground(text)
	stylePrompt = lipgloss.NewStyle().Foreground(amber).Bold(true)
)

type Console struct {
	Out    io.Writer
	ErrOut io.Writer
	Color  bool
	reader *bufio.Reader
}

func New() *Console {
	out := os.Stdout
	color := isatty.IsTerminal(out.Fd()) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	return &Console{
		Out:    out,
		ErrOut: os.Stderr,
		Color:  color,
		reader: bufio.NewReader(os.Stdin),
	}
}

func (c *Console) paint(style lipgloss.Style, s string) string {
	if !c.Color {
		return s
	}
	return style.Render(s)
}

func (c *Console) Banner(version, model, workdir string) {
	logo := `  ▄▄
 █  █  K E R N
 █▄▄█  the core of your terminal`
	fmt.Fprintln(c.Out, c.paint(styleBrand, logo))
	fmt.Fprintln(c.Out, c.paint(styleDim, fmt.Sprintf("  %s  ·  %s", version, model)))
	fmt.Fprintln(c.Out, c.paint(styleDim, fmt.Sprintf("  %s", workdir)))
	fmt.Fprintln(c.Out)
}

func (c *Console) AssistantDelta(s string) {
	fmt.Fprint(c.Out, s)
}

func (c *Console) AssistantEnd() {
	fmt.Fprintln(c.Out)
}

func (c *Console) Info(format string, args ...any) {
	fmt.Fprintln(c.Out, c.paint(styleInfo, "• "+fmt.Sprintf(format, args...)))
}

func (c *Console) Success(format string, args ...any) {
	fmt.Fprintln(c.Out, c.paint(styleOK, "✓ "+fmt.Sprintf(format, args...)))
}

func (c *Console) Warn(format string, args ...any) {
	fmt.Fprintln(c.ErrOut, c.paint(styleAccent, "! "+fmt.Sprintf(format, args...)))
}

func (c *Console) Error(format string, args ...any) {
	fmt.Fprintln(c.ErrOut, c.paint(styleErr, "✗ "+fmt.Sprintf(format, args...)))
}

func (c *Console) Dim(format string, args ...any) {
	fmt.Fprintln(c.Out, c.paint(styleDim, fmt.Sprintf(format, args...)))
}

func (c *Console) ToolStart(name, summary string) {
	line := "▸ " + name
	if summary != "" {
		line += " " + summary
	}
	fmt.Fprintln(c.Out, c.paint(styleAccent, line))
}

func (c *Console) ToolResult(name string, ok bool, summary string) {
	symbol := "✓"
	style := styleOK
	if !ok {
		symbol = "✗"
		style = styleErr
	}
	line := symbol + " " + name
	if summary != "" {
		line += " " + summary
	}
	fmt.Fprintln(c.Out, c.paint(style, line))
}

func (c *Console) Raw(s string) {
	fmt.Fprint(c.Out, s)
}

// Prompt reads a line from stdin with the given prompt prefix.
func (c *Console) Prompt(prefix string) (string, error) {
	fmt.Fprint(c.Out, c.paint(stylePrompt, prefix))
	line, err := c.reader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Ask requests confirmation from the user.
func (c *Console) Ask(question string) (bool, error) {
	fmt.Fprintln(c.Out)
	fmt.Fprintln(c.Out, c.paint(styleAccent, question))
	fmt.Fprint(c.Out, c.paint(styleDim, "  allow? [y]es / [N]o / [a]lways: "))
	line, err := c.reader.ReadString('\n')
	if err != nil && line == "" {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "a", "always":
		fmt.Fprintln(c.Out)
		return true, nil
	default:
		fmt.Fprintln(c.Out)
		return false, nil
	}
}

func (c *Console) Println(s string) {
	fmt.Fprintln(c.Out, s)
}

// Style helpers for other packages that want consistent colors.
func Brand(s string) string     { return styleBrand.Render(s) }
func Accent(s string) string    { return styleAccent.Render(s) }
func Dim(s string) string       { return styleDim.Render(s) }
func OK(s string) string        { return styleOK.Render(s) }
func Err(s string) string       { return styleErr.Render(s) }
func InfoStyle(s string) string { return styleInfo.Render(s) }
