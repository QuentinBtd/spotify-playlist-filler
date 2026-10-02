package spotifyapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/app"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
)

func TestConfiguredReadConcurrencyLimits(t *testing.T) {
	for _, limit := range []int{1, 2, 8} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			var active, maximum atomic.Int32
			entered := make(chan struct{}, 8)
			release := make(chan struct{})
			var once sync.Once
			resume := func() { once.Do(func() { close(release) }) }
			defer resume()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasPrefix(r.URL.Path, "/artists/") {
					n := active.Add(1)
					defer active.Add(-1)
					for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
					}
					entered <- struct{}{}
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				fmt.Fprint(w, `{"items":[],"next":null}`)
			}))
			defer server.Close()
			p := config.Playlist{ID: "p"}
			for i := 0; i < 8; i++ {
				p.Artists = append(p.Artists, config.Artist{ID: fmt.Sprintf("a%d", i)})
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- app.FillWithConcurrency(ctx, newClient(server.Client(), server.URL+"/"), p, limit) }()
			for i := 0; i < limit; i++ {
				select {
				case <-entered:
				case <-time.After(2 * time.Second):
					cancel()
					resume()
					<-done
					t.Fatal("configured worker count not reached")
				}
			}
			resume()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if maximum.Load() != int32(limit) {
				t.Fatalf("maximum=%d limit=%d", maximum.Load(), limit)
			}
		})
	}
}

func TestCatalogueBoundedConcurrentReads(t *testing.T) {
	for _, mode := range []string{"success", "error", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			var active, maximum atomic.Int32
			var sharedReads atomic.Int32
			entered := make(chan string, 8)
			released := make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(released) }) }
			defer release()
			writes := 0
			var added []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method != "GET" {
					writes++
					if r.Method == "POST" {
						var body struct {
							URIs []string `json:"uris"`
						}
						_ = json.NewDecoder(r.Body).Decode(&body)
						added = append(added, body.URIs...)
						w.WriteHeader(201)
					}
					fmt.Fprint(w, `{"snapshot_id":"fixture"}`)
					return
				}
				n := active.Add(1)
				defer active.Add(-1)
				for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
				}
				switch {
				case strings.HasPrefix(r.URL.Path, "/playlists/"):
					fmt.Fprint(w, `{"items":[],"next":null}`)
				case strings.HasPrefix(r.URL.Path, "/artists/"):
					id := strings.Split(r.URL.Path, "/")[2]
					entered <- id
					select {
					case <-released:
					case <-r.Context().Done():
						return
					}
					if mode == "error" && id == "a0" {
						http.Error(w, `{"error":{"status":403,"message":"fixture"}}`, 403)
						return
					}
					fmt.Fprintf(w, `{"items":[{"id":%q},{"id":"shared"},{"id":"excluded"}],"next":null}`, id)
				case strings.HasPrefix(r.URL.Path, "/albums/"):
					id := strings.Split(r.URL.Path, "/")[2]
					if id == "shared" {
						sharedReads.Add(1)
					}
					if id == "excluded" {
						t.Error("global excluded album fetched")
					}
					fmt.Fprintf(w, `{"items":[{"id":%q}],"next":null}`, id)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			p := config.Playlist{ID: "p", SkippedAlbums: []config.Album{{ID: "excluded"}}}
			for i := 0; i < 5; i++ {
				p.Artists = append(p.Artists, config.Artist{ID: fmt.Sprintf("a%d", i)})
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- app.Fill(ctx, newClient(server.Client(), server.URL+"/"), p) }()
			for i := 0; i < 3; i++ {
				select {
				case <-entered:
				case <-time.After(2 * time.Second):
					cancel()
					release()
					<-done
					t.Fatal("catalogue reads remain sequential; expected 3 in-flight GETs")
				}
			}
			if maximum.Load() != 3 {
				t.Errorf("max=%d want3", maximum.Load())
			}
			if mode == "cancel" {
				cancel()
			}
			release()
			select {
			case err := <-done:
				if mode == "success" && err != nil {
					t.Error(err)
				}
				if mode == "error" && err == nil {
					t.Error("read failure ignored")
				}
				if mode == "cancel" && !errors.Is(err, context.Canceled) {
					t.Errorf("cancellation lost: %v", err)
				}
			case <-time.After(2 * time.Second):
				cancel()
				t.Fatal("workers not drained")
			}
			if maximum.Load() > 3 {
				t.Errorf("unbounded GETs: %d", maximum.Load())
			}
			if mode != "success" && writes != 0 {
				t.Errorf("failed reads caused %d writes", writes)
			}
			if mode == "success" && !reflect.DeepEqual(added, []string{"spotify:track:a0", "spotify:track:shared", "spotify:track:a1", "spotify:track:a2", "spotify:track:a3", "spotify:track:a4"}) {
				t.Errorf("nondeterministic merge/dedup: %v", added)
			}
			if mode == "success" && sharedReads.Load() != 1 {
				t.Errorf("shared album fetched %d times", sharedReads.Load())
			}
		})
	}
}
