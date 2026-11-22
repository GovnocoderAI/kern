// Package session persists conversations as an append-only event log so that
// any consumer can replay the log to reconstruct the conversation state.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/GovnocoderAI/kern/internal/provider"
	"github.com/GovnocoderAI/kern/internal/util"
)

// Session is a replayable conversation. The Messages and Usage fields are the
// result of folding the event log; AppendEvent adds new events to the log.
type Session struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	Workdir   string             `json:"workdir"`
	Model     string             `json:"model"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
	Messages  []provider.Message `json:"messages"`
	Usage     provider.Usage     `json:"usage"`
}

// Event is one step in the append-only log.
type Event struct {
	// Seq is the 1-based position in the log.
	Seq     int              `json:"seq"`
	Role    string           `json:"role"`
	Message provider.Message `json:"message"`
	Usage   provider.Usage   `json:"usage,omitempty"`
	// Compact, when set, replaces the pre-compaction messages with the
	// provided summary message.
	Compact *Compact `json:"compact,omitempty"`
}

// Compact describes a context-compaction event.
type Compact struct {
	Summary string `json:"summary"`
	Kept    int    `json:"kept"`
}

// Fold reconstructs the conversation from a sequence of events.
// fold(events) returns the message list that the provider would see.
// Compact events are represented as user messages prefixed with
// "[compacted context] " so that Fold is stateless: it only appends.
func Fold(events []Event) []provider.Message {
	var msgs []provider.Message
	for _, ev := range events {
		if ev.Message.Role == "" {
			continue
		}
		msgs = append(msgs, ev.Message)
	}
	return msgs
}

// Events returns the session's event log, reconstructed from the message list.
// Compact summaries are stored as user messages prefixed with
// "[compacted context] " so that the log is a flat, replayable sequence:
// Fold(events) == Messages.
func (s *Session) Events() []Event {
	if len(s.Messages) == 0 {
		return nil
	}
	out := make([]Event, 0, len(s.Messages))
	for i, m := range s.Messages {
		ev := Event{Seq: i + 1, Role: string(m.Role), Message: m}
		out = append(out, ev)
	}
	return out
}

// AppendEvent records a new message in the session's log.
func (s *Session) AppendEvent(msg provider.Message) Event {
	s.Messages = append(s.Messages, msg)
	s.UpdatedAt = time.Now()
	return Event{Seq: len(s.Messages), Role: string(msg.Role), Message: msg}
}

// AppendCompact records a compaction event. The message list is rebuilt so
// that only the most recent Kept messages are retained, with the summary
// prepended as a user message the model will see as context.
func (s *Session) AppendCompact(summary string, kept int) Event {
	events := s.Events()
	// Keep only the last `kept` messages from the current log.
	var keptMsgs []provider.Message
	if kept > 0 && len(s.Messages) >= kept {
		keptMsgs = s.Messages[len(s.Messages)-kept:]
	}
	s.Messages = nil
	if summary != "" {
		s.Messages = append(s.Messages, provider.Text(provider.RoleUser, "[compacted context] "+summary))
	}
	s.Messages = append(s.Messages, keptMsgs...)
	ev := Event{Seq: len(events) + 1, Role: "compact", Compact: &Compact{Summary: summary, Kept: kept}}
	s.UpdatedAt = time.Now()
	return ev
}

type Store struct {
	Dir string
}

func NewStore() *Store {
	return &Store{Dir: util.SessionsDir()}
}

func New(workdir, model string) *Session {
	now := time.Now()
	return &Session{
		ID:        newID(now),
		Title:     "New session",
		Workdir:   workdir,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func newID(now time.Time) string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return now.Format("20060102-150405") + "-" + hex.EncodeToString(buf)
}

func (s *Store) path(id string) string {
	return filepath.Join(s.Dir, id+".json")
}

func (s *Store) Save(sess *Session) error {
	if sess == nil || sess.ID == "" {
		return fmt.Errorf("invalid session")
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	if sess.Title == "" || sess.Title == "New session" {
		sess.Title = deriveTitle(sess)
	}
	raw, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(sess.ID) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(sess.ID))
}

func deriveTitle(sess *Session) string {
	for _, m := range sess.Messages {
		if m.Role != provider.RoleUser {
			continue
		}
		text := strings.TrimSpace(m.Text())
		if text == "" || strings.HasPrefix(text, "[compacted context] ") {
			continue
		}
		if len(text) > 60 {
			text = text[:59] + "…"
		}
		return text
	}
	return "New session"
}

func (s *Store) Load(id string) (*Session, error) {
	raw, err := os.ReadFile(s.path(id))
	if err != nil {
		return nil, err
	}
	var sess Session
	if err := json.Unmarshal(raw, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) Delete(id string) error {
	return os.Remove(s.path(id))
}

func (s *Store) List() ([]*Session, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		sess, err := s.Load(id)
		if err != nil {
			continue
		}
		out = append(out, sess)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s *Store) Latest(workdir string) (*Session, error) {
	sessions, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, sess := range sessions {
		if samePath(sess.Workdir, workdir) {
			return sess, nil
		}
	}
	return nil, os.ErrNotExist
}

func samePath(a, b string) bool {
	if a == b {
		return true
	}
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.EqualFold(aa, bb)
}
