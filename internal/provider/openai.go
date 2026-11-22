package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// OpenAI implements OpenAI-compatible chat completions, which covers OpenAI,
// OpenRouter, Ollama, LM Studio, plusvibe, yesapi and most proxies.
type OpenAI struct {
	ProviderID string
	BaseURL    string
	APIKey     string
	Headers    map[string]string
	Client     *http.Client
}

func (o *OpenAI) ID() string   { return o.ProviderID }
func (o *OpenAI) Kind() string { return "openai" }

func (o *OpenAI) client() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return HTTPClient
}

func (o *OpenAI) applyHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}
	for k, v := range o.Headers {
		req.Header.Set(k, v)
	}
}

func (o *OpenAI) Models(ctx context.Context) ([]ModelInfo, error) {
	url := joinPath(o.BaseURL, "/models")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	o.applyHeaders(req)
	req.Header.Set("Accept", "application/json")
	resp, err := o.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%s: HTTP %d: %s", o.ProviderID, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(payload.Data))
	for _, m := range payload.Data {
		out = append(out, ModelInfo{Provider: o.ProviderID, ID: m.ID, Name: m.ID})
	}
	return out, nil
}

func (o *OpenAI) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	body := map[string]any{
		"model":          req.Model,
		"messages":       toOpenAIMessages(req.System, req.Messages),
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if len(req.Tools) > 0 {
		body["tools"] = toOpenAITools(req.Tools)
		body["tool_choice"] = "auto"
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, joinPath(o.BaseURL, "/chat/completions"), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	o.applyHeaders(httpReq)
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	ch := make(chan Event, 32)
	go func() {
		defer close(ch)
		resp, err := o.client().Do(httpReq)
		if err != nil {
			send(ctx, ch, errorEvent(err))
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
			send(ctx, ch, errorEvent(fmt.Errorf("%s: HTTP %d: %s", o.ProviderID, resp.StatusCode, strings.TrimSpace(string(b)))))
			return
		}

		type acc struct {
			call ToolCall
			args strings.Builder
		}
		calls := map[int]*acc{}
		var usage *Usage
		done := false

		parseErr := readSSE(resp.Body, func(msg sseMessage) error {
			if done {
				return nil
			}
			if strings.TrimSpace(msg.Data) == "[DONE]" {
				done = true
				return nil
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content          string `json:"content"`
						ReasoningContent string `json:"reasoning_content"`
						ToolCalls        []struct {
							Index    int    `json:"index"`
							ID       string `json:"id"`
							Type     string `json:"type"`
							Function struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
				Usage *struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
					TotalTokens      int `json:"total_tokens"`
				} `json:"usage"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(msg.Data), &chunk); err != nil {
				return nil
			}
			if chunk.Error != nil && chunk.Error.Message != "" {
				send(ctx, ch, errorEvent(fmt.Errorf("%s: %s", o.ProviderID, chunk.Error.Message)))
				done = true
				return nil
			}
			if chunk.Usage != nil {
				u := Usage{
					InputTokens:  chunk.Usage.PromptTokens,
					OutputTokens: chunk.Usage.CompletionTokens,
					TotalTokens:  chunk.Usage.TotalTokens,
				}
				if u.TotalTokens == 0 {
					u.TotalTokens = u.InputTokens + u.OutputTokens
				}
				usage = &u
			}
			for _, choice := range chunk.Choices {
				if choice.Delta.Content != "" {
					send(ctx, ch, Event{Type: EventText, Text: choice.Delta.Content})
				}
				if choice.Delta.ReasoningContent != "" {
					send(ctx, ch, Event{Type: EventReasoning, Text: choice.Delta.ReasoningContent})
				}
				for _, tc := range choice.Delta.ToolCalls {
					a := calls[tc.Index]
					if a == nil {
						a = &acc{}
						calls[tc.Index] = a
					}
					if tc.ID != "" {
						a.call.ID = tc.ID
					}
					if tc.Function.Name != "" {
						a.call.Name += tc.Function.Name
					}
					a.args.WriteString(tc.Function.Arguments)
				}
			}
			return nil
		})
		if parseErr != nil && ctx.Err() == nil {
			send(ctx, ch, errorEvent(parseErr))
		}

		indexes := make([]int, 0, len(calls))
		for idx := range calls {
			indexes = append(indexes, idx)
		}
		sort.Ints(indexes)
		for _, idx := range indexes {
			a := calls[idx]
			if a.call.Name == "" {
				continue
			}
			args := strings.TrimSpace(a.args.String())
			if args == "" {
				args = "{}"
			}
			a.call.Arguments = json.RawMessage(args)
			call := a.call
			send(ctx, ch, Event{Type: EventToolCall, ToolCall: &call})
		}
		if usage != nil {
			send(ctx, ch, Event{Type: EventUsage, Usage: usage})
		}
		send(ctx, ch, Event{Type: EventDone})
	}()
	return ch, nil
}

func toOpenAITools(defs []ToolDef) []any {
	out := make([]any, 0, len(defs))
	for _, d := range defs {
		params := d.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        d.Name,
				"description": d.Description,
				"parameters":  json.RawMessage(params),
			},
		})
	}
	return out
}

func toOpenAIMessages(system string, msgs []Message) []any {
	out := make([]any, 0, len(msgs)+1)
	if system != "" {
		out = append(out, map[string]any{"role": "system", "content": system})
	}
	for _, m := range msgs {
		switch m.Role {
		case RoleAssistant:
			var text strings.Builder
			var calls []any
			for _, p := range m.Parts {
				switch p.Type {
				case PartText:
					text.WriteString(p.Text)
				case PartToolCall:
					if p.ToolCall == nil {
						continue
					}
					args := string(p.ToolCall.Arguments)
					if strings.TrimSpace(args) == "" {
						args = "{}"
					}
					calls = append(calls, map[string]any{
						"id":   p.ToolCall.ID,
						"type": "function",
						"function": map[string]any{
							"name":      p.ToolCall.Name,
							"arguments": args,
						},
					})
				}
			}
			msg := map[string]any{"role": "assistant", "content": text.String()}
			if len(calls) > 0 {
				msg["tool_calls"] = calls
			}
			out = append(out, msg)
		case RoleTool:
			for _, p := range m.Parts {
				if p.Type != PartToolResult || p.ToolResult == nil {
					continue
				}
				out = append(out, map[string]any{
					"role":         "tool",
					"tool_call_id": p.ToolResult.CallID,
					"content":      p.ToolResult.Content,
				})
			}
		default:
			var texts []string
			var images []any
			for _, p := range m.Parts {
				switch p.Type {
				case PartText:
					texts = append(texts, p.Text)
				case PartImage:
					images = append(images, map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": p.ImageURL},
					})
				}
			}
			if len(images) == 0 {
				out = append(out, map[string]any{"role": string(m.Role), "content": strings.Join(texts, "\n")})
				continue
			}
			content := []any{}
			if len(texts) > 0 {
				content = append(content, map[string]any{"type": "text", "text": strings.Join(texts, "\n")})
			}
			content = append(content, images...)
			out = append(out, map[string]any{"role": string(m.Role), "content": content})
		}
	}
	return out
}
