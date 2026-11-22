package session

import (
	"testing"

	"github.com/GovnocoderAI/kern/internal/provider"
)

func TestSaveLoadLatest(t *testing.T) {
	t.Setenv("KERN_DATA_DIR", t.TempDir())
	store := NewStore()

	s := New("/work", "p/m")
	s.Messages = append(s.Messages, provider.Text(provider.RoleUser, "hello there"))
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Title != "hello there" {
		t.Fatalf("title = %q", loaded.Title)
	}
	if len(loaded.Messages) != 1 {
		t.Fatalf("messages = %d", len(loaded.Messages))
	}

	latest, err := store.Latest("/work")
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != s.ID {
		t.Fatalf("latest = %s", latest.ID)
	}

	list, err := store.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %d, %v", len(list), err)
	}
}

func TestLatestNoMatch(t *testing.T) {
	t.Setenv("KERN_DATA_DIR", t.TempDir())
	store := NewStore()
	s := New("/one", "p/m")
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Latest("/two"); err == nil {
		t.Fatal("expected not found")
	}
}
