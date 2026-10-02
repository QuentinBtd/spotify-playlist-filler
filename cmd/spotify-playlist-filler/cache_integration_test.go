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
	for _, failure := range []string{"read", "mutation", "read403", "read400", "read404"} {
		t.Run(failure, func(t *testing.T) {
			status := 401
			switch failure {
			case "read403":
				status = 403
			case "read400":
				status = 400
			case "read404":
				status = 404
			}
			dir := filepath.Join(t.TempDir(), "cache")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SPF_TOKEN_CACHE", dir)
			t.Setenv("SPF_SPOTIFY_ID", "test-id")
			t.Setenv("SPF_LOG_LEVEL", "debug")
			t.Setenv("SPF_SPOTIFY_SECRET", "synthetic-secret")
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
				case strings.HasPrefix(failure, "read"):
					w.WriteHeader(status)
					fmt.Fprintf(w, `{"error":{"status":%d,"message":"synthetic-refresh SECRET_BODY SECRET_TOKEN"}}`, status)
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
			if err == nil || (status == 401 && strings.Contains(err.Error(), "synthetic-")) || strings.Contains(out.String(), "Please log in") || writes != expected {
				t.Fatalf("unsafe CLI auth failure: writes=%d err=%v output=%s", writes, err, &out)
			}
			guidance, operation, kind := "playlist unchanged", "artist_albums", "http"
			if status == 401 {
				kind = "oauth"
			}
			if failure == "mutation" {
				guidance, operation = "inspect playlists before rerunning", "remove_items"
			}
			if strings.Count(stderr.String(), "level=ERROR") != 1 || !strings.Contains(stderr.String(), guidance) || !strings.Contains(stderr.String(), "stage=synchronization") || !strings.Contains(stderr.String(), "operation="+operation) || !strings.Contains(stderr.String(), "failure_kind="+kind) || !strings.Contains(stderr.String(), fmt.Sprintf("http_status=%d", status)) || strings.Contains(stderr.String(), "synthetic-") || strings.Contains(stderr.String(), "SECRET_") || strings.Contains(stderr.String(), "Authorization") {
				t.Fatalf("unsafe/missing failure log: %s", &stderr)
			}
			if failure == "mutation" && !strings.Contains(stderr.String(), "remove batch") {
				t.Fatal("missing batch progress")
			}
			_, cacheErr := os.Stat(path)
			if status == 401 && !os.IsNotExist(cacheErr) {
				t.Fatal("revoked cache retained")
			}
			if status != 401 && cacheErr != nil {
				t.Fatal("non-revocation failure removed cache")
			}
		})
	}
}
