package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// Anthropic implements the Anthropic Messages API.
type Anthropic struct {
	ProviderID string
	BaseURL    string
	APIKey     string
	Headers    map[string]string
	Client     *http.Client
}

func (a *Anthropic) ID() string   { return a.ProviderID }
func (a *Anthropic) Kind() string { return "anthropic" }

func (a *Anthropic) client() *http.Client {
	if a.Client != nil {
		return a.Client
	}
	return HTTPClient
}

func (a *Anthropic) applyHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("anthropic-version", "2023-06-01")
	if a.APIKey != "" {
		req.Header.Set("x-api-key", a.APIKey)
	}
	for k, v := range a.Headers {
		req.Header.Set(k, v)
	}
}

func (a *Anthropic) Models(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, joinPath(a.BaseURL, "/v1/models"), nil)
	if err != nil {
		return nil, err
	}
	a.applyHeaders(req)
	req.Header.Set("Accept", "application/json")
	resp, err := a.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &httpError{ProviderID: a.ProviderID, Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	var payload struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(payload.Data))
	for _, m := range payload.Data {
		name := m.DisplayName
		if name == "" {
			name = m.ID
		}
		out = append(out, ModelInfo{Provider: a.ProviderID, ID: m.ID, Name: name, Attachment: true})
	}
	return out, nil
}

func (a *Anthropic) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	messages, err := toAnthropicMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"model":      req.Model,
		"messages":   messages,
		"max_tokens": req.MaxTokens,
		"stream":     true,
	}
	if body["max_tokens"] == 0 {
		body["max_tokens"] = 8192
	}
	if req.System != "" {
		body["system"] = req.System
	}
	if len(req.Tools) > 0 {
		body["tools"] = toAnthropicTools(req.Tools)
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, joinPath(a.BaseURL, "/v1/messages"), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	a.applyHeaders(httpReq)
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	ch := make(chan Event, 32)
	go func() {
		defer close(ch)
		resp, err := a.client().Do(httpReq)
		if err != nil {
			var he *httpError
			cause := err
			if errors.As(err, &he) {
				cause = &httpError{ProviderID: a.ProviderID, Status: he.Status, Body: he.Body, Err: err}
			}
			send(ctx, ch, errorEvent(cause))
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
			send(ctx, ch, errorEvent(&httpError{ProviderID: a.ProviderID, Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}))
			return
		}

		type acc struct {
			call ToolCall
			args strings.Builder
		}
		blocks := map[int]*acc{}
		usage := Usage{}
		done := false

		parseErr := readSSE(resp.Body, func(msg sseMessage) error {
			if done {
				return nil
			}
			var ev struct {
				Type    string `json:"type"`
				Index   int    `json:"index"`
				Message struct {
					Usage struct {
						InputTokens  int `json:"input_tokens"`
						OutputTokens int `json:"output_tokens"`
					} `json:"usage"`
				} `json:"message"`
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(msg.Data), &ev); err != nil {
				return nil
			}
			switch ev.Type {
			case "message_start":
				usage.InputTokens = ev.Message.Usage.InputTokens
				usage.OutputTokens = ev.Message.Usage.OutputTokens
			case "content_block_start":
				if ev.ContentBlock.Type == "tool_use" {
					blocks[ev.Index] = &acc{call: ToolCall{ID: ev.ContentBlock.ID, Name: ev.ContentBlock.Name}}
				}
			case "content_block_delta":
				switch ev.Delta.Type {
				case "text_delta":
					if ev.Delta.Text != "" {
						send(ctx, ch, Event{Type: EventText, Text: ev.Delta.Text})
					}
				case "input_json_delta":
					if b := blocks[ev.Index]; b != nil {
						b.args.WriteString(ev.Delta.PartialJSON)
					}
				}
			case "content_block_stop":
				if b := blocks[ev.Index]; b != nil {
					args := strings.TrimSpace(b.args.String())
					if args == "" {
						args = "{}"
					}
					b.call.Arguments = json.RawMessage(args)
					call := b.call
					send(ctx, ch, Event{Type: EventToolCall, ToolCall: &call})
					delete(blocks, ev.Index)
				}
			case "message_delta":
				if ev.Usage.OutputTokens > 0 {
					usage.OutputTokens = ev.Usage.OutputTokens
				}
			case "message_stop":
				done = true
			case "error":
				msgText := ev.Error.Message
				if msgText == "" {
					msgText = ev.Error.Type
				}
				send(ctx, ch, errorEvent(fmt.Errorf("%s: %s", a.ProviderID, msgText)))
				done = true
			}
			return nil
		})
		if parseErr != nil && ctx.Err() == nil {
			send(ctx, ch, errorEvent(parseErr))
		}

		indexes := make([]int, 0, len(blocks))
		for idx := range blocks {
			indexes = append(indexes, idx)
		}
		sort.Ints(indexes)
		for _, idx := range indexes {
			b := blocks[idx]
			args := strings.TrimSpace(b.args.String())
			if args == "" {
				args = "{}"
			}
			b.call.Arguments = json.RawMessage(args)
			call := b.call
			send(ctx, ch, Event{Type: EventToolCall, ToolCall: &call})
		}
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
		send(ctx, ch, Event{Type: EventUsage, Usage: &usage})
		send(ctx, ch, Event{Type: EventDone})
	}()
	return ch, nil
}

