// Package tui implements the full-screen terminal interface.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	appcore "github.com/GovnocoderAI/kern/internal/app"
	"github.com/GovnocoderAI/kern/internal/provider"
	"github.com/GovnocoderAI/kern/internal/session"
	"github.com/GovnocoderAI/kern/internal/tools"
	"github.com/GovnocoderAI/kern/internal/version"
)

const (
	tabChat = iota
	tabFiles
	tabDiff
	tabProcs
)

const (
	focusInput = iota
	focusMain
	focusSidebar
)

const sidebarWidth = 30

type entry struct {
	kind     string
	text     string
	name     string
	ok       bool
	output   string
	done     bool
	rendered string
	at       time.Time
}

type Model struct {
	app  *appcore.App
	obs  *Observer
	perm *permBridge

	width, height int
	focusCmd      tea.Cmd

	tab         int
	focus       int
	showSidebar bool

	input   textarea.Model
	chat    viewport.Model
	detail  viewport.Model
	spinner spinner.Model

	entries    []entry
	streaming  bool
	chatFollow bool

	running bool
	cancel  context.CancelFunc
	status  string
	lastErr string

	files       []string
	fileIdx     int
	previewPath string

	changes         []tools.Change
	changeIdx       int
	lastChangeCount int

	procs          []tools.Process
	procIdx        int
	lastProcCount  int
	lastProcOutput int

	sessions   []*session.Session
	sessionIdx int

	modal      string
	paletteIdx int
	models     []string
	modelIdx   int
	permReq    *permRequestMsg
}

// Run starts the full-screen interface for a prepared session.
func Run(opts appcore.Options) error {
	obs := NewObserver()
	bridge := newPermBridge()
	opts.Observer = obs
	opts.Prompt = bridge.Prompt
	opts.Interactive = false

	application, err := appcore.New(opts)
	if err != nil {
		return err
	}
	m := New(application, obs, bridge)
	p := tea.NewProgram(m, tea.WithAltScreen())
	obs.SetSender(p.Send)
	_, err = p.Run()
	_ = application.Save()
	return err
}

func New(app *appcore.App, obs *Observer, perm *permBridge) *Model {
	ta := textarea.New()
	ta.Placeholder = "Ask Kern…"
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.Prompt = "❯ "
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"))
	focusCmd := ta.Focus()

	chat := viewport.New(80, 20)
	detail := viewport.New(80, 20)
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = sAccent

	m := &Model{
		app:         app,
		obs:         obs,
		perm:        perm,
		focusCmd:    focusCmd,
		focus:       focusInput,
		tab:         tabChat,
		showSidebar: true,
		input:       ta,
		chat:        chat,
		detail:      detail,
		spinner:     sp,
		chatFollow:  true,
		status:      "ready",
	}
	m.layout(110, 32)
	m.seedFromHistory()
	m.loadSessions()
	m.loadFiles()
	m.loadChanges()
	m.loadProcesses()
	m.loadModels()
	m.updateChat()
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.focusCmd, waitForPermission(m.perm), tickCmd(), m.spinner.Tick)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.layout(msg.Width, msg.Height)
		m.refreshDetail()
		return m, nil
	case TickMsg:
		m.refreshDynamic()
		return m, tickCmd()
	case spinner.TickMsg:
		if !m.running {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case permRequestMsg:
		req := msg
		m.permReq = &req
		m.modal = "perm"
		return m, nil
	case TextMsg:
		m.appendText(string(msg))
		return m, nil
	case ToolStartMsg:
		m.entries = append(m.entries, entry{kind: "tool", name: msg.Name, text: msg.Summary, at: time.Now()})
		m.updateChat()
		return m, nil
	case ToolResultMsg:
		m.finishTool(msg)
		return m, nil
	case StepDoneMsg:
		m.finishAssistant()
		return m, nil
	case TurnDoneMsg:
		m.running = false
		m.cancel = nil
		if msg.Err != nil {
			m.lastErr = msg.Err.Error()
			m.entries = append(m.entries, entry{kind: "error", text: msg.Err.Error(), at: time.Now(), done: true})
			m.updateChat()
		}
		if err := m.app.Save(); err != nil {
			m.status = "session save failed: " + err.Error()
		} else {
			m.status = "ready"
		}
		m.loadSessions()
		m.loadChanges()
		m.loadProcesses()
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) layout(width, height int) {
	if width < 40 {
		width = 40
	}
	if height < 16 {
		height = 16
	}
	m.width, m.height = width, height
	contentH := m.contentHeight()
	mainW := width - 4
	if m.showSidebar {
		mainW -= sidebarWidth
	}
	if mainW < 20 {
		mainW = 20
	}
	m.chat.Width = mainW
	m.chat.Height = contentH
	m.detail.Width = mainW
	m.detail.Height = contentH
	if m.tab != tabChat {
		m.detail.Width = mainW - 30
		if m.detail.Width < 20 {
			m.detail.Width = 20
		}
	}
	m.input.SetWidth(width - 4)
	m.input.SetHeight(3)
	if m.chatFollow {
		m.chat.GotoBottom()
	}
}

