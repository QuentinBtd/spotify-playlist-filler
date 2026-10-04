package spotifyapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCatalogueGuardRemainsBeforeUnderlyingTransport(t *testing.T) {
	var calls atomic.Int32
	hc := &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, fmt.Errorf("unexpected underlying request")
	})}
	c := New(hc)
	if e := c.EnableCatalogueCache(t.TempDir()+"/cache", time.Hour, "app", "user"); e != nil {
		t.Fatal(e)
	}
	for _, endpoint := range []string{"http://api.spotify.com/v1/albums/a/tracks", "https://api.spotify.com.evil.invalid/v1/albums/a/tracks", "https://api.spotify.com:444/v1/albums/a/tracks", "https://fixture@api.spotify.com/v1/albums/a/tracks"} {
		ctx := catalogueContext(context.Background(), "https://api.spotify.com/v1/albums/a/tracks")
		r, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if _, e := c.http.Do(r); e == nil {
			t.Fatal("unsafe destination accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("underlying OAuth-side requests=%d", calls.Load())
	}
}
func TestCatalogueExpiryNeverFallsBackOnRemoteError(t *testing.T) {
	var fail atomic.Bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"error":{"status":503,"message":"fixture"}}`)
			return
		}
		fmt.Fprintf(w, `{"items":[{"id":"stale"}],"next":null,"offset":0,"limit":%d,"total":1}`, fixtureCatalogueLimit(r))
	}))
	defer s.Close()
	dir := t.TempDir() + "/cache"
	c := newClient(s.Client(), s.URL+"/")
	if e := c.EnableCatalogueCache(dir, time.Hour, "app", "user"); e != nil {
		t.Fatal(e)
	}
	if _, e := c.AlbumTracks(context.Background(), "a"); e != nil {
		t.Fatal(e)
	}
	fail.Store(true)
	c = newClient(s.Client(), s.URL+"/")
	if e := c.EnableCatalogueCache(dir, time.Nanosecond, "app", "user"); e != nil {
		t.Fatal(e)
	}
	if got, e := c.AlbumTracks(context.Background(), "a"); e == nil || got != nil {
		t.Fatalf("stale fallback: %v %v", got, e)
	}
}
