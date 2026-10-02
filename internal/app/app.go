// Package app computes and applies playlist changes independently of the Spotify transport.
package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/logging"
	"github.com/zmb3/spotify/v2"
)

// PlaylistCapacity is separate from the API's 100-item mutation batch size.
const PlaylistCapacity = 10000

// ErrPlaylistCapacity identifies a preflight skip; the playlist was not modified.
var ErrPlaylistCapacity = errors.New("playlist capacity exceeded; playlist unchanged")

type Album struct{ ID spotify.ID }

// Catalog is the subset of Spotify used by the filler. Reads must return all pages
// and support concurrent calls (or callers must select read concurrency 1).
type Catalog interface {
	PlaylistTracks(context.Context, spotify.ID) ([]spotify.ID, error)
	SearchArtist(context.Context, string) (spotify.ID, error)
	ArtistAlbums(context.Context, spotify.ID) ([]Album, error)
	AlbumTracks(context.Context, spotify.ID) ([]spotify.ID, error)
	AddTracks(context.Context, spotify.ID, ...spotify.ID) error
	RemoveTracks(context.Context, spotify.ID, ...spotify.ID) error
}

// Fill gathers the entire desired track list before making any destructive change.
// Artist exclusions apply across the playlist, matching the original YAML behavior.
func Fill(ctx context.Context, client Catalog, playlist config.Playlist) error {
	return FillWithConcurrency(ctx, client, playlist, 3)
}

// FillWithConcurrency bounds catalogue workers; playlist mutations stay sequential.
func FillWithConcurrency(ctx context.Context, client Catalog, playlist config.Playlist, concurrency int) error {
	if concurrency < 1 || concurrency > 8 {
		return fmt.Errorf("read concurrency must be from 1 to 8")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	logger := logging.FromContext(ctx)
	logger.Info("playlist read started")
	id := spotify.ID(playlist.ID)
	var current []spotify.ID
	var err error
	total, totalKnown := 0, false
	// Optional richer read keeps legacy Catalog implementations source-compatible.
	// Counts come from this read, not mutable adapter state or an extra API request.
	if counted, ok := client.(interface {
		PlaylistTracksWithCount(context.Context, spotify.ID) ([]spotify.ID, int, error)
	}); ok {
		current, total, err = counted.PlaylistTracksWithCount(ctx, id)
		totalKnown = true
	} else {
		current, err = client.PlaylistTracks(ctx, id)
	}
	if err != nil {
		return fmt.Errorf("read playlist %q: %w", playlist.Name, err)
	}
	readFields := []any{"supported_tracks", len(current), "total_known", totalKnown}
	if totalKnown {
		readFields = append(readFields, "total_items", total)
	}
	logger.Info("playlist read complete", readFields...)
	excluded := make(map[spotify.ID]bool)
	for _, album := range playlist.SkippedAlbums {
		excluded[spotify.ID(album.ID)] = true
	}
	for _, artist := range playlist.Artists {
		for _, album := range artist.SkippedAlbums {
			excluded[spotify.ID(album.ID)] = true
		}
	}
	desired, err := gatherCatalogue(ctx, client, playlist, excluded, concurrency)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	remove, add := planTracks(current, desired, playlist.ShuffleOrder)
	removed := make(map[spotify.ID]bool, len(remove))
	for _, track := range remove {
		removed[track] = true
	}
	projectedSupported := len(add)
	for _, track := range current {
		if !removed[track] {
			projectedSupported++
		}
	}
	desiredUnique := len(uniqueTracks(desired))
	if totalKnown && total < len(current) {
		return fmt.Errorf("playlist item count is smaller than supported track count; playlist unchanged")
	}
	projected := projectedSupported
	capacityFields := []any{"desired_unique", desiredUnique, "projected_supported", projectedSupported, "total_known", totalKnown, "limit", PlaylistCapacity}
	if totalKnown {
		projected += total - len(current) // Unsupported items remain untouched, even in shuffle mode.
		capacityFields = append(capacityFields, "projected_total", projected, "current_total", total)
	}
	if desiredUnique > PlaylistCapacity || projected > PlaylistCapacity {
		logger.Warn("playlist capacity exceeded; skipping unchanged", capacityFields...)
		return ErrPlaylistCapacity
	}
	logger.Debug("track plan", "current", len(current), "desired", len(desired), "remove", len(remove), "add", len(add))
	for _, batch := range trackBatches(remove) {
		if err := ctx.Err(); err != nil {
			return err
		}
		logger.Debug("remove batch", "tracks", len(batch))
		if err := client.RemoveTracks(ctx, id, batch...); err != nil {
			return fmt.Errorf("remove playlist tracks (playlist may be partially updated): %w", err)
		}
	}
	for _, batch := range trackBatches(add) {
		if err := ctx.Err(); err != nil {
			return err
		}
		logger.Debug("add batch", "tracks", len(batch))
		if err := client.AddTracks(ctx, id, batch...); err != nil {
			return fmt.Errorf("add playlist tracks (playlist may be partially updated): %w", err)
		}
	}
	return nil
}
