package spotifyapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/zmb3/spotify/v2"
)

// Synthetic fixtures exercise the HTTP contract without contacting Spotify.
func playlistOperation(c *Client, ctx context.Context, method string) error {
	switch method {
	case http.MethodGet:
		_, err := c.PlaylistTracks(ctx, "p")
		return err
	case http.MethodPost:
		return c.AddTracks(ctx, "p", "a")
	default:
		return c.RemoveTracks(ctx, "p", "a")
	}
}

func TestPlaylistHTTPFailuresOnlyRetryRateLimitedReads(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusAccepted} {
			t.Run(fmt.Sprintf("%s/%d", method, status), func(t *testing.T) {
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.URL.Path != "/playlists/p/items" || r.Method != method {
						t.Errorf("request=%s %s", r.Method, r.URL)
					}
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(status)
					fmt.Fprintf(w, `{"error":{"status":%d,"message":"fixture denied"}}`, status)
				}))
				defer server.Close()
				c := newClient(server.Client(), server.URL+"/")
				err := playlistOperation(c, context.Background(), method)
				expectedCalls := 1
				if method == http.MethodGet && status == http.StatusTooManyRequests {
					expectedCalls = 3 // Initial GET plus two bounded retries; writes never replay.
				}
				var apiError spotify.Error
				if !errors.As(err, &apiError) || apiError.Status != status || apiError.Message != "fixture denied" || calls != expectedCalls {
					t.Fatalf("err=%v requests=%d", err, calls)
				}
			})
		}
	}
}

func TestPlaylistReadFailureDiscardsPartialResults(t *testing.T) {
	for _, page := range []int{1, 2} {
		for _, body := range []string{`{"error":{"status":403,"message":"denied"}}`, `not-json`} {
			t.Run(fmt.Sprintf("page%d/%s", page, body), func(t *testing.T) {
				calls := 0
				var base string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.URL.Path != "/playlists/p/items" {
						t.Errorf("path=%s", r.URL.Path)
					}
					if calls < page {
						fmt.Fprintf(w, `{"next":%q,"items":[{"item":{"type":"track","id":"partial"}}]}`, base+"/playlists/p/items?offset=17&limit=3")
						return
					}
					if page == 2 && (r.URL.Query().Get("offset") != "17" || r.URL.Query().Get("limit") != "3") {
						t.Errorf("next URL not followed verbatim: %s", r.URL)
					}
					if body != "not-json" {
						w.WriteHeader(http.StatusForbidden)
					}
					fmt.Fprint(w, body)
				}))
				defer server.Close()
				base = server.URL
				c := newClient(server.Client(), base+"/")
				tracks, err := c.PlaylistTracks(context.Background(), "p")
				if err == nil || tracks != nil || calls != page {
					t.Fatalf("tracks=%v err=%v calls=%d", tracks, err, calls)
				}
			})
		}
	}
}

