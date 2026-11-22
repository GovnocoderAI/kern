package tui

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/pmezard/go-difflib/difflib"

	"github.com/GovnocoderAI/kern/internal/tools"
)

var tuiSkipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true,
	"vendor": true, "dist": true, "bin": true, "build": true, "target": true,
	".venv": true, "venv": true, "__pycache__": true, ".next": true,
	".cache": true, "coverage": true, ".idea": true, ".vscode": true,
}

func (m *Model) loadFiles() {
	root := m.app.Workdir
	var files []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && tuiSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		if len(files) >= 800 {
			return filepath.SkipAll
		}
		return nil
	})
	sort.Strings(files)
	m.files = files
	m.fileIdx = clamp(m.fileIdx, 0, max(0, len(files)-1))
	if m.tab == tabFiles {
		m.selectFile()
	}
}

func (m *Model) selectFile() {
	if len(m.files) == 0 {
		m.detail.SetContent(sDim.Render("no files in the workspace"))
		return
	}
	m.fileIdx = clamp(m.fileIdx, 0, len(m.files)-1)
	rel := m.files[m.fileIdx]
	m.previewPath = rel
	path := filepath.Join(m.app.Workdir, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		m.detail.SetContent(sErr.Render(err.Error()))
		return
	}
	if len(data) > 400*1024 {
		data = data[:400*1024]
	}
	if bytes.IndexByte(data, 0) >= 0 {
		m.detail.SetContent(sDim.Render(rel + " — binary file"))
		return
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	var b strings.Builder
	b.WriteString(sTitle.Render(rel))
	b.WriteString("\n\n")
	for i, line := range lines {
		b.WriteString(fmt.Sprintf("%4d  %s\n", i+1, line))
	}
	m.detail.SetContent(b.String())
	m.detail.GotoTop()
}

func (m *Model) loadChanges() {
	m.changes = m.app.Tools().Env().ChangesSnapshot()
	m.lastChangeCount = len(m.changes)
	m.changeIdx = clamp(m.changeIdx, 0, max(0, len(m.changes)-1))
	if m.tab == tabDiff {
		m.selectChange()
	}
}

func (m *Model) selectChange() {
	if len(m.changes) == 0 {
		m.detail.SetContent(sDim.Render("no file changes yet.\n\nKern records every write and edit here."))
		return
	}
	m.changeIdx = clamp(m.changeIdx, 0, len(m.changes)-1)
	c := m.changes[m.changeIdx]
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(c.Before),
		B:        difflib.SplitLines(c.After),
		FromFile: c.Path + " (before)",
		ToFile:   c.Path + " (after)",
		Context:  3,
	})
	if err != nil {
		m.detail.SetContent(sErr.Render(err.Error()))
		return
	}
	if strings.TrimSpace(diff) == "" {
		diff = "no textual changes"
	}
	m.detail.SetContent(colorizeDiff(diff))
	m.detail.GotoTop()
}

func (m *Model) loadProcesses() {
	m.procs = m.app.Tools().Env().ProcessesSnapshot()
	m.lastProcCount = len(m.procs)
	m.procIdx = clamp(m.procIdx, 0, max(0, len(m.procs)-1))
	if m.tab == tabProcs {
		m.selectProcess()
	}
}

func (m *Model) selectProcess() {
	if len(m.procs) == 0 {
		m.detail.SetContent(sDim.Render("no processes yet.\n\nShell commands run by the agent appear here with exit codes and output."))
		return
	}
	m.procIdx = clamp(m.procIdx, 0, len(m.procs)-1)
	p := m.procs[m.procIdx]
	m.lastProcOutput = len(p.Output)
	status := "running"
	if !p.Running {
		status = fmt.Sprintf("exit %d", p.ExitCode)
	}
	dur := ""
	if !p.Ended.IsZero() {
		dur = " · " + p.Ended.Sub(p.Started).Round(time.Millisecond).String()
	}
	var b strings.Builder
	b.WriteString(sTitle.Render(truncate(p.Command, 80)))
	b.WriteString("\n")
	b.WriteString(sDim.Render("cwd: " + p.Cwd + " · " + status + dur))
	b.WriteString("\n\n")
	b.WriteString(p.Output)
	m.detail.SetContent(b.String())
	m.detail.GotoTop()
}

