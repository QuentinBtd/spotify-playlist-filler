package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
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

type localAPITransport struct{ base *url.URL }

func (transport localAPITransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "api.spotify.com" {
		return nil, fmt.Errorf("unexpected integration destination")
	}
	clone := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = transport.base.Scheme
	u.Host = transport.base.Host
	clone.URL = &u
	return http.DefaultTransport.RoundTrip(clone)
}
func TestRunCachedAuthenticationStopsOnRevocationWithoutMutationRetry(t *testing.T) {
	for _, failure := range []string{"read", "mutation"} {
		t.Run(failure, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "cache")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SPF_TOKEN_CACHE", dir)
			t.Setenv("SPOTIFY_ID", "test-id")
			t.Setenv("SPOTIFY_SECRET", "synthetic-secret")
			scopes := "playlist-modify-private playlist-modify-public playlist-read-collaborative playlist-read-private user-read-private"
			hash := sha256.Sum256([]byte("test-id\n" + scopes))
			path := filepath.Join(dir, hex.EncodeToString(hash[:])+".spf-token.json")
			token := &oauth2.Token{AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}
			data, err := json.Marshal(map[string]any{"version": 1, "client_id": "test-id", "scopes": scopes, "token": token})
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			cfg := filepath.Join(t.TempDir(), "config.yml")
			text := fmt.Sprintf("playlists:\n  - name: fixture\n    uri: %s\n    artists:\n      - uri: %s\n", strings.Repeat("b", 22), strings.Repeat("a", 22))
			if err := os.WriteFile(cfg, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer synthetic-access" {
					t.Error("missing cached authorization")
				}
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/v1/me":
					fmt.Fprint(w, `{"id":"fixture-user"}`)
				case r.Method == "DELETE" || r.Method == "POST":
					writes++
					w.WriteHeader(401)
					fmt.Fprint(w, `{"error":{"status":401,"message":"synthetic-refresh"}}`)
				case strings.Contains(r.URL.Path, "/playlists/"):
					fmt.Fprint(w, `{"items":[{"item":{"id":"obsolete","type":"track"}}],"next":null}`)
				case failure == "read":
					w.WriteHeader(401)
					fmt.Fprint(w, `{"error":{"status":401,"message":"synthetic-refresh"}}`)
				default:
					fmt.Fprint(w, `{"items":[],"next":"","limit":20,"offset":0,"total":0}`)
				}
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: localAPITransport{base: base}, Timeout: time.Second})
			listener, err := net.Listen("tcp", "127.0.0.1:8080")
			if err != nil {
				t.Skip("callback port occupied")
			}
			defer listener.Close()
			var out, stderr bytes.Buffer
			err = run(ctx, []string{"-config", cfg}, &out, &stderr)
			expected := 0
			if failure == "mutation" {
				expected = 1
			}
			if err == nil || strings.Contains(err.Error(), "synthetic-") || strings.Contains(out.String(), "Please log in") || writes != expected {
				t.Fatalf("unsafe CLI auth failure: writes=%d err=%v output=%s", writes, err, &out)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("revoked cache retained")
			}
		})
	}
}
