// Package spotifyauth manages a single interactive OAuth login on loopback.
package spotifyauth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/zmb3/spotify/v2"
	sdk "github.com/zmb3/spotify/v2/auth"
)

// RedirectURI must also be registered in the Spotify application dashboard.
const RedirectURI = "http://127.0.0.1:8080/callback"

func randomState() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate OAuth state: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}

func callbackHandler(state string, result chan<- *spotify.Client, exchange func(*http.Request) (*spotify.Client, error)) http.Handler {
	var mu sync.Mutex
	completed := false
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(state)) != 1 {
			http.Error(w, "Invalid OAuth state", http.StatusForbidden)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if completed {
			http.Error(w, "Login already completed", http.StatusConflict)
			return
		}
		client, err := exchange(r)
		if err != nil {
			http.Error(w, "Spotify authorization failed. Please try again.", http.StatusForbidden)
			return
		}
		select {
		case result <- client:
			completed = true
			fmt.Fprintln(w, "Login completed. You can close this window.")
		case <-r.Context().Done():
			http.Error(w, "Login canceled", http.StatusRequestTimeout)
		}
	})
}

// Login waits up to five minutes. No token is persisted or logged.
func Login(ctx context.Context, id, secret string, output io.Writer) (*spotify.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	state, err := randomState()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:8080")
	if err != nil {
		return nil, fmt.Errorf("listen for OAuth callback: %w", err)
	}
	defer listener.Close()
	auth := sdk.New(sdk.WithClientID(id), sdk.WithClientSecret(secret), sdk.WithRedirectURL(RedirectURI), sdk.WithScopes(sdk.ScopeUserReadPrivate, sdk.ScopePlaylistReadPrivate, sdk.ScopePlaylistReadCollaborative, sdk.ScopePlaylistModifyPublic, sdk.ScopePlaylistModifyPrivate))
	result := make(chan *spotify.Client, 1)
	mux := http.NewServeMux()
	mux.Handle("/callback", callbackHandler(state, result, func(r *http.Request) (*spotify.Client, error) {
		exchangeCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		token, err := auth.Token(exchangeCtx, state, r)
		if err != nil {
			return nil, err
		}
		// Use the application's lifetime, not the soon-to-be-canceled callback context.
		httpClient := auth.Client(ctx, token)
		httpClient.Timeout = 30 * time.Second
		return spotify.New(httpClient, spotify.WithRetry(false)), nil
	}))
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	defer func() {
		// Let the successful callback finish writing its browser response.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			server.Close()
		}
	}()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(listener) }()
	if output == nil {
		output = io.Discard
	}
	if _, err := fmt.Fprintln(output, "Please log in to Spotify by visiting:", auth.AuthURL(state)); err != nil {
		return nil, fmt.Errorf("print login URL: %w", err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	select {
	case client := <-result:
		return client, nil
	case err := <-serverErr:
		return nil, fmt.Errorf("OAuth callback server: %w", err)
	case <-waitCtx.Done():
		return nil, fmt.Errorf("wait for Spotify login: %w", waitCtx.Err())
	}
}
