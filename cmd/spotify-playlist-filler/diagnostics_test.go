package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/app"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/diagnostics"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/spotifyapi"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Rewrite only inside this fixture transport; requests never leave loopback.
type diagnosticFixtureTransport struct {
	base   http.RoundTripper
	target *url.URL
}

func (t diagnosticFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = t.target.Scheme
	u.Host = t.target.Host
	clone.URL = &u
	return t.base.RoundTrip(clone)
}
func TestSynchronizationDiagnosticLogFixture(t *testing.T) {
	for _, status := range []int{403, 400, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/artists/") {
					w.WriteHeader(status)
					fmt.Fprintf(w, `{"error":{"status":%d,"message":"SECRET_BODY SECRET_TOKEN PRIVATE_ARTIST"}}`, status)
					return
				}
				fmt.Fprint(w, `{"items":[],"next":null}`)
			}))
			defer server.Close()
			target, _ := url.Parse(server.URL)
			client := spotifyapi.New(&http.Client{Transport: diagnosticFixtureTransport{base: server.Client().Transport, target: target}})
			err := app.Fill(diagnostics.WithPlaylist(context.Background(), 2), client, config.Playlist{ID: "PRIVATE_ID", Name: "PRIVATE_PLAYLIST", Artists: []config.Artist{{ID: "a", Name: "PRIVATE_ARTIST"}}})
			if err == nil {
				t.Fatal("fixture did not fail")
			}
			for _, prior := range []bool{false, true} {
				var logs bytes.Buffer
				logSynchronizationFailure(slog.New(slog.NewTextHandler(&logs, nil)), err, prior)
				text := logs.String()
				for _, want := range []string{"stage=synchronization", "operation=artist_albums", fmt.Sprintf("http_status=%d", status), "failure_kind=http", "playlist=2", "artist=1"} {
					if !strings.Contains(text, want) {
						t.Fatalf("missing %s: %s", want, text)
					}
				}
				if strings.Count(text, "level=ERROR") != 1 {
					t.Fatalf("wrong ERROR count: %s", text)
				}
				for _, secret := range []string{"SECRET_BODY", "SECRET_TOKEN", "PRIVATE_ID", "PRIVATE_PLAYLIST", "PRIVATE_ARTIST", "http://", "api.spotify.com"} {
					if strings.Contains(text, secret) {
						t.Fatal("unsafe error log")
					}
				}
				if strings.Contains(text, "playlist unchanged") == prior {
					t.Fatalf("wrong prewrite guidance: %s", text)
				}
				t.Log(strings.TrimSpace(text))
			}
		})
	}
}
