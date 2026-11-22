package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Gemini implements the provider.Provider interface for Google's Gemini API.
// It speaks the streamGenerateContent SSE protocol and maps the response to
// OpenAI-compatible chunks so the agent loop works unchanged.
type Gemini struct {
	ProviderID string
	BaseURL    string
	APIKey     string
	Headers    map[string]string
	http       *http.Client
}

func (g *Gemini) ID() string   { return g.ProviderID }
func (g *Gemini) Kind() string { return "google" }

func (g *Gemini) base() string {
	if g.BaseURL != "" {
		return strings.TrimRight(g.BaseURL, "/")
	}
	return "https://generativelanguage.googleapis.com/v1beta"
}

func (g *Gemini) authHeaders() map[string]string {
	h := map[string]string{}
	for k, v := range g.Headers {
		h[k] = v
	}
	if g.APIKey != "" {
		h["x-goog-api-key"] = g.APIKey
	}
	return h
}

func (g *Gemini) Models(ctx context.Context) ([]ModelInfo, error) {
	// Gemini does not expose a /models endpoint; return empty.
	return nil, nil
}

func (g *Gemini) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	if req.Model == "" {
		return nil, &httpError{ProviderID: g.ProviderID, Status: 400, Body: "gemini: model is required"}
	}

	body, err := g.buildRequest(req)
	if err != nil {
		return nil, err
	}

	url := g.base() + "/models/" + req.Model + ":streamGenerateContent?alt=sse"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range g.authHeaders() {
		httpReq.Header.Set(k, v)
	}

	resp, err := g.client().Do(httpReq)
	if err != nil {
		return nil, &httpError{ProviderID: g.ProviderID, Body: err.Error()}
	}
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		resp.Body.Close()
		return nil, &httpError{ProviderID: g.ProviderID, Status: resp.StatusCode, Body: strings.TrimSpace(string(msg))}
	}

	ch := make(chan Event, 8)
	go g.readSSE(resp, ch)
	return ch, nil
}

func (g *Gemini) client() *http.Client {
	if g.http != nil {
		return g.http
	}
	g.http = &http.Client{Timeout: 0}
	return g.http
}

// buildRequest maps a provider.Request into Gemini's generateContent body.
func (g *Gemini) buildRequest(req Request) (string, error) {
	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Role  string `json:"role,omitempty"`
		Parts []part `json:"parts"`
	}
	type toolConfig struct {
		Disabled bool `json:"disabled,omitempty"`
	}
	type functionDeclaration struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	}
	type tool struct {
		FunctionDeclarations []functionDeclaration `json:"functionDeclarations,omitempty"`
	}
	type body struct {
		Contents          []content `json:"contents"`
		SystemInstruction struct {
			Parts []part `json:"parts"`
		} `json:"systemInstruction,omitempty"`
		Tools []tool `json:"tools,omitempty"`
	}
	var b body
	if req.System != "" {
		b.SystemInstruction.Parts = []part{{Text: req.System}}
	}
	for _, m := range req.Messages {
		var c content
		switch m.Role {
		case RoleUser:
			c.Role = "user"
		case RoleAssistant:
			c.Role = "model"
		case RoleTool:
			// Tool results are sent as user content in Gemini.
			c.Role = "user"
		}
		for _, p := range m.Parts {
			switch p.Type {
			case PartText:
				c.Parts = append(c.Parts, part{Text: p.Text})
			case PartToolCall:
				// Tool calls from the model are not re-sent as input; they are
				// part of the assistant turn. Skip them here.
			case PartToolResult:
				if p.ToolResult != nil {
					c.Parts = append(c.Parts, part{Text: p.ToolResult.Content})
				}
			}
		}
		if len(c.Parts) > 0 || c.Role == "model" {
			b.Contents = append(b.Contents, c)
		}
	}
	for _, td := range req.Tools {
		var f functionDeclaration
		f.Name = td.Name
		f.Description = td.Description
		f.Parameters = td.Parameters
		b.Tools = append(b.Tools, tool{FunctionDeclarations: []functionDeclaration{f}})
	}
	data, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// readSSE reads the SSE stream from Gemini and emits provider events.
func (g *Gemini) readSSE(resp *http.Response, ch chan<- Event) {
	defer close(ch)
	defer resp.Body.Close()

	err := readSSE(resp.Body, func(msg sseMessage) error {
		if msg.Data == "" {
			return nil
		}
		g.handleSSEEvent(msg.Data, ch)
		return nil
	})
	if err != nil {
		ch <- Event{Type: EventError, Err: err}
	} else {
		ch <- Event{Type: EventDone}
	}
}

func (g *Gemini) handleSSEEvent(event string, ch chan<- Event) {
	// Each SSE line is a JSON chunk from streamGenerateContent.
	var chunk struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments,omitempty"`
					} `json:"functionCall,omitempty"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(event), &chunk); err != nil {
		ch <- Event{Type: EventError, Err: err}
		return
	}
	if chunk.Error != nil {
		ch <- Event{Type: EventError, Err: fmt.Errorf("gemini: %s", chunk.Error.Message)}
		return
	}
	if chunk.UsageMetadata.TotalTokenCount > 0 {
		ch <- Event{Type: EventUsage, Usage: &Usage{
			InputTokens:  chunk.UsageMetadata.PromptTokenCount,
			OutputTokens: chunk.UsageMetadata.CandidatesTokenCount,
			TotalTokens:  chunk.UsageMetadata.TotalTokenCount,
		}}
	}
	for _, cand := range chunk.Candidates {
		for _, p := range cand.Content.Parts {
			if p.FunctionCall != nil {
				id := fmt.Sprintf("gemini_%s", p.FunctionCall.Name)
				ch <- Event{Type: EventToolCall, ToolCall: &ToolCall{
					ID:        id,
					Name:      p.FunctionCall.Name,
					Arguments: p.FunctionCall.Arguments,
				}}
			}
			if p.Text != "" {
				ch <- Event{Type: EventText, Text: p.Text}
			}
		}
	}
}
