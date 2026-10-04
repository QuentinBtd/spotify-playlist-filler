package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestRunPersistentCatalogueAcrossInstances(t *testing.T) {
	root := t.TempDir()
	tokens := filepath.Join(root, "tokens")
	if e := os.Mkdir(tokens, 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("SPF_TOKEN_CACHE", tokens)
	t.Setenv("SPF_SPOTIFY_ID", "fixture-app")
	t.Setenv("SPF_SPOTIFY_SECRET", "fixture-secret")
	t.Setenv("SPF_CACHE_DIRECTORY", filepath.Join(root, "catalogue"))
	t.Setenv("SPF_LOG_LEVEL", "debug")
	hash := sha256.Sum256([]byte("fixture-app\nplaylist-modify-private playlist-modify-public playlist-read-collaborative playlist-read-private user-read-private"))
	data, e := json.Marshal(map[string]any{"version": 1, "client_id": "fixture-app", "scopes": "playlist-modify-private playlist-modify-public playlist-read-collaborative playlist-read-private user-read-private", "token": &oauth2.Token{AccessToken: "fixture-access", RefreshToken: "fixture-refresh", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}})
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(tokens, hex.EncodeToString(hash[:])+".spf-token.json"), data, 0600); e != nil {
		t.Fatal(e)
	}
	cfg := filepath.Join(root, "fixture.yaml")
	if e := os.WriteFile(cfg, []byte("playlists:\n  - uri: "+strings.Repeat("p", 22)+"\n    artists:\n      - uri: "+strings.Repeat("a", 22)+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	users, playlists, albums, tracks, writes := 0, 0, 0, 0, 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/me":
			users++
			fmt.Fprint(w, `{"id":"fixture-user"}`)
		case r.Method != "GET":
			writes++
			t.Error("unexpected write")
		case strings.Contains(r.URL.Path, "/playlists/"):
			playlists++
			fmt.Fprint(w, `{"items":[{"item":{"id":"retained","type":"track"}}],"next":null}`)
		case strings.Contains(r.URL.Path, "/artists/"):
			albums++
			fmt.Fprintf(w, `{"items":[{"id":"album"}],"next":null,"offset":0,"limit":%d,"total":1}`, fixtureCatalogueLimit(r))
		case strings.Contains(r.URL.Path, "/albums/"):
			tracks++
			fmt.Fprintf(w, `{"items":[{"id":"retained"}],"next":null,"offset":0,"limit":%d,"total":1}`, fixtureCatalogueLimit(r))
		default:
			t.Error("unexpected fixture path")
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	u, _ := url.Parse(s.URL)
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: localAPITransport{base: u}, Timeout: time.Second})
	for range 2 {
		var out, logs bytes.Buffer
		if e := run(ctx, []string{"-config", cfg}, &out, &logs); e != nil {
			t.Fatalf("run: %v logs=%s", e, &logs)
		}
		if strings.Contains(logs.String(), "fixture-") || strings.Contains(logs.String(), s.URL) {
			t.Fatalf("sensitive telemetry: %s", &logs)
		}
	}
	if users != 2 || playlists != 2 || albums != 1 || tracks != 1 || writes != 0 {
		t.Fatalf("users=%d playlists=%d albums=%d tracks=%d writes=%d", users, playlists, albums, tracks, writes)
	}
}
