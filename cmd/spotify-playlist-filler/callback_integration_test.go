package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type callbackFixtureTransport struct{ base *url.URL }

func (transport callbackFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || (r.URL.Host != "api.spotify.com" && r.URL.Host != "accounts.spotify.com") {
		return nil, fmt.Errorf("unexpected fixture destination")
	}
	clone := r.Clone(r.Context())
	u := *r.URL
	u.Scheme, u.Host = transport.base.Scheme, transport.base.Host
	clone.URL = &u
	return http.DefaultTransport.RoundTrip(clone)
}

func TestRunExchangeRedirectAndReuseCacheAcrossPortChanges(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	path := portConfig(t, 1) // The nonempty env must override YAML through the real CLI.
	t.Setenv("SPF_OAUTH_PORT", strconv.Itoa(port))
	if err := os.WriteFile(path, []byte("oauth_port: 1\nplaylists:\n  - uri: "+strings.Repeat("b", 22)+"\n    artists:\n      - uri: "+strings.Repeat("a", 22)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", port)
	var exchanges, reads, writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/token":
			exchanges.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("redirect_uri") != redirect || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "fixture-code" {
				t.Error("code exchange does not use selected redirect")
			}
			fmt.Fprint(w, `{"access_token":"fixture-access","refresh_token":"fixture-refresh","token_type":"Bearer","expires_in":3600}`)
		case r.Method != http.MethodGet:
			writes.Add(1)
			w.WriteHeader(500)
		case r.URL.Path == "/v1/me":
			reads.Add(1)
			if r.Header.Get("Authorization") != "Bearer fixture-access" {
				t.Error("missing authorization")
			}
			fmt.Fprint(w, `{"id":"fixture-user"}`)
		case strings.Contains(r.URL.Path, "/playlists/"):
			fmt.Fprint(w, `{"items":[],"next":null}`)
		case strings.Contains(r.URL.Path, "/artists/"):
			fmt.Fprint(w, `{"items":[],"next":"","limit":20,"offset":0,"total":0}`)
		default:
			t.Errorf("unexpected fixture route: %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: callbackFixtureTransport{base: base}}), 5*time.Second)
	defer cancel()
	lines, done := make(chan string, 8), make(chan error, 1)
	go func() {
		done <- run(ctx, []string{path}, loginOutput(func(b []byte) (int, error) { lines <- string(b); return len(b), nil }), io.Discard)
	}()
	var line string
	select {
	case line = <-lines:
	case err := <-done:
		t.Fatalf("before auth URL: %v", err)
	case <-ctx.Done():
		t.Fatal("auth URL timeout")
	}
	fields := strings.Fields(line)
	u, err := url.Parse(fields[len(fields)-1])
	if err != nil || u.Query().Get("redirect_uri") != redirect {
		t.Fatalf("unexpected selected redirect: %v", err)
	}
	response, err := (&http.Client{Timeout: time.Second}).Get(redirect + "?state=" + url.QueryEscape(u.Query().Get("state")) + "&code=fixture-code")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("callback status=%d", response.StatusCode)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("CLI did not finish")
	}
	files, err := filepath.Glob(filepath.Join(os.Getenv("SPF_TOKEN_CACHE"), "*.spf-token.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("cache files=%d err=%v", len(files), err)
	}
	before, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	// Both same and changed callback ports are deliberately occupied on cache hits.
	for _, address := range []string{fmt.Sprintf("127.0.0.1:%d", port), "127.0.0.1:0"} {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("SPF_OAUTH_PORT", strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
		var output bytes.Buffer
		err = run(ctx, []string{"-config", path}, &output, io.Discard)
		listener.Close()
		if err != nil || strings.Contains(output.String(), "Please log in") {
			t.Fatalf("cached run tried listener: %v", err)
		}
	}
	after, err := os.ReadFile(files[0])
	if err != nil || !bytes.Equal(before, after) || exchanges.Load() != 1 || reads.Load() != 3 || writes.Load() != 0 {
		t.Fatalf("cache changed or requests unsafe: exchanges=%d reads=%d writes=%d err=%v", exchanges.Load(), reads.Load(), writes.Load(), err)
	}
}

func TestRunInvalidPortBeforeCacheOrNoPlaylistShortcut(t *testing.T) {
	for _, text := range []string{"", "playlists:\n  - uri: playlist\n    artists:\n      - uri: artist\n"} {
		t.Run(fmt.Sprintf("length%d", len(text)), func(t *testing.T) {
			path := portConfig(t, 8080)
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SPF_OAUTH_PORT", "private-port-value")
			var output bytes.Buffer
			err := run(context.Background(), []string{path}, &output, io.Discard)
			if err == nil || err.Error() != "SPF_OAUTH_PORT must be an integer from 1 to 65535" || output.Len() != 0 {
				t.Fatalf("invalid port bypass: err=%v", err)
			}
			if _, err := os.Stat(os.Getenv("SPF_TOKEN_CACHE")); !os.IsNotExist(err) {
				t.Fatal("invalid port touched cache")
			}
		})
	}
}

func TestRunNoPlaylistsWithOccupiedCallbackPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	path := portConfig(t, listener.Addr().(*net.TCPAddr).Port)
	if err := os.WriteFile(path, []byte("playlists: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPF_OAUTH_PORT", strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
	var output bytes.Buffer
	if err := run(context.Background(), []string{path}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "No playlists configured") {
		t.Fatal("not a no-op")
	}
	if _, err := os.Stat(os.Getenv("SPF_TOKEN_CACHE")); !os.IsNotExist(err) {
		t.Fatal("no-op touched cache")
	}
}
