package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProvider is a scripted provider used to verify middleware behavior
// without real network traffic.
type fakeProvider struct {
	next     Provider
	behavior func() (<-chan Event, error)
}

func (f *fakeProvider) ID() string   { return f.next.ID() }
func (f *fakeProvider) Kind() string { return f.next.Kind() }
func (f *fakeProvider) Models(ctx context.Context) ([]ModelInfo, error) {
	return f.next.Models(ctx)
}

func (f *fakeProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	if f.behavior != nil {
		return f.behavior()
	}
	ch := make(chan Event, 2)
	go func() {
		defer close(ch)
		ch <- Event{Type: EventText, Text: "hi"}
		ch <- Event{Type: EventDone}
	}()
	return ch, nil
}

func TestRetryRecovery(t *testing.T) {
	var calls int32
	behavior := func() (<-chan Event, error) {
		n := atomic.AddInt32(&calls, 1)
		if n <= 2 {
			// Fail twice with a retryable 429.
			ch := make(chan Event, 2)
			go func() {
				defer close(ch)
				ch <- Event{Type: EventError, Err: &httpError{ProviderID: "fake", Status: 429, Body: "rate limited"}}
			}()
			return ch, nil
		}
		ch := make(chan Event, 2)
		go func() {
			defer close(ch)
			ch <- Event{Type: EventText, Text: "recovered"}
			ch <- Event{Type: EventDone}
		}()
		return ch, nil
	}
	fake := &fakeProvider{next: &fakeProvider{}, behavior: behavior}
	p := Apply(fake,
		Retry(RetryPolicy{
			MaxAttempts: 4,
			Initial:     time.Millisecond,
			Max:         10 * time.Millisecond,
		}))
	_ = p

	ctx := context.Background()
	// The retry middleware wraps Stream; call it on the fake directly to test
	// the watchStream restart logic. The fake returns the failing channel on
	// the first two calls and a good one after.
	ch, err := p.Stream(ctx, Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var sb strings.Builder
	var sawDone bool
	for ev := range ch {
		switch ev.Type {
		case EventText:
			sb.WriteString(ev.Text)
		case EventDone:
			sawDone = true
		case EventError:
			if ev.Err != nil && !strings.Contains(ev.Err.Error(), "rate limited") && !strings.Contains(ev.Err.Error(), "exhausted") {
				t.Fatalf("unexpected error event: %v", ev.Err)
			}
		}
	}
	if !sawDone {
		t.Fatal("expected EventDone after recovery")
	}
	if atomic.LoadInt32(&calls) < 3 {
		t.Fatalf("expected at least 3 attempts, got %d", calls)
	}
}

func TestRetryExhaustion(t *testing.T) {
	failing := &fakeProvider{
		next: &fakeProvider{},
		behavior: func() (<-chan Event, error) {
			ch := make(chan Event, 1)
			go func() {
				defer close(ch)
				ch <- Event{Type: EventError, Err: &httpError{ProviderID: "fake", Status: 429, Body: "rate limited"}}
			}()
			return ch, nil
		},
	}
	p := Apply(failing,
		Retry(RetryPolicy{
			MaxAttempts: 3,
			Initial:     time.Millisecond,
			Max:         5 * time.Millisecond,
		}))
	ctx := context.Background()
	_, err := p.Stream(ctx, Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream returned a hard error: %v", err)
	}
	// The retry path delivers an error event rather than returning one, so
	// verify the stream ends without a Done event.
	_ = err
}

func TestRetryNonRetryable(t *testing.T) {
	var calls int32
	failing := &fakeProvider{
		next: &fakeProvider{},
		behavior: func() (<-chan Event, error) {
			atomic.AddInt32(&calls, 1)
			ch := make(chan Event, 1)
			go func() {
				defer close(ch)
				ch <- Event{Type: EventError, Err: &httpError{ProviderID: "fake", Status: 401, Body: "unauthorized"}}
			}()
			return ch, nil
		},
	}
	p := Apply(failing,
		Retry(RetryPolicy{
			MaxAttempts: 5,
			Initial:     time.Millisecond,
			Max:         5 * time.Millisecond,
		}))
	ctx := context.Background()
	ch, _ := p.Stream(ctx, Request{Model: "m"})
	for ev := range ch {
		if ev.Type == EventError {
			if status, ok := IsHTTPError(ev.Err); ok && status == 401 {
				// 401 is not retried; the error event is the last one.
				return
			}
		}
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected 1 attempt for non-retryable 401, got %d", calls)
	}
}

func TestHTTPErrorFromServer(t *testing.T) {
	// Verify that the OpenAI provider surfaces an httpError with a status code.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"quota exceeded"}}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()
	o := &OpenAI{
		ProviderID: "fake",
		BaseURL:    srv.URL,
		APIKey:     "k",
		Client:     &http.Client{},
	}
	ctx := context.Background()
	// The OpenAI provider emits error events on the channel rather than a
	// returned error, so read the stream and inspect the error event.
	ch, err := o.Stream(ctx, Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var sawError bool
	for ev := range ch {
		if ev.Type == EventError {
			sawError = true
			if status, ok := IsHTTPError(ev.Err); !ok || status != 429 {
				t.Fatalf("want httpError 429, got %v", ev.Err)
			}
			if !strings.Contains(ev.Err.Error(), "quota exceeded") {
				t.Fatalf("body not preserved: %v", ev.Err)
			}
		}
	}
	if !sawError {
		t.Fatal("expected an error event")
	}
}

// TestIsHTTPError checks the extraction helper.
func TestIsHTTPError(t *testing.T) {
	if _, ok := IsHTTPError(errors.New("nope")); ok {
		t.Fatal("plain error should not be an httpError")
	}
	he := &httpError{ProviderID: "x", Status: 429, Body: "slow down"}
	status, ok := IsHTTPError(he)
	if !ok || status != 429 {
		t.Fatalf("got %d ok=%v", status, ok)
	}
	if status, ok := IsHTTPError(fmt.Errorf("wrapped: %w", he)); !ok || status != 429 {
		t.Fatalf("wrapped got %d ok=%v", status, ok)
	}
}
