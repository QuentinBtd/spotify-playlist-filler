package spotifyapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCatalogueDisabledStillRejectsIncompleteResponses(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"items":[{"id":"one"}],"total":2,"offset":0,"next":null}`)
	}))
	defer s.Close()
	c := newClient(s.Client(), s.URL+"/")
	if e := c.EnableCatalogueCache("", 0, "app", "user"); e != nil {
		t.Fatal(e)
	}
	if got, e := c.AlbumTracks(context.Background(), "a"); e == nil || got != nil {
		t.Fatalf("disabled cache allowed incomplete result: %v %v", got, e)
	}
}
