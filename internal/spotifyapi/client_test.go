package spotifyapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/zmb3/spotify/v2"
)

func TestReadPaginationAndSkipUnsupportedItems(t *testing.T) {
	var base string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/playlists/p/tracks":
			if r.URL.Query().Get("page") == "2" {
				fmt.Fprint(w, `{"items":[{"track":{"type":"track","id":"second"}}]}`)
				return
			}
			fmt.Fprintf(w, `{"next":%q,"items":[{"track":null},{"is_local":true,"track":{"type":"track","id":"local"}},{"track":{"type":"episode","id":"episode"}},{"track":{"type":"track","id":"first"}}]}`, base+"/playlists/p/tracks?page=2")
		case "/artists/a/albums":
			if r.URL.Query().Get("page") == "2" {
				fmt.Fprint(w, `{"items":[{"id":"album2"}]}`)
				return
			}
			fmt.Fprintf(w, `{"next":%q,"items":[{"id":"album1"}]}`, base+"/artists/a/albums?page=2")
		case "/albums/a/tracks":
			if r.URL.Query().Get("page") == "2" {
				fmt.Fprint(w, `{"items":[{"id":"track2"}]}`)
				return
			}
			fmt.Fprintf(w, `{"next":%q,"items":[{"id":"track1"}]}`, base+"/albums/a/tracks?page=2")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	base = server.URL
	c := New(spotify.New(server.Client(), spotify.WithBaseURL(base+"/"), spotify.WithRetry(false)))
	tracks, err := c.PlaylistTracks(context.Background(), "p")
	if err != nil || !reflect.DeepEqual(tracks, []spotify.ID{"first", "second"}) {
		t.Fatalf("playlist=%v err=%v", tracks, err)
	}
	albums, err := c.ArtistAlbums(context.Background(), "a")
	if err != nil || len(albums) != 2 || albums[1].ID != "album2" {
		t.Fatalf("albums=%v err=%v", albums, err)
	}
	tracks, err = c.AlbumTracks(context.Background(), "a")
	if err != nil || !reflect.DeepEqual(tracks, []spotify.ID{"track1", "track2"}) {
		t.Fatalf("tracks=%v err=%v", tracks, err)
	}
}

func TestReadErrorsAndSearchPagination(t *testing.T) {
	var base string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/search" {
			if r.URL.Query().Get("offset") == "1" {
				fmt.Fprint(w, `{"artists":{"items":[{"id":"match","name":"Exact"}]}}`)
				return
			}
			fmt.Fprintf(w, `{"artists":{"next":%q,"limit":1,"offset":0,"items":[{"id":"other","name":"Other"}]}}`, base+"/search?offset=1")
			return
		}
		if r.URL.Path == "/albums/paged/tracks" {
			fmt.Fprintf(w, `{"next":%q,"items":[{"id":"partial"}]}`, base+"/fail")
			return
		}
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":{"status":403,"message":"denied"}}`)
	}))
	defer server.Close()
	base = server.URL
	c := New(spotify.New(server.Client(), spotify.WithBaseURL(base+"/"), spotify.WithRetry(false)))
	id, err := c.SearchArtist(context.Background(), "Exact")
	if err != nil || id != "match" {
		t.Fatalf("artist=%q err=%v", id, err)
	}
	id, err = c.SearchArtist(context.Background(), "Missing")
	if err != nil || id != "" {
		t.Fatalf("artist=%q err=%v", id, err)
	}
	if _, err = c.PlaylistTracks(context.Background(), "bad"); err == nil {
		t.Fatal("ignored playlist error")
	}
	if _, err = c.ArtistAlbums(context.Background(), "bad"); err == nil {
		t.Fatal("ignored artist error")
	}
	for _, id := range []spotify.ID{"bad", "paged"} {
		if tracks, err := c.AlbumTracks(context.Background(), id); err == nil || tracks != nil {
			t.Fatalf("partial result returned: tracks=%v err=%v", tracks, err)
		}
	}
	if _, err = c.SearchArtist(context.Background(), "Exact"); err != nil {
		t.Fatal(err)
	}
}

func TestWriteRequestsUseTrackURIs(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.URL.Path != "/playlists/p/tracks" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if r.Method == http.MethodPost {
			var uris []string
			if err := json.Unmarshal(body["uris"], &uris); err != nil {
				t.Error(err)
			}
			if !reflect.DeepEqual(uris, []string{"spotify:track:a", "spotify:track:b"}) {
				t.Errorf("uris=%v", uris)
			}
		} else {
			var tracks []struct {
				URI string `json:"uri"`
			}
			if err := json.Unmarshal(body["tracks"], &tracks); err != nil {
				t.Error(err)
			}
			if len(tracks) != 2 || tracks[0].URI != "spotify:track:a" || tracks[1].URI != "spotify:track:b" {
				t.Errorf("tracks=%v", tracks)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"snapshot_id":"snapshot"}`)
	}))
	defer server.Close()
	c := New(spotify.New(server.Client(), spotify.WithBaseURL(server.URL+"/"), spotify.WithRetry(false)))
	if err := c.AddTracks(context.Background(), "p", "a", "b"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveTracks(context.Background(), "p", "a", "b"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(methods, []string{"POST", "DELETE"}) {
		t.Fatalf("methods=%v", methods)
	}
}
