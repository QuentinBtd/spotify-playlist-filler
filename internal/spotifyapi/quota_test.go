package spotifyapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/diagnostics"
)

func TestCatalogueLongQuotaCooldownPersistsButFreshCacheWins(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if strings.Contains(r.URL.Path, "/warm/") {
			fmt.Fprintf(w, `{"items":[{"id":"retained"}],"next":null,"offset":0,"limit":%d,"total":1}`, fixtureCatalogueLimit(r))
			return
		}
		w.Header().Set("Retry-After", "80982")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"status":429,"reason":"QUOTA_EXCEEDED","message":"PRIVATE_BODY"}}`)
	}))
	defer s.Close()
	dir := t.TempDir() + "/cache"
	client := func() *Client {
		c := newClient(s.Client(), s.URL+"/")
		if e := c.EnableCatalogueCache(dir, time.Hour, "app", "user"); e != nil {
			t.Fatal(e)
		}
		return c
	}
	if _, e := client().AlbumTracks(context.Background(), "warm"); e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	_, e := client().AlbumTracks(context.Background(), "limited")
	if e == nil {
		t.Fatal("429 ignored")
	}
	fields := fmt.Sprint(diagnostics.Fields(e))
	for _, want := range []string{"http_status 429", "reason QUOTA_EXCEEDED", "retry_after_seconds 80982", "retry_at"} {
		if !strings.Contains(fields, want) {
			t.Errorf("missing %q: %s", want, fields)
		}
	}
	if strings.Contains(fields, "PRIVATE_BODY") {
		t.Fatal("unsafe diagnostic")
	}
	c := client()
	if got, e := c.AlbumTracks(context.Background(), "warm"); e != nil || len(got) != 1 {
		t.Fatalf("cache blocked by cooldown: %v %v", got, e)
	}
	if _, e := c.AlbumTracks(context.Background(), "unfetched"); e == nil {
		t.Fatal("saved cooldown not enforced")
	}
	if calls != 2 {
		t.Fatalf("cooldown restart hammer: calls=%d", calls)
	}
	if time.Since(start) > time.Second {
		t.Fatal("long wait rather than immediate failure")
	}
}
func TestRateLimitReasonAllowlist(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "80982")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"status":429,"reason":"PRIVATE_REASON","message":"PRIVATE_BODY"}}`)
	}))
	defer s.Close()
	_, e := newClient(s.Client(), s.URL+"/").AlbumTracks(context.Background(), "a")
	fields := fmt.Sprint(diagnostics.Fields(e))
	if strings.Contains(fields, "PRIVATE") {
		t.Fatal(fields)
	}
	if !strings.Contains(fields, "retry_after_seconds 80982") {
		t.Fatal(fields)
	}
}