func (m *Model) refreshDetail() {
	switch m.tab {
	case tabFiles:
		m.selectFile()
	case tabDiff:
		m.loadChanges()
		m.selectChange()
	case tabProcs:
		m.loadProcesses()
		m.selectProcess()
	default:
		m.updateChat()
	}
}

func (m *Model) refreshDynamic() {
	if m.tab == tabDiff {
		changes := m.app.Tools().Env().ChangesSnapshot()
		if len(changes) != m.lastChangeCount {
			m.loadChanges()
			m.selectChange()
		}
	}
	if m.tab == tabProcs {
		procs := m.app.Tools().Env().ProcessesSnapshot()
		changed := len(procs) != m.lastProcCount
		if !changed && m.procIdx < len(procs) {
			changed = len(procs[m.procIdx].Output) != m.lastProcOutput
		}
		if changed {
			offset := m.detail.YOffset
			m.loadProcesses()
			m.selectProcess()
			m.detail.SetYOffset(offset)
		}
	}
}

func (m *Model) readyChanges() []tools.Change { return m.changes }

func (m *Model) listView() string {
	title := "FILES"
	switch m.tab {
	case tabDiff:
		title = "CHANGES"
	case tabProcs:
		title = "PROCESSES"
	}
	lines := []string{sTitle.Render(title)}
	h := m.contentHeight() - 4
	if h < 3 {
		h = 3
	}
	switch m.tab {
	case tabFiles:
		start, end := window(len(m.files), m.fileIdx, h)
		if len(m.files) == 0 {
			lines = append(lines, sDim.Render("  empty"))
		}
		for i := start; i < end; i++ {
			lines = append(lines, selectableLine(m.files[i], i == m.fileIdx, 27))
		}
	case tabDiff:
		start, end := window(len(m.changes), m.changeIdx, h)
		if len(m.changes) == 0 {
			lines = append(lines, sDim.Render("  none"))
		}
		for i := start; i < end; i++ {
			lines = append(lines, selectableLine(m.changes[i].Path, i == m.changeIdx, 27))
		}
	case tabProcs:
		start, end := window(len(m.procs), m.procIdx, h)
		if len(m.procs) == 0 {
			lines = append(lines, sDim.Render("  none"))
		}
		for i := start; i < end; i++ {
			p := m.procs[i]
			icon := "✓"
			if p.Running {
				icon = "●"
			} else if p.ExitCode != 0 {
				icon = "✗"
			}
			lines = append(lines, selectableLine(icon+" "+p.Command, i == m.procIdx, 27))
		}
	}
	lines = append(lines, "", sDim.Render("j/k · enter · r"))
	return lipgloss.NewStyle().Width(29).Height(m.contentHeight()).Render(strings.Join(lines, "\n"))
}

func selectableLine(text string, selected bool, width int) string {
	text = truncate(text, width)
	if selected {
		return sSelected.Render("› " + text)
	}
	return sDim.Render("  " + text)
}

func colorizeDiff(diff string) string {
	var b strings.Builder
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			b.WriteString(sAccent.Render(line))
		case strings.HasPrefix(line, "+"):
			b.WriteString(sOK.Render(line))
		case strings.HasPrefix(line, "-"):
			b.WriteString(sErr.Render(line))
		case strings.HasPrefix(line, "@@"):
			b.WriteString(sInfo.Render(line))
		default:
			b.WriteString(sDim.Render(line))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return runewidth.Truncate(s, width, "…")
}

func fitLine(left, right string, width int) string {
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	if lw+rw+1 > width && width > rw+2 {
		left = truncate(left, width-rw-1)
		lw = lipgloss.Width(left)
	}
	gap := width - lw - rw
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func window(n, idx, h int) (int, int) {
	if n <= 0 {
		return 0, 0
	}
	if h <= 0 {
		h = 1
	}
	if n <= h {
		return 0, n
	}
	start := idx - h/2
	start = clamp(start, 0, n-h)
	return start, start + h
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
