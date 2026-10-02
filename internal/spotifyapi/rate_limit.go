package spotifyapi

import (
	"context"
	"io"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/logging"
)

const readRetryLimit = 2
const readWaitBudget = 30 * time.Second

// readTransport is inside the destination guard and outside OAuth. It shares
// cooldown state across SDK and /items GETs, but never replays a mutation.
// After a cooldown only one recovery probe runs until its response arrives.
// Ordinary successful reads can still run concurrently.
type readTransport struct {
	next              http.RoundTripper
	mu                sync.Mutex
	until             time.Time
	recovery, probing bool
	changed           chan struct{}
}

func newReadTransport(next http.RoundTripper) *readTransport {
	return &readTransport{next: next, changed: make(chan struct{})}
}

func (t *readTransport) signal() { close(t.changed); t.changed = make(chan struct{}) }

func (t *readTransport) wait(ctx context.Context) (bool, error) {
	for {
		t.mu.Lock()
		remaining := time.Until(t.until)
		changed := t.changed
		if remaining > 0 {
			t.mu.Unlock()
			logging.FromContext(ctx).Warn("rate limit waiting", "seconds", int(math.Ceil(remaining.Seconds())))
			timer := time.NewTimer(remaining)
			select {
			case <-ctx.Done():
				timer.Stop()
				return false, ctx.Err()
			case <-changed:
				timer.Stop()
			case <-timer.C:
			}
			continue
		}
		if t.recovery && t.probing {
			t.mu.Unlock()
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-changed:
			}
			continue
		}
		probe := t.recovery
		if probe {
			t.probing = true
		}
		t.mu.Unlock()
		return probe, nil
	}
}

// Retry-After is seconds in Spotify's 429 contract. Missing/invalid headers are
// surfaced, not guessed. A zero header uses a one-second floor (never a spin).
func retryDelay(header string) (time.Duration, bool) {
	seconds, err := strconv.ParseUint(header, 10, 31)
	if err != nil {
		return 0, false
	}
	if seconds == 0 {
		seconds = 1
	}
	return time.Duration(seconds) * time.Second, true
}

func (t *readTransport) finish(probe bool, delay time.Duration, limited bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if limited {
		until := time.Now().Add(delay)
		if until.After(t.until) {
			t.until = until
		}
		t.recovery = true
	} else if probe && !time.Now().Before(t.until) {
		// A response from an earlier in-flight request may have extended the
		// cooldown during this probe. Success must not erase that newer deadline.
		t.until = time.Time{}
		t.recovery = false
	}
	if probe {
		t.probing = false
	}
	t.signal()
}

func (t *readTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet {
		return t.next.RoundTrip(r)
	}
	// The budget bounds shared cooldown waits even for clients without Timeout.
	// Do not send this derived context downstream: cancelling it on return would
	// prematurely cancel response-body reads. The original client timeout stays.
	waitCtx, cancel := context.WithTimeout(r.Context(), readWaitBudget)
	defer cancel()
	for attempt := 0; ; attempt++ {
		probe, err := t.wait(waitCtx)
		if err != nil {
			return nil, err
		}
		response, err := t.next.RoundTrip(r)
		if err != nil {
			t.finish(probe, 0, false)
			return response, err
		}
		if response.StatusCode != http.StatusTooManyRequests {
			t.finish(probe, 0, false)
			return response, nil
		}
		delay, valid := retryDelay(response.Header.Get("Retry-After"))
		t.finish(probe, delay, valid)
		if !valid || delay > readWaitBudget || attempt >= readRetryLimit {
			return response, nil
		}
		// Retried read responses are closed; the final response remains for the
		// normal SDK/items error decoder. Drain only a small bounded amount.
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
	}
}
