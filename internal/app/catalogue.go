package app

import (
	"context"
	"fmt"
	"sync"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/diagnostics"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/logging"
	"github.com/zmb3/spotify/v2"
)

type catalogueAlbum struct {
	id     spotify.ID
	tracks []spotify.ID
}
type albumRead struct {
	done   chan struct{}
	tracks []spotify.ID
	err    error
}
type albumReads struct {
	mu      sync.Mutex
	entries map[spotify.ID]*albumRead
}

// Share an album read across overlapping artists without starting more workers.
func (r *albumReads) get(ctx context.Context, client Catalog, id spotify.ID) ([]spotify.ID, error) {
	r.mu.Lock()
	entry, exists := r.entries[id]
	if !exists {
		entry = &albumRead{done: make(chan struct{})}
		r.entries[id] = entry
	}
	r.mu.Unlock()
	if exists {
		select {
		case <-entry.done:
			return entry.tracks, entry.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	entry.tracks, entry.err = client.AlbumTracks(ctx, id)
	close(entry.done)
	return entry.tracks, entry.err
}

func readArtist(ctx context.Context, client Catalog, playlist config.Playlist, index int, excluded map[spotify.ID]bool, cache *albumReads) ([]catalogueAlbum, error) {
	artist := playlist.Artists[index]
	logger := logging.FromContext(ctx).With("artist", index+1, "artists", len(playlist.Artists))
	ctx = logging.WithContext(ctx, logger)
	logger.Info("artist catalogue started")
	id := spotify.ID(artist.ID)
	if artist.UseNameInsteadOfURI {
		logger.Info("artist search started")
		var err error
		id, err = client.SearchArtist(ctx, artist.Name)
		if err != nil {
			return nil, diagnostics.Wrap(ctx, fmt.Errorf("search artist %q: %w", artist.Name, err), diagnostics.SearchArtist, index+1, true)
		}
		logger.Info("artist search complete", "found", id != "")
		if id == "" {
			return nil, diagnostics.Wrap(ctx, fmt.Errorf("artist %q not found; playlist unchanged", artist.Name), diagnostics.SearchArtist, index+1, true)
		}
	}
	logger.Info("artist albums read started")
	albums, err := client.ArtistAlbums(ctx, id)
	if err != nil {
		return nil, diagnostics.Wrap(ctx, fmt.Errorf("read artist %q albums: %w", artist.Name, err), diagnostics.ArtistAlbums, index+1, true)
	}
	logger.Info("artist albums read complete", "albums", len(albums))
	var result []catalogueAlbum
	seen := make(map[spotify.ID]bool)
	skipped, tracksRead := 0, 0
	for i, album := range albums {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if excluded[album.ID] || seen[album.ID] {
			skipped++
			continue
		}
		seen[album.ID] = true
		albumLogger := logger.With("album", i+1, "albums", len(albums))
		albumLogger.Info("album tracks read started")
		tracks, err := cache.get(logging.WithContext(ctx, albumLogger), client, album.ID)
		if err != nil {
			return nil, diagnostics.Wrap(ctx, fmt.Errorf("read album %q: %w", album.ID, err), diagnostics.AlbumTracks, index+1, true)
		}
		result = append(result, catalogueAlbum{id: album.ID, tracks: tracks})
		tracksRead += len(tracks)
		albumLogger.Info("album tracks read complete", "tracks", len(tracks))
	}
	logger.Info("artist catalogue complete", "albums", len(albums), "albums_read", len(result), "albums_skipped", skipped, "tracks", tracksRead)
	return result, nil
}

// Fixed workers each perform one read at a time. Results are merged by original
// configuration order, never completion order. Cancellation joins every worker
// before returning, and the caller cannot mutate until this function succeeds.
func gatherCatalogue(ctx context.Context, client Catalog, playlist config.Playlist, excluded map[spotify.ID]bool, concurrency int) ([]spotify.ID, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([][]catalogueAlbum, len(playlist.Artists))
	cache := &albumReads{entries: make(map[spotify.ID]*albumRead)}
	jobs := make(chan int)
	var wg sync.WaitGroup
	var firstError error
	var errorOnce sync.Once
	for i := 0; i < min(concurrency, len(playlist.Artists)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					return
				}
				result, err := readArtist(ctx, client, playlist, index, excluded, cache)
				if err != nil {
					errorOnce.Do(func() { firstError = err; cancel() })
					return
				}
				results[index] = result
			}
		}()
	}
dispatch:
	for index := range playlist.Artists {
		select {
		case jobs <- index:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	if firstError != nil {
		return nil, firstError
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var desired []spotify.ID
	seen := make(map[spotify.ID]bool)
	for _, albums := range results {
		for _, album := range albums {
			if !seen[album.id] {
				seen[album.id] = true
				desired = append(desired, album.tracks...)
			}
		}
	}
	return desired, nil
}