func (m *Model) bodyHeight() int {
	h := m.height - 1 - 5 - 1
	if h < 6 {
		h = 6
	}
	return h
}

func (m *Model) contentHeight() int {
	h := m.bodyHeight() - 1
	if h < 4 {
		h = 4
	}
	return h
}

func (m *Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	base := lipgloss.JoinVertical(lipgloss.Left,
		m.headerView(),
		m.bodyView(),
		m.inputView(),
		m.statusView(),
	)
	if m.modal != "" {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.modalView())
	}
	return base
}

func (m *Model) headerView() string {
	left := sTitle.Render(" K E R N ") + sDim.Render(" "+version.Version)
	title := sText.Render(truncate(m.app.Session.Title, 48))
	right := sInfo.Render(m.app.ProviderID+"/"+m.app.ModelID) + sDim.Render("  "+m.app.Workdir)
	return fitLine(left+"  "+title, right, m.width)
}

func (m *Model) bodyView() string {
	sidebar := ""
	if m.showSidebar {
		sidebar = m.sidebarView()
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, m.mainView())
}

func (m *Model) sidebarView() string {
	lines := []string{sTitle.Render("SESSIONS")}
	if len(m.sessions) == 0 {
		lines = append(lines, sDim.Render("  no sessions yet"))
	}
	start, end := window(len(m.sessions), m.sessionIdx, m.contentHeight()-4)
	for i := start; i < end; i++ {
		s := m.sessions[i]
		label := truncate(s.Title, sidebarWidth-6)
		line := "  " + label
		if i == m.sessionIdx {
			line = sSelected.Render("› " + label)
		} else {
			line = sDim.Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", sDim.Render("n  new session"), sDim.Render("r  refresh"))
	box := sPanel.Width(sidebarWidth - 2).Height(m.contentHeight() - 2)
	return box.Render(strings.Join(lines, "\n"))
}

func (m *Model) mainView() string {
	tabs := m.tabsView()
	var content string
	if m.tab == tabChat {
		content = m.chat.View()
	} else {
		content = lipgloss.JoinHorizontal(lipgloss.Top, m.listView(), m.detail.View())
	}
	return lipgloss.JoinVertical(lipgloss.Left, tabs, content)
}

func (m *Model) tabsView() string {
	names := []string{"Chat", "Files", "Diff", "Processes"}
	var parts []string
	for i, name := range names {
		label := fmt.Sprintf("%d %s", i+1, name)
		if i == m.tab {
			parts = append(parts, sTabActive.Render(label))
		} else {
			parts = append(parts, sTab.Render(label))
		}
	}
	left := " " + strings.Join(parts, "   ")
	right := sDim.Render("tab focus · ctrl+p commands · ? help ")
	return fitLine(left, right, m.width)
}

func (m *Model) inputView() string {
	hint := sDim.Render(" enter send · shift+enter newline · ctrl+c quit")
	return sInput.Width(m.width - 2).Render(m.input.View() + "\n" + hint)
}

func (m *Model) statusView() string {
	spin := "●"
	if m.running {
		spin = m.spinner.View()
	}
	usage := m.app.Agent.Usage
	left := fmt.Sprintf(" %s %s  %s  in:%d out:%d  changes:%d  procs:%d",
		spin, m.app.ProviderID+"/"+m.app.ModelID, m.status,
		usage.InputTokens, usage.OutputTokens, len(m.readyChanges()), len(m.procs))
	right := fmt.Sprintf("%s %s ", m.focusName(), m.tabName())
	return sStatus.Width(m.width).Render(fitLine(left, right, m.width-2))
}

func (m *Model) focusName() string {
	switch m.focus {
	case focusMain:
		return "main"
	case focusSidebar:
		return "sidebar"
	default:
		return "input"
	}
}

func (m *Model) tabName() string {
	switch m.tab {
	case tabFiles:
		return "files"
	case tabDiff:
		return "diff"
	case tabProcs:
		return "processes"
	default:
		return "chat"
	}
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.running && m.cancel != nil {
			m.cancel()
			m.running = false
			m.status = "interrupted"
			return m, nil
		}
		return m, tea.Quit
	}
	if m.modal != "" {
		return m.handleModalKey(msg)
	}
	switch msg.String() {
	case "ctrl+p":
		m.modal = "palette"
		m.paletteIdx = 0
		return m, nil
	case "f1":
		m.modal = "help"
		return m, nil
	case "tab":
		m.focus = (m.focus + 1) % 3
		return m, m.applyFocus()
	case "shift+tab":
		m.focus = (m.focus + 2) % 3
		return m, m.applyFocus()
	case "esc":
		if m.focus == focusInput {
			m.input.Blur()
			m.focus = focusMain
		} else {
			m.focus = focusInput
			return m, m.input.Focus()
		}
		return m, nil
	}
	if m.focus == focusInput {
		if msg.String() == "enter" {
			return m, m.submit()
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	if m.focus == focusSidebar {
		return m.handleSidebarKey(msg)
	}
	return m.handleMainKey(msg)
}

func (m *Model) applyFocus() tea.Cmd {
	if m.focus == focusInput {
		return m.input.Focus()
	}
	m.input.Blur()
	return nil
}

func (m *Model) handleSidebarKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.sessionIdx < len(m.sessions)-1 {
			m.sessionIdx++
		}
	case "k", "up":
		if m.sessionIdx > 0 {
			m.sessionIdx--
		}
	case "n":
		m.newSession()
	case "r":
		m.loadSessions()
	case "enter":
		m.openSession()
		m.focus = focusMain
	}
	return m, nil
}

func (m *Model) handleMainKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "1", "2", "3", "4":
		m.tab = int(msg.String()[0] - '1')
		m.refreshDetail()
		return m, nil
	case "?":
		m.modal = "help"
		return m, nil
	case "m":
		m.openModels()
		return m, nil
	case "n":
		m.newSession()
		return m, nil
	case "r":
		m.refreshDetail()
		return m, nil
	case "q":
		return m, tea.Quit
	}
	switch m.tab {
	case tabChat:
		return m.handleChatKey(msg)
	case tabFiles:
		return m.handleFilesKey(msg)
	case tabDiff:
		return m.handleDiffKey(msg)
	case tabProcs:
		return m.handleProcsKey(msg)
	}
	return m, nil
}

