package spotifyapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// A writer changing the disk after preflight must not change the consumer's
// already selected snapshot, even when the replacement is independently valid.
func TestCatalogueSnapshotPinsBodiesBeforeConsumption(t *testing.T) {
	var base string
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		_ = json.NewEncoder(w).Encode(chainPage(base, offset, 51, false))
	}))
	defer s.Close()
	base = s.URL
	c := newClient(s.Client(), base+"/")
	if err := c.EnableCatalogueCache(filepath.Join(t.TempDir(), "cache"), time.Hour, "app", "user"); err != nil {
		t.Fatal(err)
	}
	if got, err := c.AlbumTracks(context.Background(), "a"); err != nil || len(got) != 51 {
		t.Fatalf("warm: %v %v", got, err)
	}
	transport := c.http.Transport.(apiTransport).next.(*catalogueTransport)
	ctx := catalogueContext(context.Background(), base+"/albums/a/tracks")
	root, _ := http.NewRequestWithContext(ctx, "GET", base+"/albums/a/tracks?limit=50", nil)
	resp, err := transport.RoundTrip(root)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	tail, _ := http.NewRequestWithContext(ctx, "GET", base+"/albums/a/tracks?limit=50&offset=50", nil)
	rec, err := transport.store.loadRecord(transport.store.key(tail))
	if err != nil {
		t.Fatal(err)
	}
	rec.Body, _ = json.Marshal(chainPage(base, 50, 51, true))
	if err := transport.store.saveRecord(rec); err != nil {
		t.Fatal(err)
	}
	resp, err = transport.RoundTrip(tail)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &page); err != nil || len(page.Items) != 1 || page.Items[0].ID != "t50" || calls.Load() != 2 {
		t.Fatalf("unpinned snapshot: body=%s err=%v calls=%d", body, err, calls.Load())
	}
}
