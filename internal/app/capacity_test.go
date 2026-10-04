package app

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/logging"
	"github.com/zmb3/spotify/v2"
)

func capacityTracks(n int) []spotify.ID {
	ids := make([]spotify.ID, n)
	for i := range ids {
		ids[i] = spotify.ID(fmt.Sprintf("track%d", i))
	}
	return ids
}

type countedCatalog struct {
	*fakeCatalog
	total int
}

func (f *countedCatalog) PlaylistTracksWithCount(context.Context, spotify.ID) ([]spotify.ID, int, error) {
	return f.current, f.total, nil
}

func TestFillCapacityIncludesUnsupported(t *testing.T) {
	for _, shuffle := range []bool{false, true} {
		t.Run(fmt.Sprint(shuffle), func(t *testing.T) {
			f := &countedCatalog{fakeCatalog: &fakeCatalog{current: []spotify.ID{"obsolete"}, albums: []Album{{ID: "album"}}, tracks: map[spotify.ID][]spotify.ID{"album": capacityTracks(10000)}}, total: 2}
			var logs bytes.Buffer
			ctx := logging.WithContext(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))
			err := Fill(ctx, f, config.Playlist{ID: "p", ShuffleOrder: shuffle, Artists: []config.Artist{{ID: "a"}}})
			if err == nil || len(f.writes)+len(f.removals) != 0 {
				t.Fatalf("unsupported overflow err=%v adds=%d removes=%d", err, len(f.writes), len(f.removals))
			}
			if !strings.Contains(logs.String(), "projected_total=10001") {
				t.Fatalf("missing raw projection: %s", &logs)
			}
		})
	}
}

func TestFillCapacityDuplicates(t *testing.T) {
	for _, shuffle := range []bool{false, true} {
		desired := capacityTracks(9999)
		desired = append(desired, desired...)
		f := &fakeCatalog{current: []spotify.ID{"track0", "track0", "track0"}, albums: []Album{{ID: "album"}}, tracks: map[spotify.ID][]spotify.ID{"album": desired}}
		err := Fill(context.Background(), f, config.Playlist{ID: "p", ShuffleOrder: shuffle, Artists: []config.Artist{{ID: "a"}}})
		if shuffle {
			if err != nil || len(f.writes) == 0 {
				t.Fatalf("desired duplicates must not overflow: %v", err)
			}
		} else if err == nil || len(f.writes)+len(f.removals) != 0 {
			t.Fatalf("retained duplicates overflow: %v", err)
		}
	}
	f := &fakeCatalog{albums: []Album{{ID: "album"}}, tracks: map[spotify.ID][]spotify.ID{"album": append(capacityTracks(10000), capacityTracks(10000)...)}}
	if err := Fill(context.Background(), f, config.Playlist{ID: "p", Artists: []config.Artist{{ID: "a"}}}); err != nil {
		t.Fatalf("exactly 10000 unique allowed: %v", err)
	}
}

func TestFillCapacityBoundaries(t *testing.T) {
	for _, n := range []int{9999, 10000, 10001} {
		for _, shuffle := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/shuffle=%t", n, shuffle), func(t *testing.T) {
				f := &fakeCatalog{current: []spotify.ID{"obsolete"}, albums: []Album{{ID: "album"}}, tracks: map[spotify.ID][]spotify.ID{"album": capacityTracks(n)}}
				var logs bytes.Buffer
				ctx := logging.WithContext(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))
				err := Fill(ctx, f, config.Playlist{ID: "p", ShuffleOrder: shuffle, Artists: []config.Artist{{ID: "a"}}})
				if n > 10000 {
					if err == nil || len(f.writes)+len(f.removals) != 0 {
						t.Fatalf("overflow err=%v writes=%d removals=%d", err, len(f.writes), len(f.removals))
					}
					for _, field := range []string{"level=WARN", "desired_unique=10001", "projected_supported=10001", "limit=10000"} {
						if !strings.Contains(logs.String(), field) {
							t.Errorf("missing %s in %s", field, &logs)
						}
					}
				} else if err != nil || len(f.writes) == 0 {
					t.Fatalf("allowed err=%v writes=%d", err, len(f.writes))
				}
			})
		}
	}
}
