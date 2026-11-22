// Package provider implements streaming LLM providers.
//
// Middleware wraps a Provider to add cross-cutting behavior (retry, timeouts)
// without touching the protocol-specific code. Wrap providers with
// provider.Retry(...) and provider.Timeout(...) when building them in the app
// layer.
package provider

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"
)

// Middleware decorates a Provider with cross-cutting behavior.
type Middleware func(Provider) Provider

// Apply chains middlewares left to right: Apply(p, m1, m2) == m2(m1(p)).
func Apply(p Provider, mws ...Middleware) Provider {
	for _, mw := range mws {
		p = mw(p)
	}
	return p
}

// httpError carries the HTTP status of a provider request failure.
type httpError struct {
	ProviderID string
	Status     int
	Body       string
	Err        error
}

func (e *httpError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.ProviderID, e.Err)
	}
	if e.Body != "" {
		return fmt.Sprintf("%s: HTTP %d: %s", e.ProviderID, e.Status, e.Body)
	}
	return fmt.Sprintf("%s: HTTP %d", e.ProviderID, e.Status)
}

// Unwrap exposes the cause.
func (e *httpError) Unwrap() error { return e.Err }

// IsHTTPError extracts a status code from a provider error.
func IsHTTPError(err error) (status int, ok bool) {
	var he *httpError
	if errors.As(err, &he) {
		return he.Status, true
	}
	return 0, false
}

// RetryPolicy configures the retry middleware.
type RetryPolicy struct {
	// MaxAttempts is the total number of tries, including the first.
	MaxAttempts int
	// Initial is the first backoff delay.
	Initial time.Duration
	// Max is the upper bound for a single backoff delay.
	Max time.Duration
	// RetryableExtra lists additional HTTP statuses to retry (defaults: 429, 5xx).
	RetryableExtra []int
	// Clock is injectable for tests.
	Clock func() time.Time
	// Log, when set, records each retry.
	Log func(attempt int, delay time.Duration, err error)
}

// Retry returns middleware that retries transient provider failures with
// exponential backoff and jitter. It retries network errors, 429 and 5xx
// responses.
func Retry(p RetryPolicy) Middleware {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 3
	}
	if p.Initial <= 0 {
		p.Initial = 500 * time.Millisecond
	}
	if p.Max < p.Initial {
		p.Max = 30 * time.Second
	}
	if p.Clock == nil {
		p.Clock = time.Now
	}
	extra := map[int]bool{}
	for _, c := range p.RetryableExtra {
		extra[c] = true
	}
	return func(next Provider) Provider {
		return &retryProvider{next: next, policy: p, extra: extra}
	}
}

type retryProvider struct {
	next  Provider
	policy RetryPolicy
	extra  map[int]bool
}

func (r *retryProvider) ID() string   { return r.next.ID() }
func (r *retryProvider) Kind() string { return r.next.Kind() }

func (r *retryProvider) Models(ctx context.Context) ([]ModelInfo, error) {
	var lastErr error
	for attempt := 1; ; attempt++ {
		models, err := r.next.Models(ctx)
		if err == nil {
			return models, nil
		}
		if !r.retryable(err) {
			return nil, err
		}
		lastErr = err
		if attempt >= r.policy.MaxAttempts {
			return nil, fmt.Errorf("%s: %d attempts exhausted: %v", r.next.ID(), r.policy.MaxAttempts, err)
		}
		delay := r.backoff(attempt, lastErr)
		if r.policy.Log != nil {
			r.policy.Log(attempt, delay, lastErr)
		}
		if !r.sleep(ctx, delay) {
			return nil, ctx.Err()
		}
	}
}

func (r *retryProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	for attempt := 1; ; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		ch, err := r.next.Stream(ctx, req)
		if err == nil {
			return r.watchStream(ch, ctx, req, 1), nil
		}
		if !r.retryable(err) {
			return nil, err
		}
		if attempt >= r.policy.MaxAttempts {
			return nil, fmt.Errorf("%s: %d attempts exhausted: %v", r.next.ID(), r.policy.MaxAttempts, err)
		}
		delay := r.backoff(attempt, err)
		if r.policy.Log != nil {
			r.policy.Log(attempt, delay, err)
		}
		if !r.sleep(ctx, delay) {
			return nil, ctx.Err()
		}
	}
}

// watchStream relays events and, on a transient error before the final event,
// restarts the upstream request with the same parameters.
func (r *retryProvider) watchStream(ch <-chan Event, ctx context.Context, req Request, attempts int) <-chan Event {
	out := make(chan Event, 32)
	go func() {
		defer close(out)
		for {
			restart := false
			var lastErr error
		loop:
			for ev := range ch {
				out <- ev
				if ev.Type == EventError {
					lastErr = ev.Err
					restart = r.retryable(lastErr)
					break loop
				}
				if ev.Type == EventDone {
					return
				}
			}
			if !restart || attempts >= r.policy.MaxAttempts {
				return
			}
			attempts++
			delay := r.backoff(attempts, lastErr)
			if r.policy.Log != nil {
				r.policy.Log(attempts, delay, lastErr)
			}
			if !r.sleep(ctx, delay) {
				out <- Event{Type: EventError, Err: ctx.Err()}
				return
			}
			nextCh, err := r.next.Stream(ctx, req)
			if err != nil {
				out <- Event{Type: EventError, Err: err}
				return
			}
			ch = nextCh
		}
	}()
	return out
}

func (r *retryProvider) sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (r *retryProvider) retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if status, ok := IsHTTPError(err); ok {
		if r.extra[status] {
			return true
		}
		switch {
		case status == 429:
			return true
		case status >= 500 && status < 600:
			return true
		}
		return false
	}
	// Plain network errors are considered retryable.
	return true
}

func (r *retryProvider) backoff(attempt int, err error) time.Duration {
	d := float64(r.policy.Initial) * math.Pow(2, float64(attempt-1))
	if d > float64(r.policy.Max) {
		d = float64(r.policy.Max)
	}
	jitter := rand.Int63n(int64(d)/4 + 1)
	return time.Duration(d) + time.Duration(jitter)
}

// Timeout enforces a response-header timeout on providers that expose
// WithHeaderTimeout. Providers that do not pass through unchanged: their own
// HTTP client already carries idle and handshake timeouts.
func Timeout(headerTimeout time.Duration) Middleware {
	return func(next Provider) Provider {
		return &timeoutProvider{next: next, d: headerTimeout}
	}
}

type timeoutProvider struct {
	next Provider
	d    time.Duration
}

func (t *timeoutProvider) ID() string   { return t.next.ID() }
func (t *timeoutProvider) Kind() string { return t.next.Kind() }

func (t *timeoutProvider) Models(ctx context.Context) ([]ModelInfo, error) {
	return t.next.Models(ctx)
}

func (t *timeoutProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	if p, ok := t.next.(interface {
		WithHeaderTimeout(time.Duration) Provider
	}); ok {
		return p.WithHeaderTimeout(t.d).Stream(ctx, req)
	}
	return t.next.Stream(ctx, req)
}
