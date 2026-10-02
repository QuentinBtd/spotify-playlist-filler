// Package app computes and applies playlist changes independently of the Spotify transport.
package app

import (
	"context"
	"fmt"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/logging"
	"github.com/zmb3/spotify/v2"
)

type Album struct{ ID spotify.ID }

// Catalog is the subset of Spotify used by the filler. Reads must return all pages.
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
	if err := ctx.Err(); err != nil {
		return err
	}
	logger := logging.FromContext(ctx)
	logger.Info("read catalogue", "artists", len(playlist.Artists))
	id := spotify.ID(playlist.ID)
	current, err := client.PlaylistTracks(ctx, id)
	if err != nil {
		return fmt.Errorf("read playlist %q: %w", playlist.Name, err)
	}
	excluded := make(map[spotify.ID]bool)
	for _, album := range playlist.SkippedAlbums {
		excluded[spotify.ID(album.ID)] = true
	}
	for _, artist := range playlist.Artists {
		for _, album := range artist.SkippedAlbums {
			excluded[spotify.ID(album.ID)] = true
		}
	}
	var desired []spotify.ID
	seenAlbums := make(map[spotify.ID]bool)
	for _, artist := range playlist.Artists {
		artistID := spotify.ID(artist.ID)
		if artist.UseNameInsteadOfURI {
			artistID, err = client.SearchArtist(ctx, artist.Name)
			if err != nil {
				return fmt.Errorf("search artist %q: %w", artist.Name, err)
			}
			if artistID == "" {
				return fmt.Errorf("artist %q not found; playlist unchanged", artist.Name)
			}
		}
		albums, err := client.ArtistAlbums(ctx, artistID)
		if err != nil {
			return fmt.Errorf("read artist %q albums: %w", artist.Name, err)
		}
		for _, album := range albums {
			if excluded[album.ID] || seenAlbums[album.ID] {
				continue
			}
			seenAlbums[album.ID] = true
			tracks, err := client.AlbumTracks(ctx, album.ID)
			if err != nil {
				return fmt.Errorf("read album %q: %w", album.ID, err)
			}
			desired = append(desired, tracks...)
		}
	}
	remove, add := planTracks(current, desired, playlist.ShuffleOrder)
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
