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

func TestSafeCatalogueObservedStatus(t *testing.T) {
	for _, body := range []string{`{"error":{"message":"SECRET_BODY"}}`, `SECRET_BODY`, ""} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/artists/") {
					w.WriteHeader(403)
					fmt.Fprint(w, body)
					return
				}
				fmt.Fprint(w, `{"items":[],"next":null}`)
			}))
			defer server.Close()
			err := app.Fill(context.Background(), newClient(server.Client(), server.URL+"/"), config.Playlist{ID: "p", Artists: []config.Artist{{ID: "a"}}})
			assertDiagnostic(t, err, "artist_albums", "http", 403, true)
		})
	}
}
