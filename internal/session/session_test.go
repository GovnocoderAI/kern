package session

import (
	"strings"
	"testing"

	"github.com/GovnocoderAI/kern/internal/provider"
)

func TestFoldBasic(t *testing.T) {
	events := []Event{
		{Seq: 1, Role: string(provider.RoleUser), Message: provider.Text(provider.RoleUser, "hello")},
		{Seq: 2, Role: string(provider.RoleAssistant), Message: provider.Text(provider.RoleAssistant, "hi")},
		{Seq: 3, Role: string(provider.RoleUser), Message: provider.Text(provider.RoleUser, "how are you")},
	}
	msgs := Fold(events)
	if len(msgs) != 3 {
		t.Fatalf("Fold = %d messages", len(msgs))
	}
	if msgs[0].Text() != "hello" || msgs[2].Text() != "how are you" {
		t.Fatalf("Fold messages = %+v", msgs)
	}
}

func TestFoldWithCompact(t *testing.T) {
	// Simulate what AppendCompact produces in the live message list: the
	// summary is stored as a user message, then the kept messages follow.
	// A subsequent event appends to that state.
	events := []Event{
		// After AppendCompact("summary", 1) on [a, b], the live list is
		// [summary, b]; event 4 appends "c".
		{Seq: 1, Role: string(provider.RoleUser), Message: provider.Text(provider.RoleUser, "[compacted context] discussed a and b")},
		{Seq: 2, Role: string(provider.RoleAssistant), Message: provider.Text(provider.RoleAssistant, "b")},
		{Seq: 3, Role: string(provider.RoleUser), Message: provider.Text(provider.RoleUser, "c")},
	}
	msgs := Fold(events)
	// Expected: [summary, "b", "c"]
	if len(msgs) != 3 {
		t.Fatalf("Fold after compact = %d messages: %+v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0].Text(), "discussed") {
		t.Fatalf("expected compact summary first, got %q", msgs[0].Text())
	}
	if msgs[1].Text() != "b" || msgs[2].Text() != "c" {
		t.Fatalf("expected [summary, b, c], got %+v", msgs)
	}
}

func TestAppendEvent(t *testing.T) {
	s := New("/tmp", "model")
	ev1 := s.AppendEvent(provider.Text(provider.RoleUser, "hello"))
	ev2 := s.AppendEvent(provider.Text(provider.RoleAssistant, "hi"))
	if ev1.Seq != 1 || ev2.Seq != 2 {
		t.Fatalf("seqs = %d, %d", ev1.Seq, ev2.Seq)
	}
	if len(s.Messages) != 2 {
		t.Fatalf("messages = %d", len(s.Messages))
	}
}

func TestAppendCompact(t *testing.T) {
	s := New("/tmp", "model")
	s.AppendEvent(provider.Text(provider.RoleUser, "a"))
	s.AppendEvent(provider.Text(provider.RoleAssistant, "b"))
	s.AppendEvent(provider.Text(provider.RoleUser, "c"))
	s.AppendEvent(provider.Text(provider.RoleAssistant, "d"))

	// Compact with a summary and keep the last 2 messages.
	s.AppendCompact("discussed a and b", 2)

	// After compaction: [summary, "c", "d"]
	if len(s.Messages) != 3 {
		t.Fatalf("after compact = %d messages: %+v", len(s.Messages), s.Messages)
	}
	if !strings.Contains(s.Messages[0].Text(), "discussed") {
		t.Fatalf("first message should be the summary, got %q", s.Messages[0].Text())
	}
	if s.Messages[1].Text() != "c" || s.Messages[2].Text() != "d" {
		t.Fatalf("kept messages wrong: %+v", s.Messages[1:])
	}

	// The event log now has 6 events (4 original + compact + 2 re-added? No—
	// AppendCompact resets s.Messages, so Events() reflects the new state.
	// The compact event itself is not stored in Messages, so Events()
	// returns 3 events: summary, c, d.
	events := s.Events()
	replayed := Fold(events)
	if len(replayed) != 3 {
		t.Fatalf("replay after compact = %d messages", len(replayed))
	}
}

func TestEventsRoundTrip(t *testing.T) {
	s := New("/tmp", "model")
	s.AppendEvent(provider.Text(provider.RoleUser, "question"))
	s.AppendEvent(provider.Text(provider.RoleAssistant, "answer"))
	events := s.Events()
	replayed := Fold(events)
	if len(replayed) != 2 {
		t.Fatalf("round-trip = %d messages", len(replayed))
	}
	if replayed[0].Text() != "question" || replayed[1].Text() != "answer" {
		t.Fatalf("round-trip messages = %+v", replayed)
	}
}
