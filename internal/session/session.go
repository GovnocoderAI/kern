// Package session persists conversations as JSON files.
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

	"github.com/kern-ai/kern/internal/provider"
	"github.com/kern-ai/kern/internal/util"
)

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
		if text == "" {
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
