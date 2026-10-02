package spotifyauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	sdk "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2"
)

// persistentSource serializes refresh and persistence across requests.
type persistentSource struct {
	mu     sync.Mutex
	ctx    context.Context
	config *oauth2.Config
	cache  *tokenCache
	token  *oauth2.Token
	fatal  error
}

var errRevoked = errors.New("Spotify authorization revoked; log in again")

func oauthContext(ctx context.Context) context.Context {
	original, ok := ctx.Value(oauth2.HTTPClient).(*http.Client)
	if !ok {
		original = http.DefaultClient
	}
	client := *original
	client.Timeout = 30 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("OAuth redirects are not allowed") }
	return context.WithValue(ctx, oauth2.HTTPClient, &client)
}
func newSource(ctx context.Context, id, secret string, cache *tokenCache, token *oauth2.Token) *persistentSource {
	ctx = oauthContext(ctx)
	return &persistentSource{ctx: ctx, cache: cache, token: token, config: &oauth2.Config{ClientID: id, ClientSecret: secret, Endpoint: oauth2.Endpoint{TokenURL: sdk.TokenURL, AuthStyle: oauth2.AuthStyleInHeader}}}
}

type authorizationTransport struct {
	base   http.RoundTripper
	source *persistentSource
}

func (t authorizationTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		return response, err
	}
	response.Body.Close()
	t.source.mu.Lock()
	defer t.source.mu.Unlock()
	t.source.fatal = errRevoked
	if err := t.source.cache.remove(); err != nil {
		t.source.fatal = err
	}
	return nil, t.source.fatal
}
func authorizationClient(ctx context.Context, source *persistentSource) *http.Client {
	client := oauth2.NewClient(ctx, source)
	// Do not let oauth2.NewClient cache around revocation or persistence errors.
	client.Transport.(*oauth2.Transport).Source = source
	client.Transport = authorizationTransport{base: client.Transport, source: source}
	client.Timeout = 30 * time.Second
	return client
}
func (s *persistentSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fatal != nil {
		return nil, s.fatal
	}
	if s.token.Valid() {
		copy := *s.token
		return &copy, nil
	}
	next, err := s.config.TokenSource(s.ctx, s.token).Token()
	if err != nil {
		var retrieve *oauth2.RetrieveError
		if errors.As(err, &retrieve) && retrieve.Response != nil && retrieve.Response.StatusCode == 400 {
			var body struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(retrieve.Body, &body) == nil && body.Error == "invalid_grant" {
				s.fatal = errRevoked
				if err := s.cache.remove(); err != nil {
					s.fatal = err
				}
				return nil, s.fatal
			}
		}
		if s.ctx.Err() != nil {
			return nil, s.ctx.Err()
		}
		return nil, errors.New("refresh Spotify authorization failed; retry later")
	}
	if err = s.cache.save(next); err != nil {
		s.fatal = err
		return nil, err
	}
	s.token = next
	copy := *next
	return &copy, nil
}
