package spotifyapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/app"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
	"github.com/zmb3/spotify/v2"
)

func TestCataloguePartialFillResumeNeverWritesIncomplete(t *testing.T) {
	var first, second, artists, good, writes atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	var base string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes.Add(1)
			if r.Method == "POST" {
				w.WriteHeader(201)
			}
			fmt.Fprint(w, `{"snapshot_id":"fixture"}`)
			return
		}
		switch r.URL.Path {
		case "/playlists/p/items":
			fmt.Fprint(w, `{"items":[{"item":{"id":"obsolete","type":"track"}}],"next":null}`)
		case "/artists/a/albums":
			artists.Add(1)
			fmt.Fprintf(w, `{"items":[{"id":"good"},{"id":"paged"}],"next":null,"offset":0,"limit":%d,"total":2}`, fixtureCatalogueLimit(r))
		case "/albums/good/tracks":
			good.Add(1)
			fmt.Fprintf(w, `{"items":[{"id":"retained"}],"next":null,"offset":0,"limit":%d,"total":1}`, fixtureCatalogueLimit(r))
		case "/albums/paged/tracks":
			if r.URL.Query().Get("offset") == "50" {
				second.Add(1)
				if fail.Load() {
					w.Header().Set("Retry-After", "80982")
					w.WriteHeader(429)
					fmt.Fprint(w, `{"error":{"status":429,"reason":"QUOTA_EXCEEDED","message":"fixture"}}`)
					return
				}
				fmt.Fprintf(w, `{"items":[{"id":"two"}],"next":null,"offset":50,"limit":50,"total":51}`)
				return
			}
			first.Add(1)
			fmt.Fprintf(w, `{"items":[%s],"next":%q,"offset":0,"limit":50,"total":51}`, fixtureRepeatedItems("one", 50), base+"/albums/paged/tracks?limit=50&offset=50")
		default:
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	base = s.URL
	dir := t.TempDir() + "/cache"
	fill := func() error {
		c := newClient(s.Client(), base+"/")
		if e := c.EnableCatalogueCache(dir, time.Hour, "app", "user"); e != nil {
			return e
		}
		return app.Fill(context.Background(), c, config.Playlist{ID: "p", Artists: []config.Artist{{ID: "a"}}})
	}
	if e := fill(); e == nil || writes.Load() != 0 {
		t.Fatalf("partial mutated: %v writes=%d", e, writes.Load())
	}
	// Advance the synthetic persisted cooldown without waiting a day.
	store := &catalogueStore{dir: dir, namespace: "app\x00user\x00" + base + "/\x00sdk-default-market-v1", ttl: time.Hour}
	deadline, _ := json.Marshal(quotaMetadata{Seconds: 80982, At: time.Now().Add(-time.Second), Reason: "QUOTA_EXCEEDED"})
	if e := store.save(store.cooldownKey(), deadline); e != nil {
		t.Fatal(e)
	}
	fail.Store(false)
	if e := fill(); e != nil {
		t.Fatal(e)
	}
	if first.Load() != 2 || second.Load() != 2 || artists.Load() != 1 || good.Load() != 1 || writes.Load() != 2 {
		t.Fatalf("first=%d second=%d artists=%d good=%d writes=%d", first.Load(), second.Load(), artists.Load(), good.Load(), writes.Load())
	}
}
func TestCatalogueConcurrencyCancellationAndFileSafety(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprintf(w, `{"items":[{"id":"track"}],"next":null,"offset":0,"limit":%d,"total":1}`, fixtureCatalogueLimit(r))
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
	c := client()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := c.AlbumTracks(context.Background(), spotify.ID(fmt.Sprintf("album%d", i))); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	c = client()
	for i := range 20 {
		if _, e := c.AlbumTracks(context.Background(), spotify.ID(fmt.Sprintf("album%d", i))); e != nil {
			t.Fatal(e)
		}
	}
	if calls.Load() != 20 {
		t.Fatalf("lost concurrent entries: %d", calls.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := c.AlbumTracks(ctx, "cancelled"); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if calls.Load() != 20 {
		t.Fatal("cancel reached server")
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		info, e := entry.Info()
		if e != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("cache permissions: %v", e)
		}
	}
	file := filepath.Join(dir, entries[0].Name())
	if e := os.Remove(file); e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(t.TempDir(), "target")
	if e := os.WriteFile(target, []byte(`{"items":[]}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(target, file); e != nil {
		t.Fatal(e)
	}
	before := calls.Load()
	rejected := false
	for i := range 20 {
		if _, e := c.AlbumTracks(context.Background(), spotify.ID(fmt.Sprintf("album%d", i))); e != nil {
			rejected = true
		}
	}
	if !rejected || calls.Load() != before {
		t.Fatal("symlink did not fail closed")
	}
}
func TestCatalogueCyclesRejectWithoutWaiting(t *testing.T) {
	var base string
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		next := base + "/albums/a/tracks?limit=50"
		if r.URL.Query().Get("offset") == "" {
			next += "&offset=50"
		}
		offset := 0
		if r.URL.Query().Get("offset") != "" {
			offset = 50
		}
		fmt.Fprintf(w, `{"items":[%s],"next":%q,"offset":%d,"limit":50,"total":101}`, fixtureRepeatedItems("partial", 50), next, offset)
	}))
	defer s.Close()
	base = s.URL
	c := newClient(s.Client(), base+"/")
	if e := c.EnableCatalogueCache(t.TempDir()+"/cache", time.Hour, "app", "user"); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	got, e := c.AlbumTracks(ctx, "a")
	if got != nil || e == nil || errors.Is(e, context.DeadlineExceeded) || !strings.Contains(e.Error(), "catalogue") || calls != 2 {
		t.Fatalf("cycle: %v %v calls=%d", got, e, calls)
	}
}
func TestCatalogueCacheBoundedFileAndStorage(t *testing.T) {
	dir := t.TempDir() + "/cache"
	s := &catalogueStore{dir: dir, namespace: "fixture", ttl: time.Hour}
	key := s.digest("entry")
	body := []byte(`{"items":[],"next":null}`)
	if e := s.save(key, body); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(filepath.Join(dir, key+".spf-catalogue.json"))
	if e != nil {
		t.Fatal(e)
	}
	data = append(data, []byte(strings.Repeat(" ", maxCatalogueEntry))...)
	if e := os.WriteFile(filepath.Join(dir, key+".spf-catalogue.json"), data, 0600); e != nil {
		t.Fatal(e)
	}
	if got, e := s.load(key); e == nil && got != nil {
		t.Fatal("oversize cache usable")
	}
	// A private sparse file exercises the aggregate limit without allocating RAM.
	f, e := os.OpenFile(filepath.Join(dir, "filler.json"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if e := f.Truncate(64 << 20); e != nil {
		t.Fatal(e)
	}
	f.Close()
	if e := s.save(s.digest("new"), body); e == nil {
		t.Fatal("unbounded disk cache")
	}
}
func TestCatalogueSavedCooldownExpiry(t *testing.T) {
	dir := t.TempDir() + "/cache"
	s := &catalogueStore{dir: dir, namespace: "fixture", ttl: time.Hour}
	data, _ := json.Marshal(quotaMetadata{Seconds: 80982, At: time.Now().Add(-time.Hour), Reason: "QUOTA_EXCEEDED"})
	if e := s.save(s.cooldownKey(), data); e != nil {
		t.Fatal(e)
	}
	meta, e := s.cooldown()
	if e != nil || time.Now().Before(meta.At) {
		t.Fatal("expired cooldown remains active")
	}
}
