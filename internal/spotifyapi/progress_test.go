package spotifyapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/app"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/logging"
)

type progressLogs struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (l *progressLogs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buffer.Write(p)
}
func (l *progressLogs) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.buffer.String() }

// Pause real local HTTP catalogue reads without sleeps, and inspect logs while
// the request is in flight. This reproduces the apparent freeze after /items.
func TestCatalogueDebugPagination(t *testing.T) {
	var logs progressLogs
	var base string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		kind, field := "artist albums", "albums"
		if strings.HasPrefix(r.URL.Path, "/albums/") {
			kind, field = "album tracks", "tracks"
		}
		page := 1
		previous := 0
		if r.URL.Query().Get("page") == "2" {
			page = 2
			previous = 2
		}
		want := fmt.Sprintf("msg=\"%s page read started\" page=%d cumulative_%s=%d", kind, page, field, previous)
		if !strings.Contains(logs.String(), want) {
			t.Errorf("request started without debug stage %q in %s", want, logs.String())
		}
		if page == 2 {
			fmt.Fprint(w, `{"items":[{"id":"third"}],"next":null}`)
			return
		}
		fmt.Fprintf(w, `{"items":[{"id":"first"},{"id":"second"}],"next":%q}`, base+r.URL.Path+"?page=2")
	}))
	defer server.Close()
	base = server.URL
	ctx := logging.WithContext(context.Background(), slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	c := newClient(server.Client(), base+"/")
	if albums, err := c.ArtistAlbums(ctx, "a"); err != nil || len(albums) != 3 {
		t.Fatalf("albums=%v err=%v", albums, err)
	}
	if tracks, err := c.AlbumTracks(ctx, "a"); err != nil || len(tracks) != 3 {
		t.Fatalf("tracks=%v err=%v", tracks, err)
	}
	for _, want := range []string{
		`msg="artist albums page" page=1 albums=2 cumulative_albums=2`,
		`msg="artist albums page" page=2 albums=1 cumulative_albums=3`,
		`msg="album tracks page" page=1 tracks=2 cumulative_tracks=2`,
		`msg="album tracks page" page=2 tracks=1 cumulative_tracks=3`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("missing %q in %s", want, logs.String())
		}
	}
	if strings.Contains(logs.String(), server.URL) || strings.Contains(logs.String(), "first") {
		t.Fatal("URL/ID in telemetry")
	}
}

func TestCatalogueProgressWhileWaiting(t *testing.T) {
	for _, cancelRead := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelRead), func(t *testing.T) {
			var logs progressLogs
			entered := make(chan string, 2)
			release := make(chan struct{})
			var releaseOnce sync.Once
			resume := func() { releaseOnce.Do(func() { close(release) }) }
			defer resume()
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/playlists/p/items":
					fmt.Fprint(w, `{"items":[{"item":{"type":"track","id":"keep"}},{"item":null}],"next":null}`)
				case "/artists/a/albums":
					entered <- "artist"
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
					fmt.Fprint(w, `{"items":[{"id":"album"}],"next":null}`)
				case "/albums/album/tracks":
					entered <- "album"
					fmt.Fprint(w, `{"items":[{"id":"keep"}],"next":null}`)
				default:
					writes++
					http.Error(w, "unexpected write", 500)
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx = logging.WithContext(ctx, slog.New(slog.NewTextHandler(&logs, nil)))
			done := make(chan error, 1)
			go func() {
				done <- app.Fill(ctx, newClient(server.Client(), server.URL+"/"), config.Playlist{ID: "p", Artists: []config.Artist{{ID: "a", Name: "PRIVATE_ARTIST"}}})
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				cancel()
				t.Fatal("fixture was not reached")
			}
			during := logs.String()
			for _, want := range []string{"playlist read complete", "supported_tracks=1", "total_items=2", "artist catalogue started", "artist=1", "artists=1", "artist albums read started"} {
				if !strings.Contains(during, want) {
					t.Errorf("no progress before catalogue response: missing %q in %s", want, during)
				}
			}
			if cancelRead {
				cancel()
			}
			resume()
			select {
			case err := <-done:
				if cancelRead && !errors.Is(err, context.Canceled) {
					t.Errorf("cancellation lost: %v", err)
				}
				if !cancelRead && err != nil {
					t.Error(err)
				}
			case <-time.After(2 * time.Second):
				cancel()
				t.Fatal("fill did not finish")
			}
			if writes != 0 {
				t.Errorf("unexpected writes=%d", writes)
			}
			if !cancelRead {
				for _, want := range []string{"artist albums read complete", "album tracks read started", "album tracks read complete", "artist catalogue complete", "albums_read=1", "tracks=1"} {
					if !strings.Contains(logs.String(), want) {
						t.Errorf("missing %q: %s", want, logs.String())
					}
				}
			}
			if strings.Contains(logs.String(), "PRIVATE_ARTIST") || strings.Contains(logs.String(), server.URL) {
				t.Fatal("private data logged")
			}
		})
	}
}
