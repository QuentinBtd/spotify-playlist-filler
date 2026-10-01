package spotifyauth

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zmb3/spotify/v2"
)

func TestCallbackRejectsStateBeforeExchangeAndCompletesOnce(t *testing.T) {
	calls := 0
	expected := spotify.New(http.DefaultClient)
	result := make(chan *spotify.Client, 1)
	handler := callbackHandler("expected", result, func(r *http.Request) (*spotify.Client, error) { calls++; return expected, nil })
	for _, state := range []string{"", "wrong"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "/callback?state="+state, nil))
		if w.Code != http.StatusForbidden || calls != 0 {
			t.Fatalf("state=%q status=%d calls=%d", state, w.Code, calls)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/callback?state=expected&code=test", nil))
	if w.Code != http.StatusOK || <-result != expected {
		t.Fatal("login did not complete")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/callback?state=expected&code=test", nil))
	if w.Code != http.StatusConflict || calls != 1 {
		t.Fatalf("duplicate status=%d calls=%d", w.Code, calls)
	}
}

func TestCallbackExchangeErrorDoesNotExposeSecret(t *testing.T) {
	handler := callbackHandler("state", make(chan *spotify.Client, 1), func(*http.Request) (*spotify.Client, error) { return nil, errors.New("private token detail") })
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/callback?state=state", nil))
	if w.Code != http.StatusForbidden || w.Body.String() != "Spotify authorization failed. Please try again.\n" {
		t.Fatalf("response: %d %s", w.Code, w.Body)
	}
}

func TestRandomState(t *testing.T) {
	a, err := randomState()
	if err != nil {
		t.Fatal(err)
	}
	b, err := randomState()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 64 || a == b {
		t.Fatalf("bad states %q %q", a, b)
	}
}

func TestLoginCanceledDoesNotListen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Login(ctx, "id", "secret", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestRedirectUsesExplicitLoopback(t *testing.T) {
	if RedirectURI != "http://127.0.0.1:8080/callback" {
		t.Fatalf("unsupported Spotify redirect URI: %s", RedirectURI)
	}
}

type outputWriter func([]byte) (int, error)

func (write outputWriter) Write(data []byte) (int, error) { return write(data) }

func TestLoginLocalServerCanceledAndClosed(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:8080")
	if err != nil {
		t.Skip("local callback port already occupied")
	}
	probe.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		_, err := Login(ctx, "test-id", "test-secret", outputWriter(func(data []byte) (int, error) { output <- string(data); return len(data), nil }))
		done <- err
	}()
	var loginURL string
	select {
	case line := <-output:
		loginURL = strings.Fields(line)[len(strings.Fields(line))-1]
	case err := <-done:
		t.Fatalf("login failed before URL: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("login did not start")
	}
	parsed, err := url.Parse(loginURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("redirect_uri") != RedirectURI || len(parsed.Query().Get("state")) != 64 || strings.Contains(loginURL, "test-secret") {
		t.Fatalf("unsafe auth URL: %s", loginURL)
	}
	localClient := &http.Client{Timeout: 3 * time.Second}
	response, err := localClient.Get(RedirectURI + "?state=wrong")
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
		t.Fatal("login did not stop")
	}
	probe, err = net.Listen("tcp", "127.0.0.1:8080")
	if err != nil {
		t.Fatalf("callback listener leaked: %v", err)
	}
	probe.Close()
}
