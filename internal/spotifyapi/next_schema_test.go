package spotifyapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlaylistPageRequiresNext(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"items":[]}`) }))
	defer s.Close()
	tracks, err := newClient(s.Client(), s.URL+"/").PlaylistTracks(context.Background(), "p")
	if err == nil || tracks != nil {
		t.Fatalf("missing next accepted: tracks=%v err=%v", tracks, err)
	}
}
