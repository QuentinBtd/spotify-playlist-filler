package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type loginOutput func([]byte) (int, error)

func (write loginOutput) Write(b []byte) (int, error) { return write(b) }

func portConfig(t *testing.T, port int) string {
	t.Helper()
	t.Setenv("SPF_OAUTH_PORT", "")
	t.Setenv("SPF_SPOTIFY_ID", "fixture-id")
	t.Setenv("SPF_SPOTIFY_SECRET", "fixture-secret")
	t.Setenv("SPF_TOKEN_CACHE", filepath.Join(t.TempDir(), "tokens"))
	path := filepath.Join(t.TempDir(), "config.yaml")
	text := fmt.Sprintf("oauth_port: %d\nplaylists:\n  - uri: playlist\n    artists:\n      - uri: artist\n", port)
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunSelectedPortURLListenerAndCancellation(t *testing.T) {
	for _, level := range []string{"warn", "error"} {
		t.Run(level, func(t *testing.T) {
			t.Setenv("SPF_LOG_LEVEL", level)
			testSelectedPortURLListenerAndCancellation(t)
		})
	}
}

func testSelectedPortURLListenerAndCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	path := portConfig(t, port)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lines := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"-config", path}, loginOutput(func(b []byte) (int, error) { lines <- string(b); return len(b), nil }), &bytes.Buffer{})
	}()
	var line string
	select {
	case line = <-lines:
	case err := <-done:
		t.Fatalf("login failed: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("no auth URL")
	}
	fields := strings.Fields(line)
	u, err := url.Parse(fields[len(fields)-1])
	if err != nil {
		t.Fatal(err)
	}
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", port)
	if u.Query().Get("redirect_uri") != redirect {
		t.Fatalf("selected port ignored: redirect=%q want=%q", u.Query().Get("redirect_uri"), redirect)
	}
	response, err := (&http.Client{Timeout: time.Second}).Get(redirect + "?state=wrong")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d", response.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled login stuck")
	}
	listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("listener leaked: %v", err)
	}
	listener.Close()
}

func TestRunSelectedPortCollision(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	path := portConfig(t, listener.Addr().(*net.TCPAddr).Port)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	var out, stderr bytes.Buffer
	err = run(ctx, []string{path}, &out, &stderr)
	if err == nil || !strings.Contains(err.Error(), "listen for OAuth callback") || out.Len() != 0 || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatalf("collision: err=%v output=%s", err, &out)
	}
}
