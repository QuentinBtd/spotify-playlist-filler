package spotifyauth

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/diagnostics"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSafeOAuth401Diagnostics(t *testing.T) {
	_, path := cacheFixture(t, validToken())
	client, err := Login(context.Background(), "test-id", "synthetic-secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(401)
		fmt.Fprint(w, "SECRET_BODY SECRET_TOKEN")
	}))
	defer server.Close()
	_, err = client.Get(server.URL)
	if !errors.Is(err, errRevoked) {
		t.Fatal("lost revocation identity")
	}
	fields := fmt.Sprint(diagnostics.Fields(err))
	if !strings.Contains(fields, "http_status 401") || !strings.Contains(fields, "failure_kind oauth") || strings.Contains(fields, "SECRET_") {
		t.Fatalf("unsafe/missing OAuth fields: %s", fields)
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("revoked token not invalidated")
	}
	_, err = client.Post(server.URL, "application/json", strings.NewReader(`{}`))
	if err == nil || requests != 1 {
		t.Fatal("revocation allowed mutation/retry")
	}
}
