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

func TestCatalogueCacheRestartRefetchesIncompleteEntity(t *testing.T) {
	var first, second atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	var base string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") == "50" {
			second.Add(1)
			if fail.Load() {
				w.WriteHeader(503)
				fmt.Fprint(w, `{"error":{"status":503,"message":"fixture"}}`)
				return
			}
			fmt.Fprint(w, `{"items":[{"id":"two"}],"next":null,"offset":50,"limit":50,"total":51}`)
			return
		}
		first.Add(1)
		fmt.Fprintf(w, `{"items":[%s],"next":%q,"offset":0,"limit":50,"total":51}`, fixtureRepeatedItems("one", 50), base+"/albums/a/tracks?limit=50&offset=50")
	}))
	defer s.Close()
	base = s.URL
	dir := t.TempDir() + "/catalogue"
	client := func() *Client {
		c := newClient(s.Client(), base+"/")
		if e := c.EnableCatalogueCache(dir, time.Hour, "app", "user"); e != nil {
			t.Fatal(e)
		}
		return c
	}
	if got, e := client().AlbumTracks(context.Background(), "a"); e == nil || got != nil {
		t.Fatalf("partial usable: %v %v", got, e)
	}
	fail.Store(false)
	if got, e := client().AlbumTracks(context.Background(), "a"); e != nil || len(got) != 51 {
		t.Fatalf("resume: %v %v", got, e)
	}
	if got, e := client().AlbumTracks(context.Background(), "a"); e != nil || len(got) != 51 {
		t.Fatalf("restart: %v %v", got, e)
	}
	if first.Load() != 2 || second.Load() != 2 {
		t.Fatalf("calls first=%d second=%d; incomplete entity not fully refreshed", first.Load(), second.Load())
	}
}
