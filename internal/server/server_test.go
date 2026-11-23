package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appcore "github.com/GovnocoderAI/kern/internal/app"
	"github.com/GovnocoderAI/kern/internal/provider"
)

// echoProvider streams back the user's prompt, then a usage event.
type echoProvider struct{}

func (echoProvider) ID() string   { return "echo" }
func (echoProvider) Kind() string { return "openai" }
func (echoProvider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	return nil, nil
}

func (echoProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	ch := make(chan provider.Event, 4)
	go func() {
		defer close(ch)
		ch <- provider.Event{Type: provider.EventText, Text: "echo: "}
		ch <- provider.Event{Type: provider.EventText, Text: "hello"}
		ch <- provider.Event{Type: provider.EventUsage, Usage: &provider.Usage{TotalTokens: 7}}
		ch <- provider.Event{Type: provider.EventDone}
	}()
	return ch, nil
}

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("KERN_HOME", filepath.Join(home, ".kern"))
	t.Setenv("KERN_CONFIG_DIR", "")
	t.Setenv("KERN_DATA_DIR", filepath.Join(home, "data"))
	t.Setenv("OPENCODE_CONFIG_DIR", filepath.Join(home, "no-oc"))
	t.Setenv("OPENCODE_DATA_DIR", filepath.Join(home, "no-oc-data"))
	t.Setenv("ProgramData", filepath.Join(home, "pd"))

	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	// Minimal kern.json so config resolves a model.
	if err := os.WriteFile(filepath.Join(proj, "kern.json"), []byte(`{
		"model": "echo/model",
		"provider": {"echo": {"kind": "openai", "options": {"baseURL": "http://localhost:1"}}}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := New(Options{
		App:   appcore.Options{Workdir: proj, Model: "echo/model", Auto: true},
		Token: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Swap the provider with the echo double so no network is needed.
	srv.app.Agent.Provider = echoProvider{}

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func TestAuthRequired(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestConfigEndpoint(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/config", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var cfg map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["provider"] != "echo" || cfg["model"] != "model" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestChatStreamsSSE(t *testing.T) {
	_, ts := newTestServer(t)
	body := strings.NewReader(`{"text":"hi there"}`)
	req, _ := http.NewRequest("POST", ts.URL+"/api/chat", body)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}

	var text strings.Builder
	var sawDone bool
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "event: text") {
			// next data line follows
		}
		if strings.HasPrefix(line, "data: ") {
			var d map[string]any
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &d) == nil {
				if v, ok := d["delta"].(string); ok {
					text.WriteString(v)
				}
			}
		}
		if strings.HasPrefix(line, "event: turn_done") {
			sawDone = true
		}
	}
	if !strings.Contains(text.String(), "echo: hello") {
		t.Fatalf("streamed text = %q", text.String())
	}
	if !sawDone {
		t.Fatal("no turn_done event")
	}
}

func TestSessionsEndpoints(t *testing.T) {
	_, ts := newTestServer(t)

	// Create a session.
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/new", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// List.
	req, _ = http.NewRequest("GET", ts.URL+"/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var data struct {
		Sessions []map[string]any `json:"sessions"`
		Active   string           `json:"active"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}
	if data.Active == "" {
		t.Fatal("no active session id")
	}
	if len(data.Sessions) < 1 {
		t.Fatalf("expected at least one session, got %d", len(data.Sessions))
	}
}

func TestStaticAssetsServed(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	buf := make([]byte, 200)
	n, _ := resp.Body.Read(buf)
	if !strings.Contains(strings.ToLower(string(buf[:n])), "<!doctype html") {
		t.Fatalf("index.html not served: %q", string(buf[:n]))
	}
}
