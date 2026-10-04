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
		case "/playlists/p/items":
			if r.URL.Query().Get("page") == "2" {
				fmt.Fprint(w, `{"items":[{"item":{"type":"track","id":"second"}}],"next":null}`)
				return
			}
			// Synthetic fixtures for the current contract, not recorded account data.
			fmt.Fprintf(w, `{"next":%q,"items":[{"item":null},{"is_local":true,"item":{"type":"track","id":"local"}},{"item":{"type":"track","id":"inner-local","is_local":true}},{"item":{"type":"episode","id":"episode"}},{"item":{"type":"future-type","id":"unknown"}},{"item":{"type":"track","id":""}},{"item":{"type":"track","id":"first","album":{"id":"album"},"artists":[{"id":"artist"}]},"track":{"type":"track","id":"legacy-decoy"}}]}`, base+"/playlists/p/items?page=2")
		case "/artists/a/albums":
			if r.URL.Query().Get("offset") == "10" {
				fmt.Fprint(w, `{"items":[{"id":"album2"}],"next":null,"offset":10,"limit":10,"total":11}`)
				return
			}
			fmt.Fprintf(w, `{"next":%q,"items":[%s],"offset":0,"limit":10,"total":11}`, base+"/artists/a/albums?limit=10&offset=10", fixtureRepeatedItems("album1", 10))
		case "/albums/a/tracks":
			if r.URL.Query().Get("offset") == "50" {
				fmt.Fprint(w, `{"items":[{"id":"track2"}],"next":null,"offset":50,"limit":50,"total":51}`)
				return
			}
			fmt.Fprintf(w, `{"next":%q,"items":[%s],"offset":0,"limit":50,"total":51}`, base+"/albums/a/tracks?limit=50&offset=50", fixtureRepeatedItems("track1", 50))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	base = server.URL
	c := newClient(server.Client(), base+"/")
	tracks, err := c.PlaylistTracks(context.Background(), "p")
	if err != nil || !reflect.DeepEqual(tracks, []spotify.ID{"first", "second"}) {
		t.Fatalf("playlist=%v err=%v", tracks, err)
	}
	albums, err := c.ArtistAlbums(context.Background(), "a")
	if err != nil || len(albums) != 11 || albums[10].ID != "album2" {
		t.Fatalf("albums=%v err=%v", albums, err)
	}
	tracks, err = c.AlbumTracks(context.Background(), "a")
	if err != nil || len(tracks) != 51 || tracks[0] != "track1" || tracks[50] != "track2" {
		t.Fatalf("tracks=%v err=%v", tracks, err)
	}
}

func TestReadErrorsAndSearchPagination(t *testing.T) {
	var base string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/search" {
			if r.URL.Query().Get("offset") == "10" {
				fmt.Fprint(w, `{"artists":{"items":[{"id":"match","name":"Exact"}],"next":null,"offset":10,"limit":10,"total":11}}`)
				return
			}
			items := make([]map[string]string, 10)
			for i := range items {
				items[i] = map[string]string{"id": "other", "name": "Other"}
			}
			query := r.URL.Query()
			query.Set("offset", "10")
			_ = json.NewEncoder(w).Encode(map[string]any{"artists": map[string]any{"next": base + "/search?" + query.Encode(), "limit": 10, "offset": 0, "total": 11, "items": items}})
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
	c := newClient(server.Client(), base+"/")
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
		if r.URL.Path != "/playlists/p/items" {
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
			if err := json.Unmarshal(body["items"], &tracks); err != nil {
				t.Error(err)
			}
			if len(tracks) != 2 || tracks[0].URI != "spotify:track:a" || tracks[1].URI != "spotify:track:b" {
				t.Errorf("tracks=%v", tracks)
			}
		}
		if len(body) != 1 || r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected payload/header/query: %v %s %s", body, r.Header.Get("Content-Type"), r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
		}
		fmt.Fprint(w, `{"snapshot_id":"snapshot"}`)
	}))
	defer server.Close()
	c := newClient(server.Client(), server.URL+"/")
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