func toAnthropicTools(defs []ToolDef) []any {
	out := make([]any, 0, len(defs))
	for _, d := range defs {
		schema := any(map[string]any{"type": "object", "properties": map[string]any{}})
		if len(d.Parameters) > 0 {
			var parsed any
			if err := json.Unmarshal(d.Parameters, &parsed); err == nil && parsed != nil {
				schema = parsed
			}
		}
		out = append(out, map[string]any{
			"name":         d.Name,
			"description":  d.Description,
			"input_schema": schema,
		})
	}
	return out
}

func toAnthropicMessages(msgs []Message) ([]any, error) {
	out := make([]any, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case RoleAssistant:
			blocks := []any{}
			for _, p := range m.Parts {
				switch p.Type {
				case PartText:
					if p.Text != "" {
						blocks = append(blocks, map[string]any{"type": "text", "text": p.Text})
					}
				case PartToolCall:
					if p.ToolCall == nil {
						continue
					}
					var input any = map[string]any{}
					if len(p.ToolCall.Arguments) > 0 {
						if err := json.Unmarshal(p.ToolCall.Arguments, &input); err != nil {
							input = map[string]any{}
						}
					}
					blocks = append(blocks, map[string]any{
						"type":  "tool_use",
						"id":    p.ToolCall.ID,
						"name":  p.ToolCall.Name,
						"input": input,
					})
				}
			}
			if len(blocks) == 0 {
				continue
			}
			out = append(out, map[string]any{"role": "assistant", "content": blocks})
		case RoleTool:
			blocks := []any{}
			for _, p := range m.Parts {
				if p.Type != PartToolResult || p.ToolResult == nil {
					continue
				}
				blocks = append(blocks, map[string]any{
					"type":        "tool_result",
					"tool_use_id": p.ToolResult.CallID,
					"content":     p.ToolResult.Content,
					"is_error":    p.ToolResult.IsError,
				})
			}
			if len(blocks) == 0 {
				continue
			}
			out = append(out, map[string]any{"role": "user", "content": blocks})
		default:
			blocks := []any{}
			for _, p := range m.Parts {
				switch p.Type {
				case PartText:
					if p.Text != "" {
						blocks = append(blocks, map[string]any{"type": "text", "text": p.Text})
					}
				case PartImage:
					if media, data, ok := parseDataURL(p.ImageURL); ok {
						blocks = append(blocks, map[string]any{
							"type": "image",
							"source": map[string]any{
								"type":       "base64",
								"media_type": media,
								"data":       data,
							},
						})
					}
				}
			}
			if len(blocks) == 0 {
				continue
			}
			out = append(out, map[string]any{"role": "user", "content": blocks})
		}
	}
	return out, nil
}

func parseDataURL(url string) (media, data string, ok bool) {
	if !strings.HasPrefix(url, "data:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(url, "data:")
	comma := strings.Index(rest, ",")
	if comma < 0 {
		return "", "", false
	}
	meta := rest[:comma]
	data = rest[comma+1:]
	if !strings.HasSuffix(meta, ";base64") {
		return "", "", false
	}
	media = strings.TrimSuffix(meta, ";base64")
	if media == "" {
		return "", "", false
	}
	return media, data, true
}
