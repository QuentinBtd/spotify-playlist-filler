package spotifyapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/app"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
)

func TestPlaylistSchemaFailureDiscardsEveryPage(t *testing.T) {
	bodies := []string{
		`null`, `{}`, `[]`, `{"next":null}`,
		`{"items":null,"next":null}`, `{"items":{},"next":null}`,
		`{"items":[null],"next":null}`, `{"items":[{}],"next":null}`,
		`{"items":[{"track":{"type":"track","id":"legacy"}}],"next":null}`,
		`{"items":[{"is_local":true}],"next":null}`,
		`{"items":[{"item":17}],"next":null}`,
		`{"items":[{"item":{"id":17}}],"next":null}`,
		`{"items":[{"item":{"type":false}}],"next":null}`,
		`{"items":[{"is_local":"true","item":null}],"next":null}`,
		`{"items":[{"is_local":null,"item":null}],"next":null}`,
		`{"items":[{"item":{"is_local":null}}],"next":null}`,
		`{"items":[{"item":{"type":null}}],"next":null}`,
		`{"items":[],"next":42}`, `{"items":[],"next":{}}`,
		`{"items":[],"next":""}`, `{"items":[],"next":"%zz"}`,
		`{"items":[],"next":"?offset=1;bad=2"}`,
		`{"items":[],"next":null} trailing-garbage`,
		`{"items":[],"next":null} {"items":[],"next":null}`,
	}
	for _, page := range []int{1, 2} {
		for _, body := range bodies {
			t.Run(fmt.Sprintf("page%d/%s", page, body), func(t *testing.T) {
				calls := 0
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if calls < page {
						fmt.Fprint(w, `{"items":[{"item":{"type":"track","id":"partial"}}],"next":"?offset=1"}`)
						return
					}
					fmt.Fprint(w, body)
				}))
				defer s.Close()
				tracks, err := newClient(s.Client(), s.URL+"/").PlaylistTracks(context.Background(), "p")
				if err == nil || tracks != nil || calls != page {
					t.Fatalf("tracks=%v err=%v calls=%d", tracks, err, calls)
				}
			})
		}
	}
}

func TestValidEmptyAndUnavailableItems(t *testing.T) {
	for _, body := range []string{`{"items":[],"next":null}`, `{"items":[{"item":null},{"is_local":true,"item":null},{"is_local":true,"item":{"type":"track","id":null,"is_local":true}},{"item":{"type":"episode"}},{"item":{"type":"future-type"}}],"next":null}`} {
		t.Run(body, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer s.Close()
			tracks, err := newClient(s.Client(), s.URL+"/").PlaylistTracks(context.Background(), "p")
			if err != nil || len(tracks) != 0 {
				t.Fatalf("tracks=%v err=%v", tracks, err)
			}
		})
	}
}

func TestFillSchemaFailureMakesNoMutation(t *testing.T) {
	var mutations atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			mutations.Add(1)
			fmt.Fprint(w, `{"snapshot_id":"fixture"}`)
			return
		}
		if r.URL.Query().Get("offset") == "1" {
			fmt.Fprint(w, `{"items":[{"track":{"type":"track","id":"legacy"}}]}`)
			return
		}
		fmt.Fprint(w, `{"items":[{"item":{"type":"track","id":"partial"}}],"next":"?offset=1"}`)
	}))
	defer s.Close()
	err := app.Fill(context.Background(), newClient(s.Client(), s.URL+"/"), config.Playlist{ID: "p", ShuffleOrder: true})
	if err == nil || mutations.Load() != 0 {
		t.Fatalf("err=%v mutations=%d", err, mutations.Load())
	}
}