func TestPlaylistWritesAccept100Items(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			ids := make([]spotify.ID, 100)
			expected := make([]string, 100)
			for i := range ids {
				ids[i] = spotify.ID(fmt.Sprint(i))
				expected[i] = "spotify:track:" + string(ids[i])
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body struct {
					URIs  []string `json:"uris"`
					Items []struct {
						URI string `json:"uri"`
					} `json:"items"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				uris := body.URIs
				if method == http.MethodDelete {
					uris = nil
					for _, item := range body.Items {
						uris = append(uris, item.URI)
					}
				}
				if !reflect.DeepEqual(uris, expected) {
					t.Errorf("URIs=%v", uris)
				}
				if method == http.MethodPost {
					w.WriteHeader(http.StatusCreated)
				}
				fmt.Fprint(w, `{"snapshot_id":"fixture"}`)
			}))
			defer server.Close()
			c := newClient(server.Client(), server.URL+"/")
			var err error
			if method == http.MethodPost {
				err = c.AddTracks(context.Background(), "p", ids...)
			} else {
				err = c.RemoveTracks(context.Background(), "p", ids...)
			}
			if err != nil || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestPlaylistMutationsRejectInvalidSuccessJSON(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if method == http.MethodPost {
					w.WriteHeader(http.StatusCreated)
				}
				fmt.Fprint(w, `invalid-json`)
			}))
			defer server.Close()
			err := playlistOperation(newClient(server.Client(), server.URL+"/"), context.Background(), method)
			if err == nil || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestPlaylistCancellation(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; t.Error("canceled request reached server") }))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := playlistOperation(newClient(server.Client(), server.URL+"/"), ctx, method)
			if !errors.Is(err, context.Canceled) || calls != 0 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestPlaylistSecondPageCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	secondPage := make(chan struct{})
	var base string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") == "1" {
			close(secondPage)
			<-r.Context().Done()
			return
		}
		fmt.Fprintf(w, `{"next":%q,"items":[{"item":{"type":"track","id":"partial"}}]}`, base+"/playlists/p/items?offset=1")
	}))
	defer server.Close()
	base = server.URL
	c := newClient(server.Client(), base+"/")
	type outcome struct {
		tracks []spotify.ID
		err    error
	}
	done := make(chan outcome, 1)
	go func() { tracks, err := c.PlaylistTracks(ctx, "p"); done <- outcome{tracks, err} }()
	select {
	case <-secondPage:
		cancel()
	case <-ctx.Done():
		t.Fatal("second page not requested")
	}
	result := <-done
	if !errors.Is(result.err, context.Canceled) || result.tracks != nil {
		t.Fatalf("tracks=%v err=%v", result.tracks, result.err)
	}
}

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNewSharesAuthenticatedHTTPClient(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer synthetic-fixture" {
			t.Error("authenticated transport not shared")
		}
		switch r.URL.Path {
		case "/v1/playlists/p/items":
			if r.Method == http.MethodGet {
				fmt.Fprint(w, `{"items":[],"next":null}`)
				return
			}
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusCreated)
			}
			fmt.Fprint(w, `{"snapshot_id":"fixture"}`)
		case "/v1/artists/a/albums":
			fmt.Fprint(w, `{"items":[],"next":null}`)
		default:
			t.Errorf("unexpected request: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	transport := server.Client().Transport
	httpClient := &http.Client{Timeout: time.Second, Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.spotify.com" {
			t.Errorf("production endpoint=%s", r.URL)
		}
		r = r.Clone(r.Context())
		r.URL.Scheme = target.Scheme
		r.URL.Host = target.Host
		r.Header.Set("Authorization", "Bearer synthetic-fixture")
		return transport.RoundTrip(r)
	})}
	c := New(httpClient)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		if err := playlistOperation(c, context.Background(), method); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.ArtistAlbums(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if c.http == httpClient || c.http.Timeout != time.Second || len(requests) != 4 {
		t.Fatalf("shared client or requests: %v", requests)
	}
}

func TestPlaylistHonorsHTTPTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain mutation bodies so net/http can detect the disconnected client.
		io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer server.Close()
	httpClient := server.Client()
	httpClient.Timeout = 20 * time.Millisecond
	c := newClient(httpClient, server.URL+"/")
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		err := playlistOperation(c, context.Background(), method)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s err=%v", method, err)
		}
	}
}

func TestPlaylistMutationsRejectOversizedBatch(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if method == http.MethodPost {
					w.WriteHeader(http.StatusCreated)
				}
				fmt.Fprint(w, `{"snapshot_id":"fixture"}`)
			}))
			defer server.Close()
			c := newClient(server.Client(), server.URL+"/")
			ids := make([]spotify.ID, 101)
			for i := range ids {
				ids[i] = spotify.ID(fmt.Sprint(i))
			}
			var err error
			if method == http.MethodPost {
				err = c.AddTracks(context.Background(), "p", ids...)
			} else {
				err = c.RemoveTracks(context.Background(), "p", ids...)
			}
			if err == nil || calls != 0 {
				t.Fatalf("oversized batch: err=%v requests=%d", err, calls)
			}
		})
	}
}
