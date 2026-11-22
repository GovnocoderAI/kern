package tui

import (
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/GovnocoderAI/kern/internal/provider"
)

type TextMsg string

type ToolStartMsg struct {
	Name    string
	Summary string
}

type ToolResultMsg struct {
	Name   string
	OK     bool
	Output string
}

type UsageMsg provider.Usage

type StepDoneMsg struct{}

type TurnDoneMsg struct {
	Err error
}

type TickMsg struct{}

type permRequestMsg struct {
	Action   string
	Resource string
	Reply    chan bool
}

// Observer bridges agent events into the Bubble Tea program.
type Observer struct {
	mu   sync.RWMutex
	send func(tea.Msg)
}

func NewObserver() *Observer { return &Observer{} }

func (o *Observer) SetSender(send func(tea.Msg)) {
	o.mu.Lock()
	o.send = send
	o.mu.Unlock()
}

func (o *Observer) emit(msg tea.Msg) {
	o.mu.RLock()
	send := o.send
	o.mu.RUnlock()
	if send != nil {
		send(msg)
	}
}

func (o *Observer) OnText(delta string)                  { o.emit(TextMsg(delta)) }
func (o *Observer) OnToolStart(name, summary string)     { o.emit(ToolStartMsg{Name: name, Summary: summary}) }
func (o *Observer) OnToolResult(name string, ok bool, output string) {
	o.emit(ToolResultMsg{Name: name, OK: ok, Output: output})
}
func (o *Observer) OnUsage(usage provider.Usage) { o.emit(UsageMsg(usage)) }
func (o *Observer) OnStepDone()                  { o.emit(StepDoneMsg{}) }
func (o *Observer) TurnDone(err error)           { o.emit(TurnDoneMsg{Err: err}) }

type permBridge struct {
	req chan permRequestMsg
}

func newPermBridge() *permBridge {
	return &permBridge{req: make(chan permRequestMsg)}
}

func (b *permBridge) Prompt(action, resource string) (bool, error) {
	reply := make(chan bool, 1)
	b.req <- permRequestMsg{Action: action, Resource: resource, Reply: reply}
	return <-reply, nil
}

func waitForPermission(b *permBridge) tea.Cmd {
	return func() tea.Msg { return <-b.req }
}

func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return TickMsg{} })
}
