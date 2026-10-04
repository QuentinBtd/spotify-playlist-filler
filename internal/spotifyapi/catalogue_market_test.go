package spotifyapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCatalogueMarketNamespaceIsolation(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintf(w, `{"items":[],"next":null,"offset":0,"limit":%d,"total":0}`, fixtureCatalogueLimit(r))
	}))
	defer s.Close()
	dir := t.TempDir() + "/cache"
	for _, market := range []string{"US", "FR", "US"} {
		c := newClient(s.Client(), s.URL+"/")
		if e := c.EnableCatalogueCache(dir, time.Hour, "app", "user", market); e != nil {
			t.Fatal(e)
		}
		if _, e := c.AlbumTracks(context.Background(), "a"); e != nil {
			t.Fatal(e)
		}
	}
	if calls != 2 {
		t.Fatalf("market isolation: %d", calls)
	}
}
