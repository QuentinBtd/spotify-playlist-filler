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
func TestWholeEntityRootRefreshBeforeDelete(t *testing.T) {
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
			if err != nil || deleted != "" || firstCalls != 2 || tailCalls != 2 {
				t.Fatalf("unsafe mixed generation: err=%v first_calls=%d tail_calls=%d writes=%d deleted=%s", err, firstCalls, tailCalls, writes, deleted)
			}
			got, err := c.AlbumTracks(context.Background(), "album")
			if err != nil || len(got) != 51 || firstCalls != 2 || tailCalls != 2 {
				t.Fatalf("warm coherent aggregate: len=%d err=%v calls=%d/%d", len(got), err, firstCalls, tailCalls)
			}
			for i, id := range got {
				if string(id) != fmt.Sprintf("t%d", (i+1)%51) {
					t.Fatalf("fresh aggregate[%d]=%s", i, id)
				}
			}
		})
	}
}

func TestWholeEntityInvalidPaginationAggregate(t *testing.T) {
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

func TestWholeEntityTailRefreshBeforeDelete(t *testing.T) {
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
				if page.Offset == 50 {
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
			if err != nil || deleted != "" || firstCalls != 2 || tailCalls != 2 {
				t.Fatalf("unsafe mixed generation: err=%v first_calls=%d tail_calls=%d writes=%d deleted=%s", err, firstCalls, tailCalls, writes, deleted)
			}
			got, err := c.AlbumTracks(context.Background(), "album")
			if err != nil || len(got) != 51 || firstCalls != 2 || tailCalls != 2 {
				t.Fatalf("warm coherent aggregate: len=%d err=%v calls=%d/%d", len(got), err, firstCalls, tailCalls)
			}
			for i, id := range got {
				if string(id) != fmt.Sprintf("t%d", (i+1)%51) {
					t.Fatalf("fresh aggregate[%d]=%s", i, id)
				}
			}
		})
	}
}

func TestWholeEntityChangedTotalRefresh(t *testing.T) {
	for _, mode := range []string{"missing", "expired"} {
		t.Run(mode, func(t *testing.T) {
			total, rootCalls, tailCalls := 51, 0, 0
			var base string
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				offset := 0
				if r.URL.Query().Get("offset") == "50" {
					offset = 50
					tailCalls++
				} else {
					rootCalls++
				}
				_ = json.NewEncoder(w).Encode(chainPage(base, offset, total, false))
			}))
			defer s.Close()
			base = s.URL
			dir := filepath.Join(t.TempDir(), "cache")
			client := func() *Client {
				c := newClient(s.Client(), base+"/")
				if err := c.EnableCatalogueCache(dir, 24*time.Hour, "app", "user"); err != nil {
					t.Fatal(err)
				}
				return c
			}
			if got, err := client().AlbumTracks(context.Background(), "a"); err != nil || len(got) != 51 {
				t.Fatalf("warm: %v %v", got, err)
			}
			files, _ := filepath.Glob(filepath.Join(dir, "*.spf-catalogue.json"))
			for _, file := range files {
				data, _ := os.ReadFile(file)
				var rec catalogueRecord
				_ = json.Unmarshal(data, &rec)
				var p struct {
					Offset int `json:"offset"`
				}
				_ = json.Unmarshal(rec.Body, &p)
				if p.Offset == 50 {
					if mode == "missing" {
						if err := os.Remove(file); err != nil {
							t.Fatal(err)
						}
					} else {
						rec.Saved = time.Now().Add(-48 * time.Hour)
						data, _ = json.Marshal(rec)
						if err := os.WriteFile(file, data, 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			total = 52
			for attempt := 1; attempt <= 1; attempt++ {
				got, err := client().AlbumTracks(context.Background(), "a")
				t.Logf("attempt=%d len=%d err=%v root_calls=%d tail_calls=%d", attempt, len(got), err, rootCalls, tailCalls)
				if err == nil && len(got) == 52 && rootCalls == 2 && tailCalls == 2 {
					for i, id := range got {
						if string(id) != fmt.Sprintf("t%d", i) {
							t.Fatalf("fresh aggregate[%d]=%s", i, id)
						}
					}
					if warm, e := client().AlbumTracks(context.Background(), "a"); e != nil || len(warm) != 52 || rootCalls != 2 || tailCalls != 2 {
						t.Fatalf("warm: len=%d err=%v calls=%d/%d", len(warm), e, rootCalls, tailCalls)
					}
					return
				}
			}
			t.Fatalf("no checkpoint progress: root_calls=%d tail_calls=%d; cached root retains total=51, remote total=52", rootCalls, tailCalls)
		})
	}
}

func TestWholeEntityManyPageRefresh(t *testing.T) {
	var base string
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		offset := 0
		_, _ = fmt.Sscan(r.URL.Query().Get("offset"), &offset)
		_ = json.NewEncoder(w).Encode(chainPage(base, offset, 1001, false))
	}))
	defer s.Close()
	base = s.URL
	dir := filepath.Join(t.TempDir(), "cache")
	client := func() *Client {
		c := newClient(s.Client(), base+"/")
		if err := c.EnableCatalogueCache(dir, 24*time.Hour, "app", "user"); err != nil {
			t.Fatal(err)
		}
		return c
	}
	if got, err := client().AlbumTracks(context.Background(), "a"); err != nil || len(got) != 1001 {
		t.Fatalf("warm: len=%d err=%v", len(got), err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.spf-catalogue.json"))
	for _, file := range files {
		data, _ := os.ReadFile(file)
		var rec catalogueRecord
		_ = json.Unmarshal(data, &rec)
		var page struct {
			Offset int `json:"offset"`
		}
		_ = json.Unmarshal(rec.Body, &page)
		if page.Offset == 0 {
			rec.Saved = time.Now().Add(-48 * time.Hour)
			data, _ = json.Marshal(rec)
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	failures := 0
	for invocation := 1; invocation <= 1; invocation++ {
		got, err := client().AlbumTracks(context.Background(), "a")
		if err != nil {
			failures++
			continue
		}
		t.Logf("21-page unchanged entity: failed_invocations=%d success_invocation=%d total_gets=%d len=%d", failures, invocation, calls, len(got))
		if failures > 0 {
			t.Fatalf("ordinary root expiry requires %d manual failed relaunches", failures)
		}
		if len(got) != 1001 || calls != 42 {
			t.Fatalf("refresh len=%d calls=%d", len(got), calls)
		}
		if warm, e := client().AlbumTracks(context.Background(), "a"); e != nil || len(warm) != 1001 || calls != 42 {
			t.Fatalf("warm len=%d err=%v calls=%d", len(warm), e, calls)
		}
		return
	}
	t.Fatal("never resumed")
}