func (m *Model) handleChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.chat.LineDown(1)
		m.chatFollow = false
	case "k", "up":
		m.chat.LineUp(1)
		m.chatFollow = false
	case "pagedown", "ctrl+f":
		m.chat.PageDown()
		m.chatFollow = false
	case "pageup", "ctrl+b":
		m.chat.PageUp()
		m.chatFollow = false
	case "g":
		m.chat.GotoTop()
		m.chatFollow = false
	case "G":
		m.chat.GotoBottom()
		m.chatFollow = true
	case "enter", "i":
		m.focus = focusInput
		return m, m.input.Focus()
	}
	return m, nil
}

func (m *Model) handleFilesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.fileIdx < len(m.files)-1 {
			m.fileIdx++
			m.selectFile()
		}
	case "k", "up":
		if m.fileIdx > 0 {
			m.fileIdx--
			m.selectFile()
		}
	case "g":
		m.fileIdx = 0
		m.selectFile()
	case "G":
		if len(m.files) > 0 {
			m.fileIdx = len(m.files) - 1
			m.selectFile()
		}
	case "enter":
		m.selectFile()
	case "r":
		m.loadFiles()
	}
	return m, nil
}

func (m *Model) handleDiffKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.changeIdx < len(m.changes)-1 {
			m.changeIdx++
			m.selectChange()
		}
	case "k", "up":
		if m.changeIdx > 0 {
			m.changeIdx--
			m.selectChange()
		}
	case "g":
		m.changeIdx = 0
		m.selectChange()
	case "G":
		if len(m.changes) > 0 {
			m.changeIdx = len(m.changes) - 1
			m.selectChange()
		}
	case "r":
		m.loadChanges()
	}
	return m, nil
}

