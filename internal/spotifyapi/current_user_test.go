package spotifyapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zmb3/spotify/v2"
	"golang.org/x/oauth2"
)

func TestCurrentUserHasGuardedEntryPoint(t *testing.T) {
	var received atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1); fmt.Fprint(w, `{"id":"fixture"}`) }))
	defer sink.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me" {
			t.Errorf("path=%s", r.URL.Path)
		}
		http.Redirect(w, r, sink.URL+"/me", http.StatusFound)
	}))
	defer origin.Close()
	hc := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "synthetic-user-fixture"}))
	hc.Timeout = time.Second
	c := newClient(hc, origin.URL+"/")
	userClient, ok := any(c).(interface {
		CurrentUser(context.Context) (*spotify.PrivateUser, error)
	})
	if !ok {
		t.Fatal("CLI has no guarded CurrentUser entry point; its separate raw SDK client bypasses the guard")
	}
	user, err := userClient.CurrentUser(context.Background())
	if err == nil || user != nil || received.Load() != 0 {
		t.Fatalf("user=%v err=%v hostile requests=%d", user, err, received.Load())
	}
}
