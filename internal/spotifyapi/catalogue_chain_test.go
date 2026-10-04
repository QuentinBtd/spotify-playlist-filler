package spotifyapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func chainPage(base string, offset, total int, changed bool) map[string]any {
	count := 50
	if total-offset < count {
		count = total - offset
	}
	items := make([]map[string]string, count)
	for i := range items {
		n := offset + i
		if changed {
			n = (n + 1) % total
		}
		items[i] = map[string]string{"id": fmt.Sprintf("t%d", n)}
	}
	var next any
	if offset+count < total {
		next = fmt.Sprintf("%s/albums/a/tracks?limit=50&offset=%d", base, offset+count)
	}
	return map[string]any{"items": items, "next": next, "offset": offset, "limit": 50, "total": total}
}

func TestCataloguePaginationWithoutCacheSetup(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"items":[{"id":"unsafe"}],"next":null}`) }))
	defer s.Close()
	if got, err := newClient(s.Client(), s.URL+"/").AlbumTracks(context.Background(), "a"); err == nil || got != nil {
		t.Fatalf("missing counts accepted: %v %v", got, err)
	}
}

func TestCatalogueChainValidationVariants(t *testing.T) {
	for _, ttl := range []time.Duration{0, time.Hour} {
		for _, mode := range []string{"stable_total", "limit", "short_nonterminal", "null_offset", "fraction_total", "duplicate_query", "changed_include_groups", "changed_q", "changed_type"} {
			t.Run(fmt.Sprintf("%v/%s", ttl, mode), func(t *testing.T) {
				var base string
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
					page := chainPage(base, offset, 51, false)
					if offset == 0 {
						switch mode {
						case "limit":
							page["limit"] = 49
						case "short_nonterminal":
							page["items"] = []map[string]string{{"id": "one"}}
						case "null_offset":
							page["offset"] = nil
						case "fraction_total":
							page["total"] = 51.5
						case "duplicate_query":
							page["next"] = page["next"].(string) + "&offset=50"
						case "changed_include_groups":
							page["next"] = page["next"].(string) + "&include_groups=single"
						case "changed_q":
							page["next"] = page["next"].(string) + "&q=other"
						case "changed_type":
							page["next"] = page["next"].(string) + "&type=artist"
						}
					} else if mode == "stable_total" {
						page = chainPage(base, offset, 52, false)
					}
					_ = json.NewEncoder(w).Encode(page)
				}))
				defer s.Close()
				base = s.URL
				c := newClient(s.Client(), base+"/")
				if err := c.EnableCatalogueCache(filepath.Join(t.TempDir(), "cache"), ttl, "app", "user"); err != nil {
					t.Fatal(err)
				}
				if got, err := c.AlbumTracks(context.Background(), "a"); err == nil || got != nil {
					t.Fatalf("invalid chain: %v %v", got, err)
				}
			})
		}
	}
}

func TestCatalogueIncompleteEntityFullRefreshAfterQuota(t *testing.T) {
	for _, offsetToRemove := range []int{0, 50} {
		for _, mode := range []string{"missing", "expired", "corrupt", "invalid_page"} {
			t.Run(fmt.Sprintf("%d/%s", offsetToRemove, mode), func(t *testing.T) {
				var changed, fail atomic.Bool
				var failures atomic.Int32
				var calls [3]atomic.Int32
				var base string
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
					calls[offset/50].Add(1)
					if fail.Load() && offset == 100 {
						failures.Add(1)
						w.Header().Set("Retry-After", "80982")
						w.WriteHeader(429)
						fmt.Fprint(w, `{"error":{"status":429,"reason":"QUOTA_EXCEEDED"}}`)
						return
					}
					_ = json.NewEncoder(w).Encode(chainPage(base, offset, 101, changed.Load()))
				}))
				defer s.Close()
				base = s.URL
				dir := filepath.Join(t.TempDir(), "cache")
				client := func() *Client {
					c := newClient(s.Client(), base+"/")
					if err := c.EnableCatalogueCache(dir, time.Hour, "app", "user"); err != nil {
						t.Fatal(err)
					}
					return c
				}
				if got, err := client().AlbumTracks(context.Background(), "a"); err != nil || len(got) != 101 {
					t.Fatalf("warm %v %v", got, err)
				}
				files, _ := filepath.Glob(filepath.Join(dir, "*.spf-catalogue.json"))
				for _, file := range files {
					data, err := os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					var rec catalogueRecord
					if err := json.Unmarshal(data, &rec); err != nil {
						t.Fatal(err)
					}
					var page struct {
						Offset int `json:"offset"`
					}
					_ = json.Unmarshal(rec.Body, &page)
					if page.Offset == offsetToRemove {
						switch mode {
						case "missing":
							if err := os.Remove(file); err != nil {
								t.Fatal(err)
							}
						case "expired":
							rec.Saved = time.Now().Add(-2 * time.Hour)
							data, _ = json.Marshal(rec)
							if err := os.WriteFile(file, data, 0600); err != nil {
								t.Fatal(err)
							}
						case "invalid_page":
							rec.Body = []byte(`{"items":[],"next":null}`)
							data, _ = json.Marshal(rec)
							if err := os.WriteFile(file, data, 0600); err != nil {
								t.Fatal(err)
							}
						case "corrupt":
							if err := os.WriteFile(file, []byte("{"), 0600); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
				changed.Store(true)
				fail.Store(true)
				// Any cache miss refreshes the whole entity and reaches the genuine failed tail.
				if got, err := client().AlbumTracks(context.Background(), "a"); err == nil || got != nil {
					t.Fatalf("partial usable %v %v", got, err)
				}
				for attempts := 0; failures.Load() == 0 && attempts < 2; attempts++ {
					if got, err := client().AlbumTracks(context.Background(), "a"); err == nil || got != nil {
						t.Fatalf("quota partial usable %v %v", got, err)
					}
				}
				if failures.Load() != 1 {
					t.Fatalf("expected one quota attempt, got %d", failures.Load())
				}
				store := &catalogueStore{dir: dir, namespace: "app\x00user\x00" + base + "/\x00sdk-default-market-v1", ttl: time.Hour}
				deadline, _ := json.Marshal(quotaMetadata{Seconds: 80982, At: time.Now().Add(-time.Second), Reason: "QUOTA_EXCEEDED"})
				if err := store.save(store.cooldownKey(), deadline); err != nil {
					t.Fatal(err)
				}
				fail.Store(false)
				got, err := client().AlbumTracks(context.Background(), "a")
				if err != nil || len(got) != 101 {
					t.Fatalf("single invocation full refresh: len=%d err=%v", len(got), err)
				}
				for i, id := range got {
					if string(id) != fmt.Sprintf("t%d", (i+1)%101) {
						t.Fatalf("fresh aggregate[%d]=%s", i, id)
					}
				}
				before := [3]int32{calls[0].Load(), calls[1].Load(), calls[2].Load()}
				if before[2] < 2 {
					t.Fatal("old unvisited tail resurrected")
				}
				if before != [3]int32{3, 3, 3} {
					t.Fatalf("incomplete entity must fully refetch: %v", before)
				}
				if _, err := client().AlbumTracks(context.Background(), "a"); err != nil {
					t.Fatal(err)
				}
				after := [3]int32{calls[0].Load(), calls[1].Load(), calls[2].Load()}
				if before != after {
					t.Fatalf("warm complete fetched: %v -> %v", before, after)
				}
			})
		}
	}
}

func TestCatalogueConcurrentSameEntityChain(t *testing.T) {
	var base string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		_ = json.NewEncoder(w).Encode(chainPage(base, offset, 51, false))
	}))
	defer s.Close()
	base = s.URL
	dir := filepath.Join(t.TempDir(), "cache")
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := newClient(s.Client(), base+"/")
			if err := c.EnableCatalogueCache(dir, time.Hour, "app", "user"); err != nil {
				t.Error(err)
				return
			}
			got, err := c.AlbumTracks(context.Background(), "a")
			if err == nil && len(got) != 51 {
				t.Errorf("usable partial %v", got)
			}
		}()
	}
	wg.Wait()
	c := newClient(s.Client(), base+"/")
	if err := c.EnableCatalogueCache(dir, time.Hour, "app", "user"); err != nil {
		t.Fatal(err)
	}
	for tries := 0; tries < 2; tries++ {
		if got, err := c.AlbumTracks(context.Background(), "a"); err == nil && len(got) == 51 {
			return
		}
	}
	t.Fatal("concurrent checkpoints could not resume")
}

func TestCatalogueSearchValidatesConsumedPagesBeforeExactMatch(t *testing.T) {
	for _, mode := range []string{"early_match", "changed_total", "missing_counts", "changed_query"} {
		t.Run(mode, func(t *testing.T) {
			var base string
			var calls int
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
				total, count := 11, 10
				if offset == 10 {
					count = 1
					if mode == "changed_total" {
						total = 12
						count = 2
					}
				}
				items := make([]map[string]string, count)
				for i := range items {
					items[i] = map[string]string{"id": "other", "name": "Other"}
				}
				if offset == 10 || mode == "early_match" || mode == "changed_query" || mode == "missing_counts" {
					items[0] = map[string]string{"id": "exact", "name": "Exact"}
				}
				var next any
				if offset == 0 {
					q := r.URL.Query()
					q.Set("offset", "10")
					if mode == "changed_query" {
						q.Set("q", "Changed")
					}
					next = base + "/search?" + q.Encode()
				}
				page := map[string]any{"items": items, "next": next, "offset": offset, "limit": 10, "total": total}
				if mode == "missing_counts" {
					delete(page, "total")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"artists": page})
			}))
			defer s.Close()
			base = s.URL
			c := newClient(s.Client(), base+"/")
			id, err := c.SearchArtist(context.Background(), "Exact")
			if mode == "early_match" {
				if err != nil || id != "exact" || calls != 1 {
					t.Fatalf("early exact match: id=%s err=%v calls=%d", id, err, calls)
				}
			} else if err == nil || id != "" {
				t.Fatalf("invalid search consumed: id=%s err=%v", id, err)
			}
		})
	}
}

func TestCatalogueCachedTailCannotBypassStableTotal(t *testing.T) {
	var base string
	var fail atomic.Bool
	var calls [2]atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		calls[offset/50].Add(1)
		if fail.Load() && offset == 50 {
			w.WriteHeader(503)
			return
		}
		_ = json.NewEncoder(w).Encode(chainPage(base, offset, 51, false))
	}))
	defer s.Close()
	base = s.URL
	dir := filepath.Join(t.TempDir(), "cache")
	c := newClient(s.Client(), base+"/")
	if err := c.EnableCatalogueCache(dir, time.Hour, "app", "user"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AlbumTracks(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.spf-catalogue.json"))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var rec catalogueRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			t.Fatal(err)
		}
		var page struct {
			Offset int `json:"offset"`
		}
		_ = json.Unmarshal(rec.Body, &page)
		if page.Offset == 50 {
			rec.Body, _ = json.Marshal(chainPage(base, 50, 52, false))
			data, _ = json.Marshal(rec)
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	fail.Store(true)
	if got, err := c.AlbumTracks(context.Background(), "a"); err == nil || got != nil {
		t.Fatalf("cached aggregate accepted %v %v", got, err)
	}
	if calls[0].Load() != 2 || calls[1].Load() != 2 {
		t.Fatal("invalid entity not fully refetched")
	}
}
