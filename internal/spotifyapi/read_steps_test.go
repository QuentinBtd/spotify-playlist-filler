package spotifyapi

import (
	"context"
	"fmt"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/app"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSafeOtherReadSteps(t *testing.T) {
	for _, op := range []string{"read_playlist", "search_artist", "album_tracks"} {
		t.Run(op, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fail := (op == "read_playlist" && strings.Contains(r.URL.Path, "/playlists/")) || (op == "search_artist" && strings.Contains(r.URL.Path, "/search")) || (op == "album_tracks" && strings.Contains(r.URL.Path, "/albums/"))
				if fail {
					w.WriteHeader(400)
					fmt.Fprint(w, `{"error":{"status":400,"message":"SECRET_BODY"}}`)
					return
				}
				if strings.Contains(r.URL.Path, "/artists/") {
					fmt.Fprintf(w, `{"items":[{"id":"album"}],"next":null,"offset":0,"limit":%d,"total":1}`, fixtureCatalogueLimit(r))
					return
				}
				fmt.Fprintf(w, `{"items":[],"next":null,"offset":0,"limit":%d,"total":0}`, fixtureCatalogueLimit(r))
			}))
			defer server.Close()
			p := config.Playlist{ID: "p", Artists: []config.Artist{{ID: "a", Name: "PRIVATE_ARTIST", UseNameInsteadOfURI: op == "search_artist"}}}
			err := app.Fill(context.Background(), newClient(server.Client(), server.URL+"/"), p)
			assertDiagnostic(t, err, op, "http", 400, true)
		})
	}
}
func TestSafeInvalidResponseStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `SECRET_BODY`) }))
	defer server.Close()
	err := app.Fill(context.Background(), newClient(server.Client(), server.URL+"/"), config.Playlist{ID: "p"})
	assertDiagnostic(t, err, "read_playlist", "invalid_response", 200, true)
}
