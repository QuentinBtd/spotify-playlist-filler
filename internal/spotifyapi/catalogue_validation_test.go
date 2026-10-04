package spotifyapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCatalogueInvalidPagesNeverBecomeComplete(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `{"items":null,"next":null}`, `{"items":[],"next":null} {}`, `{"items":[{}],"next":null}`, `{"items":[],"next":12}`, `{"items":[],"next":""}`, `{"items":[],"next":"https://elsewhere.invalid/albums/a/tracks"}`, `{"items":[],"next":null,"error":{"message":"bad"}}`, `{"items":[{"id":"one"}],"total":2,"offset":0,"next":null}`, `{"items":[],"next":"?offset=1&access_token=PRIVATE_TOKEN"}`, `{"items":[],"next":null,"total":null}`} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, body) }))
			defer s.Close()
			dir := t.TempDir() + "/cache"
			for range 2 {
				c := newClient(s.Client(), s.URL+"/")
				if e := c.EnableCatalogueCache(dir, time.Hour, "app", "user"); e != nil {
					t.Fatal(e)
				}
				if got, e := c.AlbumTracks(context.Background(), "a"); e == nil || got != nil {
					t.Fatalf("invalid catalogue accepted: %v %v", got, e)
				}
			}
			if calls != 2 {
				t.Fatalf("invalid page cached: %d", calls)
			}
		})
	}
}
func TestCatalogueTTLIsolationDisableAndCorruption(t *testing.T) {
	var calls int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintf(w, `{"items":[],"next":null,"offset":0,"limit":%d,"total":0}`, fixtureCatalogueLimit(r))
	}))
	defer s.Close()
	dir := t.TempDir() + "/cache"
	read := func(app, user string, ttl time.Duration) {
		t.Helper()
		c := newClient(s.Client(), s.URL+"/")
		if e := c.EnableCatalogueCache(dir, ttl, app, user); e != nil {
			t.Fatal(e)
		}
		if _, e := c.AlbumTracks(context.Background(), "a"); e != nil {
			t.Fatal(e)
		}
	}
	read("app", "user", time.Hour)
	read("app", "user", time.Hour)
	if calls != 1 {
		t.Fatalf("restart calls=%d", calls)
	}
	read("other-app", "user", time.Hour)
	read("app", "other-user", time.Hour)
	read("app", "user", 0)
	if calls != 4 {
		t.Fatalf("isolation/disable calls=%d", calls)
	}
	time.Sleep(time.Millisecond)
	read("app", "user", time.Nanosecond)
	if calls != 5 {
		t.Fatal("expired entry reused")
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{`{`, `{"version":999}`, `null`, `{} {}`} {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".json") {
				if e := os.WriteFile(filepath.Join(dir, entry.Name()), []byte(bad), 0600); e != nil {
					t.Fatal(e)
				}
			}
		}
		before := calls
		read("app", "user", time.Hour)
		if calls != before+1 {
			t.Fatalf("corrupt entry reused: %q", bad)
		}
	}
}
func TestCatalogueUnsafePathsFailClosed(t *testing.T) {
	parent := t.TempDir()
	real := filepath.Join(parent, "real")
	if e := os.Mkdir(real, 0700); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(parent, "link")
	if e := os.Symlink(real, link); e != nil {
		t.Fatal(e)
	}
	for _, dir := range []string{link, link + "/../cache", parent + "/../cache"} {
		c := newClient(&http.Client{}, "http://127.0.0.1:1/")
		if e := c.EnableCatalogueCache(dir, time.Hour, "app", "user"); e == nil {
			t.Fatalf("unsafe path accepted: %s", dir)
		}
	}
}
func TestCatalogueStripsUnrelatedResponseFields(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"items":[{"id":"track","name":"PRIVATE_NAME","token":"PRIVATE_TOKEN"}],"next":null,"offset":0,"limit":%d,"total":1,"secret":"PRIVATE_TOKEN"}`, fixtureCatalogueLimit(r))
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
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		data, e := os.ReadFile(filepath.Join(dir, entry.Name()))
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(data), "PRIVATE_") {
			t.Fatalf("unrelated fields persisted: %s", data)
		}
	}
}
