package spotifyapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/zmb3/spotify/v2"
)

func TestPlaylistReadRetainsRawCount(t *testing.T) {
	var base string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `{"items":[{"item":{"type":"episode","id":"episode"}}],"next":null}`)
			return
		}
		fmt.Fprintf(w, `{"items":[{"item":null},{"item":{"type":"track","id":"keep"}},{"item":{"type":"track","id":"keep"}}],"next":%q}`, base+"/playlists/p/items?page=2")
	}))
	defer server.Close()
	base = server.URL
	c := newClient(server.Client(), base+"/")
	counted, ok := any(c).(interface {
		PlaylistTracksWithCount(context.Context, spotify.ID) ([]spotify.ID, int, error)
	})
	if !ok {
		t.Fatal("adapter cannot retain original item count for capacity preflight")
	}
	tracks, total, err := counted.PlaylistTracksWithCount(context.Background(), "p")
	if err != nil || total != 4 || !reflect.DeepEqual(tracks, []spotify.ID{"keep", "keep"}) {
		t.Fatalf("tracks=%v total=%d err=%v", tracks, total, err)
	}
}
