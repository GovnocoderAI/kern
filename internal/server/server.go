// Package server exposes a running Kern session over HTTP with Server-Sent
// Events. It is the backend for the web UI and the desktop shell: one local
// process, one active session, streamed turns.
package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/GovnocoderAI/kern/internal/agent"
	appcore "github.com/GovnocoderAI/kern/internal/app"
	"github.com/GovnocoderAI/kern/internal/permission"
	"github.com/GovnocoderAI/kern/internal/provider"
	"github.com/GovnocoderAI/kern/internal/session"
)

// Options configures the server.
type Options struct {
	// App options used to build the session (workdir, model, auto, …).
	App appcore.Options
	// Token, when set, must be presented as a Bearer token or ?token=. An
	// empty token disables auth (fine for localhost-only use).
	Token string
	// Addr is the listen address, e.g. "127.0.0.1:0".
	Addr string
}

// Server is a local HTTP/SSE front end for one Kern session.
type Server struct {
	app   *appcore.App
	token string
	addr  string

	// chatMu serialises turns; a single session has a single history.
	chatMu sync.Mutex

	permMu  sync.Mutex
	pending map[string]chan permissionReply
	permSeq atomic.Uint64

	// emit is set for the duration of a turn so the observer can write SSE.
	emitMu sync.Mutex
	emit   func(event string, data any)

	listener net.Listener
}

type permissionReply struct {
	allow  bool
	always bool
}

// New builds the session and returns a ready-to-run server.
func New(opts Options) (*Server, error) {
	app, err := appcore.New(opts.App)
	if err != nil {
		return nil, err
	}
	s := &Server{
		app:     app,
		token:   opts.Token,
		addr:    opts.Addr,
		pending: map[string]chan permissionReply{},
	}
	if s.addr == "" {
		s.addr = "127.0.0.1:0"
	}
	return s, nil
}

// App exposes the underlying application (used by the desktop shell).
func (s *Server) App() *appcore.App { return s.app }

// Handler returns the HTTP handler with all routes and auth applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.auth(s.handleHealth))
	mux.HandleFunc("/api/config", s.auth(s.handleConfig))
	mux.HandleFunc("/api/sessions", s.auth(s.handleSessions))
	mux.HandleFunc("/api/sessions/new", s.auth(s.handleNewSession))
	mux.HandleFunc("/api/sessions/load", s.auth(s.handleLoadSession))
	mux.HandleFunc("/api/model", s.auth(s.handleModel))
	mux.HandleFunc("/api/permission", s.auth(s.handlePermission))
	mux.HandleFunc("/api/chat", s.auth(s.handleChat))
	mux.Handle("/", s.authHandler(http.FileServer(http.FS(assets()))))
	return mux
}

// Listen starts the HTTP server and returns the URL including the token.
func (s *Server) Listen() (string, error) {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return "", err
	}
	s.listener = ln
	go func() {
		_ = http.Serve(ln, s.Handler())
	}()
	return s.URL(), nil
}

// URL is the base URL a browser should open, token included.
func (s *Server) URL() string {
	addr := s.addr
	if s.listener != nil {
		addr = s.listener.Addr().String()
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	u := fmt.Sprintf("http://%s:%s/", host, port)
	if s.token != "" {
		u += "?token=" + s.token
	}
	return u
}

// Close shuts the listener down.
func (s *Server) Close() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// ---- auth ----

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" {
			got := r.Header.Get("Authorization")
			if len(got) > 7 && got[:7] == "Bearer " {
				got = got[7:]
			}
			if got == "" {
				got = r.URL.Query().Get("token")
			}
			if got != s.token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

// authHandler wraps an http.Handler with the same token check as auth.
func (s *Server) authHandler(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" {
			got := r.Header.Get("Authorization")
			if len(got) > 7 && got[:7] == "Bearer " {
				got = got[7:]
			}
			if got == "" {
				got = r.URL.Query().Get("token")
			}
			if got != s.token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	}
}

// NewToken returns a random URL-safe token.
func NewToken() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// ---- handlers ----

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "version": "kern"})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	type providerView struct {
		ID     string   `json:"id"`
		Kind   string   `json:"kind"`
		Models []string `json:"models"`
	}
	var provs []providerView
	for _, pid := range s.app.Config.ProviderIDs() {
		p := s.app.Config.Providers[pid]
		provs = append(provs, providerView{ID: pid, Kind: p.Kind(), Models: p.SortedModels()})
	}
	writeJSON(w, map[string]any{
		"provider":  s.app.ProviderID,
		"model":     s.app.ModelID,
		"workdir":   s.app.Workdir,
		"theme":     s.app.Config.Theme,
		"providers": provs,
		"agents":    agentNames(),
	})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	list, err := s.app.Store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type view struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Model   string `json:"model"`
		Updated string `json:"updated"`
		Active  bool   `json:"active"`
	}
	out := make([]view, 0, len(list))
	for _, ss := range list {
		out = append(out, view{
			ID: ss.ID, Title: ss.Title, Model: ss.Model,
			Updated: ss.UpdatedAt.Format(time.RFC3339),
			Active:  ss.ID == s.app.Session.ID,
		})
	}
	writeJSON(w, map[string]any{"sessions": out, "active": s.app.Session.ID})
}

