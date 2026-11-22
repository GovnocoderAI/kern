// Package provider implements streaming chat providers.
package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

const (
	PartText       = "text"
	PartImage      = "image"
	PartToolCall   = "tool_call"
	PartToolResult = "tool_result"
	PartReasoning  = "reasoning"
)

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolResult struct {
	CallID  string `json:"call_id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

type Part struct {
	Type       string      `json:"type"`
	Text       string      `json:"text,omitempty"`
	ImageURL   string      `json:"image_url,omitempty"`
	ToolCall   *ToolCall   `json:"tool_call,omitempty"`
	ToolResult *ToolResult `json:"tool_result,omitempty"`
}

type Message struct {
	Role  Role   `json:"role"`
	Parts []Part `json:"parts"`
}

func Text(role Role, text string) Message {
	return Message{Role: role, Parts: []Part{{Type: PartText, Text: text}}}
}

func (m Message) Text() string {
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Type == PartText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type Request struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []ToolDef
	Temperature *float64
	MaxTokens   int
	Headers     map[string]string
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type EventType string

const (
	EventText      EventType = "text"
	EventReasoning EventType = "reasoning"
	EventToolCall  EventType = "tool_call"
	EventUsage     EventType = "usage"
	EventDone      EventType = "done"
	EventError     EventType = "error"
)

type Event struct {
	Type     EventType
	Text     string
	ToolCall *ToolCall
	Usage    *Usage
	Err      error
}

type ModelInfo struct {
	Provider   string `json:"provider"`
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
	Context    int    `json:"context,omitempty"`
	Output     int    `json:"output,omitempty"`
	Attachment bool   `json:"attachment,omitempty"`
}

type Provider interface {
	ID() string
	Kind() string
	Models(ctx context.Context) ([]ModelInfo, error)
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}

// HTTPClient is shared by all providers.
var HTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: time.Second,
	},
}

type sseMessage struct {
	Event string
	Data  string
}

// readSSE parses a Server-Sent Events stream.
func readSSE(r io.Reader, fn func(sseMessage) error) error {
	br := bufio.NewReaderSize(r, 128*1024)
	var event string
	var data strings.Builder
	dispatch := func() error {
		if data.Len() == 0 {
			event = ""
			return nil
		}
		msg := sseMessage{Event: event, Data: data.String()}
		event = ""
		data.Reset()
		return fn(msg)
	}
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			switch {
			case line == "":
				if e := dispatch(); e != nil {
					return e
				}
			case strings.HasPrefix(line, ":"):
				// comment / keep-alive
			case strings.HasPrefix(line, "event:"):
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				d := strings.TrimPrefix(line, "data:")
				d = strings.TrimPrefix(d, " ")
				if data.Len() > 0 {
					data.WriteString("\n")
				}
				data.WriteString(d)
			}
		}
		if err != nil {
			if e := dispatch(); e != nil {
				return e
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func send(ctx context.Context, ch chan<- Event, ev Event) bool {
	select {
	case ch <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

func joinPath(base, path string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/v1") && strings.HasPrefix(path, "/v1") {
		return base + strings.TrimPrefix(path, "/v1")
	}
	if strings.HasPrefix(path, "/") {
		return base + path
	}
	return base + "/" + path
}

func errorEvent(err error) Event {
	return Event{Type: EventError, Err: err}
}
