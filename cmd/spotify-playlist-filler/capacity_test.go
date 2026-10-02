package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/app"
	"golang.org/x/oauth2"
)

func TestRunCapacitySkipsUnchangedAndContinues(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPF_TOKEN_CACHE", dir)
	t.Setenv("SPF_SPOTIFY_ID", "fixture-client")
	t.Setenv("SPF_SPOTIFY_SECRET", "PRIVATE_SECRET")
	t.Setenv("SPF_LOG_LEVEL", "debug")
	scopes := "playlist-modify-private playlist-modify-public playlist-read-collaborative playlist-read-private user-read-private"
	hash := sha256.Sum256([]byte("fixture-client\n" + scopes))
	token := &oauth2.Token{AccessToken: "PRIVATE_TOKEN", RefreshToken: "PRIVATE_REFRESH", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}
	data, err := json.Marshal(map[string]any{"version": 1, "client_id": "fixture-client", "scopes": scopes, "token": token})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, hex.EncodeToString(hash[:])+".spf-token.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	large, small, artist := strings.Repeat("b", 22), strings.Repeat("d", 22), strings.Repeat("a", 22)
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	text := fmt.Sprintf("playlists:\n  - name: PRIVATE_NAME\n    uri: %s\n    artists:\n      - uri: %s\n  - uri: %s\n    artists:\n      - uri: %s\n", large, artist, small, strings.Repeat("c", 22))
	if err = os.WriteFile(cfg, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	writes := map[string]int{}
	readNext := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/me":
			fmt.Fprint(w, `{"id":"fixture"}`)
		case r.Method == "DELETE" || r.Method == "POST":
			writes[r.URL.Path]++
			if r.Method == "POST" {
				w.WriteHeader(http.StatusCreated)
			}
			fmt.Fprint(w, `{"snapshot_id":"fixture"}`)
		case strings.Contains(r.URL.Path, "/playlists/"):
			if strings.Contains(r.URL.Path, small) {
				readNext = true
			}
			fmt.Fprint(w, `{"items":[{"item":{"id":"obsolete","type":"track"}}],"next":null}`)
		case strings.Contains(r.URL.Path, "/artists/"):
			album := "small"
			if strings.Contains(r.URL.Path, artist) {
				album = "large"
			}
			fmt.Fprintf(w, `{"items":[{"id":%q}],"next":null}`, album)
		case strings.Contains(r.URL.Path, "/albums/large/"):
			items := make([]map[string]string, 10001)
			for i := range items {
				items[i] = map[string]string{"id": fmt.Sprintf("track%d", i)}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "next": nil})
		case strings.Contains(r.URL.Path, "/albums/small/"):
			fmt.Fprint(w, `{"items":[{"id":"new"}],"next":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: localAPITransport{base: base}, Timeout: time.Second})
	var out, logs bytes.Buffer
	err = run(ctx, []string{"-config", cfg}, &out, &logs)
	if !errors.Is(err, app.ErrPlaylistCapacity) {
		t.Errorf("capacity skip must be identifiable and exit nonzero: %v", err)
	}
	if writes["/v1/playlists/"+large+"/items"] != 0 {
		t.Error("oversized playlist mutated")
	}
	if !readNext || writes["/v1/playlists/"+small+"/items"] != 2 {
		t.Errorf("next playlist not processed: read=%t writes=%v", readNext, writes)
	}
	for _, want := range []string{`level=WARN msg="playlist capacity exceeded; skipping unchanged" playlist=1`, "projected_total=10001", `msg="playlist sync complete" playlist=2`, "skipped=1"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("missing %q in %s", want, &logs)
		}
	}
	for _, secret := range []string{"PRIVATE_NAME", "PRIVATE_SECRET", "PRIVATE_TOKEN", server.URL} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("private data logged: %s", secret)
		}
	}
	if out.Len() != 0 {
		t.Error("telemetry on stdout")
	}
}