func (s *Server) handleNewSession(w http.ResponseWriter, r *http.Request) {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	_ = s.app.Save()
	sess := session.New(s.app.Workdir, s.app.ProviderID+"/"+s.app.ModelID)
	s.app.Session = sess
	s.app.Agent.Reset()
	writeJSON(w, map[string]any{"id": sess.ID})
}

func (s *Server) handleLoadSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	sess, err := s.app.Store.Load(body.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	_ = s.app.Save()
	s.app.Session = sess
	s.app.Agent.Reset()
	s.app.Agent.History = sess.Messages
	s.app.Agent.Usage = sess.Usage
	writeJSON(w, map[string]any{"id": sess.ID, "title": sess.Title})
}

func (s *Server) handleModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model == "" {
		http.Error(w, "model required", http.StatusBadRequest)
		return
	}
	if err := s.app.RebuildModel(body.Model); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"provider": s.app.ProviderID, "model": s.app.ModelID})
}

func (s *Server) handlePermission(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID     string `json:"id"`
		Allow  bool   `json:"allow"`
		Always bool   `json:"always"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	s.permMu.Lock()
	ch, ok := s.pending[body.ID]
	if ok {
		delete(s.pending, body.ID)
	}
	s.permMu.Unlock()
	if !ok {
		http.Error(w, "no such permission request", http.StatusNotFound)
		return
	}
	ch <- permissionReply{allow: body.Allow, always: body.Always}
	writeJSON(w, map[string]any{"ok": true})
}

// ---- chat (SSE) ----

type sseObserver struct {
	emit func(event string, data any)
}

func (o *sseObserver) OnText(delta string) { o.emit("text", map[string]any{"delta": delta}) }
func (o *sseObserver) OnToolStart(name, summary string) {
	o.emit("tool_start", map[string]any{"name": name, "summary": summary})
}
func (o *sseObserver) OnToolResult(name string, ok bool, output string) {
	o.emit("tool_result", map[string]any{"name": name, "ok": ok, "output": output})
}
func (o *sseObserver) OnUsage(u provider.Usage) { o.emit("usage", u) }
func (o *sseObserver) OnStepDone()              { o.emit("step_done", nil) }

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text  string `json:"text"`
		Skill string `json:"skill"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Text == "" {
		http.Error(w, "text required", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	var writeMu sync.Mutex
	emit := func(event string, data any) {
		writeMu.Lock()
		defer writeMu.Unlock()
		raw, err := json.Marshal(data)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
		flusher.Flush()
	}

	// One turn at a time: the session has one history and one agent.
	s.chatMu.Lock()
	defer s.chatMu.Unlock()

	prevObserver := s.app.Agent.Observer
	prevPrompt := s.app.Agent.Perms.Prompt
	s.app.Agent.Observer = &sseObserver{emit: emit}
	s.app.Agent.Perms.Prompt = s.permissionPrompt(emit)
	defer func() {
		s.app.Agent.Observer = prevObserver
		s.app.Agent.Perms.Prompt = prevPrompt
	}()

	in := agent.RunInput{Text: body.Text}
	if body.Skill != "" {
		in.Skill = body.Skill
	}

	err := s.app.Agent.Run(r.Context(), in)
	s.app.Sync()
	if saveErr := s.app.Save(); saveErr != nil && err == nil {
		err = saveErr
	}
	if err != nil {
		emit("turn_done", map[string]any{"error": err.Error()})
		return
	}
	emit("turn_done", map[string]any{
		"session":  s.app.Session.ID,
		"provider": s.app.ProviderID,
		"model":    s.app.ModelID,
	})
}

// permissionPrompt returns a PromptFunc that asks the browser and blocks until
// the client replies via POST /api/permission.
func (s *Server) permissionPrompt(emit func(string, any)) permission.PromptFunc {
	return func(action, resource string) (bool, error) {
		id := fmt.Sprintf("perm-%d", s.permSeq.Add(1))
		ch := make(chan permissionReply, 1)
		s.permMu.Lock()
		s.pending[id] = ch
		s.permMu.Unlock()
		defer func() {
			s.permMu.Lock()
			delete(s.pending, id)
			s.permMu.Unlock()
		}()
		emit("permission", map[string]any{"id": id, "action": action, "resource": resource})
		rep := <-ch
		if rep.always && rep.allow {
			s.app.Agent.Perms.Allow(action, resource)
		}
		return rep.allow, nil
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// agentNames returns the built-in role names for the web UI.
func agentNames() []string {
	return []string{"plan", "build", "test", "review"}
}
