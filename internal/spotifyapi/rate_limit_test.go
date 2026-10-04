package spotifyapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/logging"
)

type cooldownLogs struct {
	progressLogs
	once    sync.Once
	waiting chan struct{}
}

func (l *cooldownLogs) Write(p []byte) (int, error) {
	n, e := l.progressLogs.Write(p)
	if strings.Contains(string(p), "rate limit waiting") {
		l.once.Do(func() { close(l.waiting) })
	}
	return n, e
}

func TestReadRateLimitRetriesBounded(t *testing.T) {
	for _, tc := range []struct {
		name, header string
		always       bool
		want         int
	}{
		{"retry", "1", false, 2}, {"exhausted", "0", true, 3}, {"missing", "", true, 1}, {"too-long", "999999", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				n := requests.Add(1)
				if tc.always || n == 1 {
					w.Header().Set("Retry-After", tc.header)
					w.WriteHeader(429)
					fmt.Fprint(w, `{"error":{"status":429,"message":"PRIVATE_RESPONSE"}}`)
					return
				}
				fmt.Fprintf(w, `{"items":[],"next":null,"offset":0,"limit":%d,"total":0}`, fixtureCatalogueLimit(r))
			}))
			defer server.Close()
			var logs progressLogs
			ctx := logging.WithContext(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))
			start := time.Now()
			_, err := newClient(server.Client(), server.URL+"/").PlaylistTracks(ctx, "p")
			if requests.Load() != int32(tc.want) {
				t.Errorf("requests=%d want%d", requests.Load(), tc.want)
			}
			if !tc.always && err != nil {
				t.Error(err)
			}
			if tc.always && err == nil {
				t.Error("exhausted rate limit ignored")
			}
			if tc.want > 1 && time.Since(start) < time.Second {
				t.Error("did not respect cooldown")
			}
			if tc.want > 1 && !strings.Contains(logs.String(), "seconds=1") {
				t.Errorf("missing safe wait warning: %s", logs.String())
			}
			if strings.Contains(logs.String(), "PRIVATE_RESPONSE") || strings.Contains(logs.String(), server.URL) {
				t.Fatal("rate-limit telemetry leaks")
			}
		})
	}
}

func TestWriteRateLimitNeverReplayed(t *testing.T) {
	for _, method := range []string{"POST", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Header().Set("Retry-After", "1")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(429)
				fmt.Fprint(w, `{"error":{"status":429,"message":"fixture"}}`)
			}))
			defer server.Close()
			c := newClient(server.Client(), server.URL+"/")
			var err error
			if method == "POST" {
				err = c.AddTracks(context.Background(), "p", "track")
			} else {
				err = c.RemoveTracks(context.Background(), "p", "track")
			}
			if err == nil || requests != 1 {
				t.Fatalf("write retried: requests=%d err=%v", requests, err)
			}
		})
	}
}

func TestReadCooldownCancellation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "1")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"status":429,"message":"fixture"}}`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := newClient(server.Client(), server.URL+"/").ArtistAlbums(ctx, "a")
	if !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 1 {
		t.Fatalf("wait not cancellable: requests=%d err=%v", requests.Load(), err)
	}
}

func TestRecoverySuccessDoesNotEraseNewerCooldown(t *testing.T) {
	limiter := newReadTransport(http.DefaultTransport)
	limiter.finish(false, time.Second, true)
	limiter.mu.Lock()
	limiter.until = time.Now().Add(-time.Second)
	limiter.mu.Unlock()
	probe, err := limiter.wait(context.Background())
	if err != nil || !probe {
		t.Fatalf("probe=%t err=%v", probe, err)
	}
	// Another read that was already in flight receives a later 429.
	limiter.finish(false, 2*time.Second, true)
	limiter.finish(probe, 0, false)
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if !limiter.recovery || time.Until(limiter.until) < time.Second {
		t.Fatal("successful probe erased newer shared cooldown")
	}
}

func TestSharedCooldownSingleRecoveryProbe(t *testing.T) {
	var requests atomic.Int32
	probes := make(chan struct{}, 8)
	release := make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	defer resume()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			fmt.Fprint(w, `{"error":{"status":429,"message":"fixture"}}`)
			return
		}
		probes <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprintf(w, `{"items":[],"next":null,"offset":0,"limit":%d,"total":0}`, fixtureCatalogueLimit(r))
	}))
	defer server.Close()
	logs := &cooldownLogs{waiting: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ctx = logging.WithContext(ctx, slog.New(slog.NewTextHandler(logs, nil)))
	c := newClient(server.Client(), server.URL+"/")
	done := make(chan error, 3)
	go func() { _, e := c.ArtistAlbums(ctx, "a"); done <- e }()
	select {
	case <-logs.waiting:
	case <-time.After(time.Second):
		cancel()
		resume()
		<-done
		t.Fatal("missing shared read cooldown")
	}
	go func() { _, e := c.AlbumTracks(ctx, "b"); done <- e }()
	go func() { _, e := c.PlaylistTracks(ctx, "p"); done <- e }()
	select {
	case <-probes:
	case <-ctx.Done():
		resume()
		t.Fatal("no recovery probe")
	}
	select {
	case <-probes:
		t.Error("workers stampeded after cooldown")
	case <-time.After(100 * time.Millisecond):
	}
	resume()
	for i := 0; i < 3; i++ {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	if requests.Load() != 4 {
		t.Errorf("requests=%d want4", requests.Load())
	}
}