func (m *Model) handleProcsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.procIdx < len(m.procs)-1 {
			m.procIdx++
			m.selectProcess()
		}
	case "k", "up":
		if m.procIdx > 0 {
			m.procIdx--
			m.selectProcess()
		}
	case "x":
		if m.procIdx < len(m.procs) && m.procs[m.procIdx].Running {
			m.app.Tools().Env().KillProcess(m.procs[m.procIdx].ID)
			m.status = "process kill requested"
		}
	case "r":
		m.loadProcesses()
	}
	return m, nil
}

func (m *Model) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.modal == "perm" {
		switch msg.String() {
		case "y", "enter":
			m.replyPermission(true, false)
			return m, waitForPermission(m.perm)
		case "a":
			m.replyPermission(true, true)
			return m, waitForPermission(m.perm)
		case "n", "esc", "q":
			m.replyPermission(false, false)
			return m, waitForPermission(m.perm)
		}
		return m, nil
	}
	switch msg.String() {
	case "esc", "q":
		m.modal = ""
		return m, nil
	case "j", "down":
		m.moveModal(1)
	case "k", "up":
		m.moveModal(-1)
	case "enter":
		return m.selectModal()
	}
	return m, nil
}

func (m *Model) moveModal(delta int) {
	switch m.modal {
	case "palette":
		m.paletteIdx = clamp(m.paletteIdx+delta, 0, len(paletteItems)-1)
	case "models":
		m.modelIdx = clamp(m.modelIdx+delta, 0, len(m.models)-1)
	}
}

func (m *Model) selectModal() (tea.Model, tea.Cmd) {
	switch m.modal {
	case "palette":
		return m.executePalette(m.paletteIdx)
	case "models":
		if m.modelIdx >= 0 && m.modelIdx < len(m.models) {
			ref := m.models[m.modelIdx]
			if err := m.app.RebuildModel(ref); err != nil {
				m.status = "model: " + err.Error()
			} else {
				m.status = "model set to " + ref
			}
			m.modal = ""
		}
		return m, nil
	}
	m.modal = ""
	return m, nil
}

func (m *Model) replyPermission(allow, always bool) {
	if m.permReq == nil {
		m.modal = ""
		return
	}
	if always && allow {
		m.app.Agent.Perms.Allow(m.permReq.Action, m.permReq.Resource)
	}
	m.permReq.Reply <- allow
	m.permReq = nil
	m.modal = ""
}

// ---- chat ----

func (m *Model) submit() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return nil
	}
	m.input.Reset()
	if strings.HasPrefix(text, "/") {
		return m.handleSlash(text)
	}
	if m.running {
		m.status = "a turn is already running"
		return nil
	}
	m.entries = append(m.entries, entry{kind: "user", text: text, at: time.Now(), done: true})
	m.chatFollow = true
	m.running = true
	m.status = "thinking…"
	m.updateChat()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go func() {
		err := m.app.Agent.Run(ctx, text)
		m.obs.TurnDone(err)
	}()
	return m.spinner.Tick
}

func (m *Model) handleSlash(line string) tea.Cmd {
	fields := strings.Fields(line)
	cmd := strings.ToLower(fields[0])
	arg := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
	switch cmd {
	case "/exit", "/quit", "/q":
		return tea.Quit
	case "/help":
		m.modal = "help"
	case "/models":
		m.openModels()
	case "/model":
		if arg == "" {
			m.openModels()
			break
		}
		if err := m.app.RebuildModel(arg); err != nil {
			m.addInfo("model: " + err.Error())
			break
		}
		m.addInfo("model set to " + m.app.ProviderID + "/" + m.app.ModelID)
	case "/clear":
		m.clearConversation()
	case "/tools":
		m.addInfo(strings.Join(m.app.Agent.Tools.Names(), ", "))
	case "/session":
		m.addInfo(fmt.Sprintf("%s  %s  %s", m.app.Session.ID, m.app.Session.Title, m.app.Session.Model))
	case "/cost":
		u := m.app.Agent.Usage
		m.addInfo(fmt.Sprintf("tokens: in %d, out %d, total %d", u.InputTokens, u.OutputTokens, u.TotalTokens))
	default:
		m.addInfo("unknown command " + cmd + "; try /help")
	}
	return nil
}

