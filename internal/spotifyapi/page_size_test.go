package spotifyapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCatalogueUsesCurrentMaximumPageSizes(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "10"
		if r.URL.Path == "/albums/a/tracks" {
			want = "50"
		}
		if r.URL.Query().Get("limit") != want {
			t.Errorf("path=%s limit=%s want=%s", r.URL.Path, r.URL.Query().Get("limit"), want)
		}
		if r.URL.Path == "/search" {
			fmt.Fprintf(w, `{"artists":{"items":[],"next":null,"offset":0,"limit":%d,"total":0}}`, fixtureCatalogueLimit(r))
		} else {
			fmt.Fprintf(w, `{"items":[],"next":null,"offset":0,"limit":%d,"total":0}`, fixtureCatalogueLimit(r))
		}
	}))
	defer s.Close()
	c := newClient(s.Client(), s.URL+"/")
	ctx := context.Background()
	if _, e := c.ArtistAlbums(ctx, "a"); e != nil {
		t.Fatal(e)
	}
	if _, e := c.AlbumTracks(ctx, "a"); e != nil {
		t.Fatal(e)
	}
	if _, e := c.SearchArtist(ctx, "Synthetic"); e != nil {
		t.Fatal(e)
	}
}
