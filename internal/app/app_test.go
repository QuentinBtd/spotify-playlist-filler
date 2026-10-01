package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
	"github.com/zmb3/spotify/v2"
)

type fakeCatalog struct {
	current       []spotify.ID
	albums        []Album
	tracks        map[spotify.ID][]spotify.ID
	albumErr      error
	writes        [][]spotify.ID
	removals      [][]spotify.ID
	reads         []spotify.ID
	writeErr      error
	searched      string
	onRead        func()
	missingArtist bool
}

func (f *fakeCatalog) PlaylistTracks(context.Context, spotify.ID) ([]spotify.ID, error) {
	return f.current, nil
}
func (f *fakeCatalog) ArtistAlbums(context.Context, spotify.ID) ([]Album, error) {
	return f.albums, f.albumErr
}
func (f *fakeCatalog) AlbumTracks(_ context.Context, id spotify.ID) ([]spotify.ID, error) {
	f.reads = append(f.reads, id)
	if f.onRead != nil {
		f.onRead()
	}
	return f.tracks[id], nil
}
func (f *fakeCatalog) SearchArtist(_ context.Context, name string) (spotify.ID, error) {
	f.searched = name
	if f.missingArtist {
		return "", nil
	}
	return "artist", nil
}
func (f *fakeCatalog) AddTracks(_ context.Context, _ spotify.ID, ids ...spotify.ID) error {
	f.writes = append(f.writes, ids)
	return f.writeErr
}
func (f *fakeCatalog) RemoveTracks(_ context.Context, _ spotify.ID, ids ...spotify.ID) error {
	f.removals = append(f.removals, ids)
	return f.writeErr
}

func TestFillExcludesPlaylistAndArtistAlbums(t *testing.T) {
	f := &fakeCatalog{current: []spotify.ID{"old", "keep"}, albums: []Album{{ID: "skip-playlist"}, {ID: "skip-artist"}, {ID: "ok"}}, tracks: map[spotify.ID][]spotify.ID{"ok": {"keep", "new", "new"}}}
	p := config.Playlist{ID: "playlist", Artists: []config.Artist{{Name: "Artist", UseNameInsteadOfURI: true, SkippedAlbums: []config.Album{{ID: "skip-artist"}}}}, SkippedAlbums: []config.Album{{ID: "skip-playlist"}}}
	if err := Fill(context.Background(), f, p); err != nil {
		t.Fatal(err)
	}
	if f.searched != "Artist" || !reflect.DeepEqual(f.reads, []spotify.ID{"ok"}) {
		t.Fatalf("search=%q reads=%v", f.searched, f.reads)
	}
	if !reflect.DeepEqual(f.writes, [][]spotify.ID{{"new"}}) || !reflect.DeepEqual(f.removals, [][]spotify.ID{{"old"}}) {
		t.Fatalf("adds=%v removes=%v", f.writes, f.removals)
	}
}
func TestFillCanceledNeverWrites(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeCatalog{current: []spotify.ID{"old"}, albums: []Album{{ID: "album"}}, tracks: map[spotify.ID][]spotify.ID{"album": {"new"}}}
	err := Fill(ctx, f, config.Playlist{ID: "p", Artists: []config.Artist{{ID: "artist"}}})
	if !errors.Is(err, context.Canceled) || len(f.writes)+len(f.removals) != 0 {
		t.Fatalf("err=%v writes=%v removals=%v", err, f.writes, f.removals)
	}
}

func TestFillReadErrorNeverWrites(t *testing.T) {
	sentinel := errors.New("read failed")
	f := &fakeCatalog{current: []spotify.ID{"existing"}, albumErr: sentinel}
	err := Fill(context.Background(), f, config.Playlist{ID: "p", Artists: []config.Artist{{ID: "artist"}}})
	if !errors.Is(err, sentinel) || len(f.writes)+len(f.removals) != 0 {
		t.Fatalf("err=%v writes=%v removals=%v", err, f.writes, f.removals)
	}
}

func TestFillBatchesAndStopsOnWriteFailure(t *testing.T) {
	current := make([]spotify.ID, 201)
	desired := make([]spotify.ID, 201)
	for i := range current {
		current[i] = spotify.ID(fmt.Sprintf("old%d", i))
		desired[i] = spotify.ID(fmt.Sprintf("new%d", i))
	}
	p := config.Playlist{ID: "p", Artists: []config.Artist{{ID: "a"}}}
	f := &fakeCatalog{current: current, albums: []Album{{ID: "album"}}, tracks: map[spotify.ID][]spotify.ID{"album": desired}}
	if err := Fill(context.Background(), f, p); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 3 || len(f.removals) != 3 || len(f.writes[0]) != 100 || len(f.writes[2]) != 1 {
		t.Fatalf("writes=%v removals=%v", f.writes, f.removals)
	}
	sentinel := errors.New("write failed")
	f.writes = nil
	f.removals = nil
	f.writeErr = sentinel
	if err := Fill(context.Background(), f, p); !errors.Is(err, sentinel) {
		t.Fatalf("err=%v", err)
	}
	if len(f.removals) != 1 || len(f.writes) != 0 {
		t.Fatalf("retried or added after failed removal: %v %v", f.removals, f.writes)
	}
	f.current = nil
	f.writes = nil
	f.removals = nil
	if err := Fill(context.Background(), f, p); !errors.Is(err, sentinel) {
		t.Fatalf("err=%v", err)
	}
	if len(f.writes) != 1 {
		t.Fatalf("retried failed addition: %v", f.writes)
	}
}

func TestFillCanceledDuringReadNeverWrites(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &fakeCatalog{current: []spotify.ID{"old"}, albums: []Album{{ID: "album"}}, tracks: map[spotify.ID][]spotify.ID{"album": {"new"}}, onRead: cancel}
	err := Fill(ctx, f, config.Playlist{ID: "p", Artists: []config.Artist{{ID: "artist"}}})
	if !errors.Is(err, context.Canceled) || len(f.writes)+len(f.removals) != 0 {
		t.Fatalf("err=%v writes=%v removals=%v", err, f.writes, f.removals)
	}
}

func TestMissingSearchedArtistNeverWrites(t *testing.T) {
	f := &fakeCatalog{current: []spotify.ID{"existing"}, missingArtist: true}
	err := Fill(context.Background(), f, config.Playlist{ID: "p", Artists: []config.Artist{{Name: "Missing", UseNameInsteadOfURI: true}}})
	if err == nil || len(f.writes)+len(f.removals) != 0 {
		t.Fatalf("err=%v writes=%v removals=%v", err, f.writes, f.removals)
	}
}
