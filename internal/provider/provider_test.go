package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func collect(t *testing.T, ch <-chan Event) []Event {
	t.Helper()
	var out []Event
	for ev := range ch {
		if ev.Type == EventError {
			t.Fatalf("stream error: %v", ev.Err)
		}
		out = append(out, ev)
	}
	return out
}

func TestOpenAIStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer key" {
			t.Errorf("auth = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`{"choices":[{"delta":{"content":"Hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo"}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read","arguments":"{\"path\":\"a\"}"}}]},"finish_reason":"tool_calls"}]}`,
			`{"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
			`[DONE]`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	p := &OpenAI{ProviderID: "test", BaseURL: srv.URL + "/v1", APIKey: "key"}
	ch, err := p.Stream(context.Background(), Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	events := collect(t, ch)

	var text strings.Builder
	var call *ToolCall
	var usage *Usage
	for _, ev := range events {
		switch ev.Type {
		case EventText:
			text.WriteString(ev.Text)
		case EventToolCall:
			call = ev.ToolCall
		case EventUsage:
			usage = ev.Usage
		}
	}
	if text.String() != "Hello" {
		t.Fatalf("text = %q", text.String())
	}
	if call == nil || call.Name != "read" || string(call.Arguments) != `{"path":"a"}` {
		t.Fatalf("call = %+v", call)
	}
	if usage == nil || usage.TotalTokens != 5 || usage.InputTokens != 3 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestAnthropicStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "key" {
			t.Errorf("api key = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		events := []struct{ name, data string }{
			{"message_start", `{"type":"message_start","message":{"usage":{"input_tokens":4,"output_tokens":0}}}`},
			{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`},
			{"content_block_stop", `{"type":"content_block_stop","index":0}`},
			{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"read"}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"a\"}"}}`},
			{"content_block_stop", `{"type":"content_block_stop","index":1}`},
			{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}`},
			{"message_stop", `{"type":"message_stop"}`},
		}
		for _, e := range events {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.name, e.data)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	p := &Anthropic{ProviderID: "test", BaseURL: srv.URL, APIKey: "key"}
	ch, err := p.Stream(context.Background(), Request{Model: "m", Messages: []Message{Text(RoleUser, "hi")}})
	if err != nil {
		t.Fatal(err)
	}
	events := collect(t, ch)

	var text strings.Builder
	var call *ToolCall
	var usage *Usage
	for _, ev := range events {
		switch ev.Type {
		case EventText:
			text.WriteString(ev.Text)
		case EventToolCall:
			call = ev.ToolCall
		case EventUsage:
			usage = ev.Usage
		}
	}
	if text.String() != "Hi" {
		t.Fatalf("text = %q", text.String())
	}
	if call == nil || call.Name != "read" || !strings.Contains(string(call.Arguments), `"path"`) {
		t.Fatalf("call = %+v", call)
	}
	if usage == nil || usage.InputTokens != 4 || usage.OutputTokens != 3 || usage.TotalTokens != 7 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestToolConversion(t *testing.T) {
	msgs := toOpenAIMessages("sys", []Message{
		Text(RoleUser, "hi"),
		{Role: RoleAssistant, Parts: []Part{
			{Type: PartText, Text: "ok"},
			{Type: PartToolCall, ToolCall: &ToolCall{ID: "1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)}},
		}},
		{Role: RoleTool, Parts: []Part{{Type: PartToolResult, ToolResult: &ToolResult{CallID: "1", Name: "read", Content: "x"}}}},
	})
	if len(msgs) != 4 {
		t.Fatalf("messages = %d", len(msgs))
	}
	raw, _ := json.Marshal(msgs)
	if !strings.Contains(string(raw), "tool_call_id") {
		t.Fatalf("missing tool result: %s", raw)
	}
}
