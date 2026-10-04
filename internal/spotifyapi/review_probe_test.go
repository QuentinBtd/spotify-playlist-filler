package spotifyapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/app"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
)

// Independent fixture: only page zero expires; Spotify then reorders 51 tracks.
func TestIndependentRejectMixedGenerationBeforeDelete(t *testing.T) {
	for _, mode := range []string{"expired", "missing"} {
		t.Run(mode, func(t *testing.T) {
			changed := false
			firstCalls, tailCalls, writes := 0, 0, 0
			var deleted string
			var base string
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					writes++
					if r.Method == "DELETE" {
						b, _ := io.ReadAll(r.Body)
						deleted = string(b)
					}
					if r.Method == "POST" {
						w.WriteHeader(201)
					}
					fmt.Fprint(w, `{"snapshot_id":"fixture"}`)
					return
				}
				switch r.URL.Path {
				case "/playlists/p/items":
					fmt.Fprint(w, `{"items":[{"item":{"id":"t0","type":"track"}},{"item":{"id":"t50","type":"track"}}],"next":null}`)
				case "/artists/a/albums":
					fmt.Fprint(w, `{"items":[{"id":"album"}],"next":null,"offset":0,"limit":10,"total":1}`)
				case "/albums/album/tracks":
					if r.URL.Query().Get("offset") == "50" {
						tailCalls++
						id := "t50"
						if changed {
							id = "t0"
						}
						fmt.Fprintf(w, `{"items":[{"id":%q}],"next":null,"offset":50,"limit":50,"total":51}`, id)
					} else {
						firstCalls++
						items := make([]map[string]string, 50)
						for i := range items {
							n := i
							if changed {
								n++
							}
							items[i] = map[string]string{"id": fmt.Sprintf("t%d", n)}
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "next": base + "/albums/album/tracks?limit=50&offset=50", "offset": 0, "limit": 50, "total": 51})
					}
				default:
					w.WriteHeader(404)
				}
			}))
			defer s.Close()
			base = s.URL
			dir := filepath.Join(t.TempDir(), "catalogue")
			c := newClient(s.Client(), base+"/")
			if err := c.EnableCatalogueCache(dir, time.Hour, "app", "user"); err != nil {
				t.Fatal(err)
			}
			if got, err := c.AlbumTracks(context.Background(), "album"); err != nil || len(got) != 51 {
				t.Fatalf("warm %v %v", got, err)
			}
			files, _ := filepath.Glob(filepath.Join(dir, "*.spf-catalogue.json"))
			for _, file := range files {
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				var record catalogueRecord
				if err := json.Unmarshal(data, &record); err != nil {
					t.Fatal(err)
				}
				var page struct {
					Offset int `json:"offset"`
				}
				_ = json.Unmarshal(record.Body, &page)
				if page.Offset == 0 {
					if mode == "missing" {
						if err := os.Remove(file); err != nil {
							t.Fatal(err)
						}
					} else {
						record.Saved = time.Now().Add(-2 * time.Hour)
						data, _ = json.Marshal(record)
						if err := os.WriteFile(file, data, 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			changed = true
			c = newClient(s.Client(), base+"/")
			if err := c.EnableCatalogueCache(dir, time.Hour, "app", "user"); err != nil {
				t.Fatal(err)
			}
			err := app.Fill(context.Background(), c, config.Playlist{ID: "p", Artists: []config.Artist{{ID: "a"}}})
			if err != nil || deleted != "" || writes != 1 {
				t.Fatalf("unsafe mixed generation: err=%v first_calls=%d tail_calls=%d writes=%d deleted=%s", err, firstCalls, tailCalls, writes, deleted)
			}
			if firstCalls != 2 || tailCalls != 2 {
				t.Fatalf("replaced tail was not refetched: first=%d tail=%d", firstCalls, tailCalls)
			}
			// The refresh is complete in the same invocation; the next run is warm.
			// t0 remains remotely present and must never be deleted.
			if err := app.Fill(context.Background(), c, config.Playlist{ID: "p", Artists: []config.Artist{{ID: "a"}}}); err != nil {
				t.Fatal(err)
			}
			if deleted != "" || firstCalls != 2 || tailCalls != 2 || writes != 2 {
				t.Fatalf("unsafe/inefficient warm replacement: first=%d tail=%d writes=%d deleted=%s", firstCalls, tailCalls, writes, deleted)
			}
		})
	}
}

func TestIndependentRejectInvalidPaginationAggregate(t *testing.T) {
	for _, mode := range []string{"offset_gap", "changed_total", "changed_market", "missing_counts"} {
		t.Run(mode, func(t *testing.T) {
			var base string
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("offset") != "" {
					offset, total := 50, 51
					if mode == "offset_gap" {
						offset, total = 100, 101
					}
					if mode == "changed_total" {
						total = 101
						offset = 100
					}
					if mode == "missing_counts" {
						fmt.Fprint(w, `{"items":[{"id":"tail"}],"next":null}`)
						return
					}
					fmt.Fprintf(w, `{"items":[{"id":"tail"}],"next":null,"offset":%d,"limit":50,"total":%d}`, offset, total)
					return
				}
				items := make([]map[string]string, 50)
				for i := range items {
					items[i] = map[string]string{"id": fmt.Sprintf("t%d", i)}
				}
				nextOffset := 50
				if mode == "offset_gap" {
					nextOffset = 100
				}
				next := fmt.Sprintf("%s/albums/a/tracks?limit=50&offset=%d", base, nextOffset)
				if mode == "changed_market" {
					next += "&market=FR"
				}
				if mode == "missing_counts" {
					_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "next": next})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "next": next, "offset": 0, "limit": 50, "total": 51})
			}))
			defer s.Close()
			base = s.URL
			c := newClient(s.Client(), base+"/")
			if err := c.EnableCatalogueCache("", 0, "app", "user"); err != nil {
				t.Fatal(err)
			}
			got, err := c.AlbumTracks(context.Background(), "a")
			if err == nil || got != nil {
				t.Fatalf("unsafe %s accepted as complete: len=%d err=%v", mode, len(got), err)
			}
		})
	}
}
