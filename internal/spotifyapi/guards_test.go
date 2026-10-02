package spotifyapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// Only static synthetic fixture credentials are used; no Spotify API is called.
func TestOAuthDestinationsAreConfined(t *testing.T) {
	for _, mode := range []string{"next", "redirect", "catalog-next", "catalog-redirect", "user-redirect"} {
		for _, destination := range []string{"cross-origin", "wrong-path", "other-playlist", "userinfo"} {
			t.Run(mode+"/"+destination, func(t *testing.T) {
				var received atomic.Int32
				sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					received.Add(1)
					t.Errorf("hostile destination received request (Authorization present=%t)", r.Header.Get("Authorization") != "")
					fmt.Fprint(w, `{"items":[],"next":null}`)
				}))
				defer sink.Close()
				var base string
				var unintended atomic.Int32
				origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					expected := "/playlists/p/items"
					if strings.HasPrefix(mode, "catalog") {
						expected = "/artists/a/albums"
					}
					if mode == "user-redirect" {
						expected = "/me"
					}
					if r.URL.Path != expected || r.URL.Query().Get("hostile") == "1" {
						unintended.Add(1)
						fmt.Fprint(w, `{"items":[],"next":null}`)
						return
					}
					if r.Header.Get("Authorization") != "Bearer synthetic-guard-fixture" {
						t.Error("missing fixture OAuth bearer")
					}
					target := sink.URL + "/playlists/p/items"
					switch destination {
					case "wrong-path":
						target = base + "/capture?hostile=1"
					case "other-playlist":
						target = base + "/playlists/other/items?hostile=1"
					case "userinfo":
						u, _ := url.Parse(base + expected + "?hostile=1")
						u.User = url.User("fixture")
						target = u.String()
					}
					// Catalogue routes need only an API-origin boundary, not playlist scope.
					if strings.HasPrefix(mode, "catalog") || mode == "user-redirect" {
						if destination == "wrong-path" || destination == "other-playlist" {
							target = sink.URL + "/capture"
						}
					}
					if strings.Contains(mode, "redirect") {
						http.Redirect(w, r, target, http.StatusFound)
						return
					}
					fmt.Fprintf(w, `{"items":[{"item":{"type":"track","id":"partial"}}],"next":%q}`, target)
				}))
				defer origin.Close()
				base = origin.URL
				hc := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "synthetic-guard-fixture"}))
				hc.Timeout = time.Second
				c := newClient(hc, base+"/")
				var err error
				switch {
				case strings.HasPrefix(mode, "catalog"):
					_, err = c.ArtistAlbums(context.Background(), "a")
				case mode == "user-redirect":
					_, err = c.CurrentUser(context.Background())
				default:
					tracks, e := c.PlaylistTracks(context.Background(), "p")
					err = e
					if tracks != nil {
						t.Errorf("partial tracks=%v", tracks)
					}
				}
				if err == nil || received.Load() != 0 || unintended.Load() != 0 {
					t.Fatalf("err=%v sink requests=%d unintended=%d", err, received.Load(), unintended.Load())
				}
			})
		}
	}
}

func TestProductionOriginGuardBeforeOAuth(t *testing.T) {
	var calls atomic.Int32
	hc := &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, fmt.Errorf("unexpected transport request")
	})}
	c := New(hc)
	for _, endpoint := range []string{"http://api.spotify.com/v1/playlists/p/items", "https://api.spotify.com.evil.invalid/v1/playlists/p/items", "https://api.spotify.com:444/v1/playlists/p/items", "https://fixture@api.spotify.com/v1/playlists/p/items"} {
		req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
		if _, err := c.http.Do(req); err == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("OAuth-side transport called %d times", calls.Load())
	}
}

func TestSafeNextPreservesQueryAndRedirectPolicy(t *testing.T) {
	for _, next := range []string{"?limit=3&offset=17&opaque=a%2Bb", "/playlists/p/items?limit=3&offset=17&opaque=a%2Bb", "items?limit=3&offset=17&opaque=a%2Bb"} {
		t.Run(next, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 1 {
					fmt.Fprintf(w, `{"items":[],"next":%q}`, next)
					return
				}
				if r.URL.RawQuery != "limit=3&offset=17&opaque=a%2Bb" {
					t.Errorf("query rewritten: %s", r.URL.RawQuery)
				}
				fmt.Fprint(w, `{"items":[],"next":null}`)
			}))
			defer s.Close()
			if _, err := newClient(s.Client(), s.URL+"/").PlaylistTracks(context.Background(), "p"); err != nil || calls != 2 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
	for _, custom := range []bool{false, true} {
		t.Run(fmt.Sprint("redirect-policy/", custom), func(t *testing.T) {
			calls := 0
			policyCalls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", fmt.Sprintf("?n=%d", calls))
				w.WriteHeader(http.StatusFound)
			}))
			defer s.Close()
			hc := s.Client()
			if custom {
				hc.CheckRedirect = func(r *http.Request, via []*http.Request) error { policyCalls++; return http.ErrUseLastResponse }
			}
			c := newClient(hc, s.URL+"/")
			if c.http == hc || c.http.Transport == nil {
				t.Error("client must be cloned without mutating original")
			}
			if (hc.CheckRedirect != nil) != custom {
				t.Error("raw client policy changed")
			}
			if _, err := c.PlaylistTracks(context.Background(), "p"); err == nil {
				t.Error("redirect loop/status accepted")
			} else {
				t.Logf("redirect policy result: %v", err)
			}
			expected := 10
			if custom {
				expected = 1
			}
			if calls != expected || (custom && policyCalls != 1) {
				t.Fatalf("calls=%d policyCalls=%d", calls, policyCalls)
			}
		})
	}
}
