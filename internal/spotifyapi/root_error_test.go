package spotifyapi

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/app"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/diagnostics"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSafeRootWorkerFailurePreserved(t *testing.T) {
	sibling := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/artists/sibling/"):
			close(sibling)
			<-r.Context().Done()
			return
		case strings.Contains(r.URL.Path, "/artists/root/"):
			<-sibling
			w.WriteHeader(403)
			fmt.Fprint(w, `{"error":{"status":403,"message":"SECRET_BODY"}}`)
			return
		default:
			fmt.Fprint(w, `{"items":[],"next":null}`)
		}
	}))
	defer server.Close()
	err := app.FillWithConcurrency(diagnostics.WithPlaylist(context.Background(), 3), newClient(server.Client(), server.URL+"/"), config.Playlist{ID: "p", Artists: []config.Artist{{ID: "sibling"}, {ID: "root"}}}, 2)
	assertDiagnostic(t, err, "artist_albums", "http", 403, true)
	var d diagnosticError
	if !errors.As(err, &d) {
		t.Fatal("missing diagnostic")
	}
	fields := fmt.Sprint(d.DiagnosticFields())
	if !strings.Contains(fields, "artist 2") || !strings.Contains(fields, "playlist 3") {
		t.Fatalf("wrong root context: %s", fields)
	}
}
