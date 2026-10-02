package spotifyapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/app"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
)

func TestPlaylistPaginationCyclesFailBeforeRepeat(t *testing.T) {
	for _, mode := range []string{"self-absolute", "self-relative", "two-page", "equivalent-query", "fill"} {
		t.Run(mode, func(t *testing.T) {
			var calls, mutations atomic.Int32
			var base string
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					mutations.Add(1)
					fmt.Fprint(w, `{"snapshot_id":"fixture"}`)
					return
				}
				calls.Add(1)
				next := base + "/playlists/p/items"
				switch mode {
				case "self-relative":
					next = "items"
				case "two-page":
					if r.URL.RawQuery == "" {
						next = "?offset=1"
					}
				case "equivalent-query":
					next = "?offset=1&limit=3&opaque=a%2Bb"
					if r.URL.RawQuery != "" {
						next = "?opaque=a%2bb&limit=3&offset=%31"
					}
				}
				fmt.Fprintf(w, `{"items":[{"item":{"type":"track","id":"partial"}}],"next":%q}`, next)
			}))
			defer s.Close()
			base = s.URL
			// Safety net for RED only: correct code must stop on a cycle, not a deadline.
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			c := newClient(s.Client(), base+"/")
			var err error
			if mode == "fill" {
				err = app.Fill(ctx, c, config.Playlist{ID: "p", ShuffleOrder: true})
			} else {
				tracks, e := c.PlaylistTracks(ctx, "p")
				err = e
				if tracks != nil {
					t.Errorf("partial results=%v", tracks)
				}
			}
			expected := int32(1)
			if mode == "two-page" || mode == "equivalent-query" {
				expected = 2
			}
			t.Logf("requests=%d mutations=%d err=%v", calls.Load(), mutations.Load(), err)
			if err == nil || errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "cycle") || calls.Load() != expected || mutations.Load() != 0 {
				t.Fatalf("cycle not rejected before repeat: requests=%d mutations=%d err=%v", calls.Load(), mutations.Load(), err)
			}
		})
	}
}