func (m *Model) addInfo(text string) {
	m.entries = append(m.entries, entry{kind: "info", text: text, at: time.Now(), done: true})
	m.updateChat()
}

func (m *Model) appendText(delta string) {
	if len(m.entries) == 0 {
		m.entries = append(m.entries, entry{kind: "assistant", at: time.Now()})
	}
	last := &m.entries[len(m.entries)-1]
	if last.kind != "assistant" || last.done {
		m.entries = append(m.entries, entry{kind: "assistant", at: time.Now()})
		last = &m.entries[len(m.entries)-1]
	}
	last.text += delta
	m.streaming = true
	m.updateChat()
}

func (m *Model) finishAssistant() {
	for i := len(m.entries) - 1; i >= 0; i-- {
		if m.entries[i].kind == "assistant" && !m.entries[i].done {
			m.entries[i].done = true
			m.entries[i].rendered = m.renderMarkdown(m.entries[i].text)
			break
		}
	}
	m.streaming = false
	m.updateChat()
}

func (m *Model) finishTool(msg ToolResultMsg) {
	for i := len(m.entries) - 1; i >= 0; i-- {
		e := &m.entries[i]
		if e.kind == "tool" && e.name == msg.Name && !e.done {
			e.done = true
			e.ok = msg.OK
			e.output = msg.Output
			break
		}
	}
	m.updateChat()
}

func (m *Model) updateChat() {
	m.chat.SetContent(m.chatContent())
	if m.chatFollow {
		m.chat.GotoBottom()
	}
	if m.tab == tabChat {
		m.layout(m.width, m.height)
	}
}

func (m *Model) chatContent() string {
	var b strings.Builder
	for i, e := range m.entries {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(m.renderEntry(e))
	}
	if b.Len() == 0 {
		return sDim.Render("  Ask Kern to inspect the repo, edit files or run commands.\n  Use ctrl+p for the command palette and 2/3/4 for files, diffs and processes.")
	}
	return b.String()
}

func (m *Model) renderEntry(e entry) string {
	switch e.kind {
	case "user":
		return sUser.Render("❯ ") + sText.Render(e.text)
	case "assistant":
		if e.rendered != "" {
			return e.rendered
		}
		return sText.Render(e.text)
	case "tool":
		if !e.done {
			return sTool.Render("▸ " + e.name + " " + e.text)
		}
		if e.ok {
			return sOK.Render("✓ "+e.name+" ") + sDim.Render(e.output)
		}
		return sErr.Render("✗ "+e.name+" ") + sDim.Render(e.output)
	case "error":
		return sErr.Render("✗ " + e.text)
	case "info":
		return sInfo.Render("• " + e.text)
	default:
		return e.text
	}
}

func (m *Model) renderMarkdown(text string) string {
	width := m.width - 40
	if m.showSidebar {
		width -= sidebarWidth
	}
	if width < 40 {
		width = 40
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return text
	}
	out, err := r.Render(text)
	if err != nil {
		return text
	}
	return strings.TrimRight(out, "\n")
}

func (m *Model) seedFromHistory() {
	for _, msg := range m.app.Agent.History {
		switch msg.Role {
		case provider.RoleUser:
			if text := strings.TrimSpace(msg.Text()); text != "" {
				m.entries = append(m.entries, entry{kind: "user", text: text, done: true})
			}
		case provider.RoleAssistant:
			if text := strings.TrimSpace(msg.Text()); text != "" {
				m.entries = append(m.entries, entry{kind: "assistant", text: text, done: true, rendered: m.renderMarkdown(text)})
			}
			for _, p := range msg.Parts {
				if p.Type == provider.PartToolCall && p.ToolCall != nil {
					m.entries = append(m.entries, entry{kind: "tool", name: p.ToolCall.Name, text: string(p.ToolCall.Arguments)})
				}
			}
		case provider.RoleTool:
			for _, p := range msg.Parts {
				if p.Type == provider.PartToolResult && p.ToolResult != nil {
					for i := len(m.entries) - 1; i >= 0; i-- {
						if m.entries[i].kind == "tool" && !m.entries[i].done {
							m.entries[i].done = true
							m.entries[i].ok = !p.ToolResult.IsError
							m.entries[i].output = firstLine(p.ToolResult.Content)
							break
						}
					}
				}
			}
		}
	}
}

// ---- commands ----

func (m *Model) newSession() {
	_ = m.app.Save()
	sess := session.New(m.app.Workdir, m.app.ProviderID+"/"+m.app.ModelID)
	m.app.Session = sess
	m.app.Agent.Reset()
	m.entries = nil
	m.status = "new session"
	m.loadSessions()
	m.updateChat()
}

func (m *Model) clearConversation() {
	m.app.Agent.Reset()
	m.app.Session.Messages = nil
	m.app.Session.Usage = provider.Usage{}
	m.entries = nil
	m.status = "conversation cleared"
	m.updateChat()
}

func (m *Model) openModels() {
	m.loadModels()
	m.modal = "models"
}

func (m *Model) loadModels() {
	m.models = m.models[:0]
	current := m.app.ProviderID + "/" + m.app.ModelID
	m.modelIdx = 0
	for _, pid := range m.app.Config.ProviderIDs() {
		p := m.app.Config.Providers[pid]
		for _, mid := range p.SortedModels() {
			ref := pid + "/" + mid
			m.models = append(m.models, ref)
			if ref == current {
				m.modelIdx = len(m.models) - 1
			}
		}
	}
}

var paletteItems = []string{
	"New session",
	"Clear conversation",
	"Switch model",
	"Refresh files",
	"Toggle sidebar",
	"Show help",
	"Quit",
}

func (m *Model) executePalette(idx int) (tea.Model, tea.Cmd) {
	m.modal = ""
	switch idx {
	case 0:
		m.newSession()
	case 1:
		m.clearConversation()
	case 2:
		m.openModels()
	case 3:
		m.tab = tabFiles
		m.loadFiles()
		m.refreshDetail()
	case 4:
		m.showSidebar = !m.showSidebar
		m.layout(m.width, m.height)
	case 5:
		m.modal = "help"
	case 6:
		return m, tea.Quit
	}
	return m, nil
}

// ---- helpers ----

func (m *Model) modalView() string {
	var title, body string
	width := 64
	switch m.modal {
	case "help":
		title = "Help"
		body = helpBody
		width = 72
	case "palette":
		title = "Command palette"
		lines := make([]string, 0, len(paletteItems))
		for i, item := range paletteItems {
			if i == m.paletteIdx {
				lines = append(lines, sSelected.Render("› "+item))
			} else {
				lines = append(lines, sText.Render("  "+item))
			}
		}
		body = strings.Join(lines, "\n")
	case "models":
		title = "Models"
		lines := make([]string, 0, len(m.models))
		start, end := window(len(m.models), m.modelIdx, 16)
		for i := start; i < end; i++ {
			if i == m.modelIdx {
				lines = append(lines, sSelected.Render("› "+m.models[i]))
			} else {
				lines = append(lines, sDim.Render("  "+m.models[i]))
			}
		}
		body = strings.Join(lines, "\n")
	case "perm":
		title = "Permission required"
		if m.permReq != nil {
			body = sText.Render(m.permReq.Action) + "\n" + sAccent.Render(m.permReq.Resource) + "\n\n" +
				sDim.Render("y allow · a always allow · n deny · esc deny")
		}
	}
	content := sTitle.Render(title) + "\n\n" + body
	box := sPanel.Width(width-2).Padding(1, 2).Render(content)
	return box
}

const helpBody = `Navigation
  tab / shift+tab   cycle focus: input → main → sidebar
  1 2 3 4           Chat · Files · Diff · Processes
  j / k, ↑ / ↓      move or scroll
  g / G             top / bottom
  enter             send (input) or open (files)
  esc               leave input / close dialog
  ctrl+p            command palette
  m                 model picker
  n                 new session
  r                 refresh current panel
  x                 kill selected process
  ctrl+c            interrupt, then quit

Files
  The Files tab lists workspace files. Enter previews a file.
  The Diff tab lists every file Kern wrote or edited.
  The Processes tab lists shell commands, exit codes and output.`
