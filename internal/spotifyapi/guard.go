package spotifyapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

type playlistPathKey struct{}

// This guard is outside the OAuth transport: reject destinations before OAuth
// can add credentials, including SDK pagination and CurrentUser redirects.
type apiTransport struct {
	base *url.URL
	next http.RoundTripper
}

func origin(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" || (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		return strings.ToLower(u.Scheme) + "://" + host
	}
	return strings.ToLower(u.Scheme) + "://" + host + ":" + port
}

func validateDestination(u, base *url.URL, path string) error {
	if u == nil || base == nil || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.Host == "" || origin(u) != origin(base) {
		return fmt.Errorf("Spotify API destination outside configured origin")
	}
	if path != "" && u.EscapedPath() != path {
		return fmt.Errorf("Spotify playlist items destination has unexpected path")
	}
	if _, err := url.ParseQuery(u.RawQuery); err != nil {
		return fmt.Errorf("Spotify API destination has invalid query")
	}
	return nil
}

func (t apiTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	path, _ := r.Context().Value(playlistPathKey{}).(string)
	if err := validateDestination(r.URL, t.base, path); err != nil {
		return nil, err
	}
	if status, ok := r.Context().Value(responseStatusKey{}).(*atomic.Int32); ok {
		status.Store(0)
	}
	response, err := t.next.RoundTrip(r)
	if response != nil {
		if quota, ok := r.Context().Value(quotaMetadataKey{}).(*quotaMetadata); ok {
			*quota = quotaDetails(response)
		}
		if status, ok := r.Context().Value(responseStatusKey{}).(*atomic.Int32); ok {
			status.Store(int32(response.StatusCode))
		}
	}
	return response, err
}

func secureClient(raw *http.Client, baseURL string) *http.Client {
	if raw == nil {
		raw = http.DefaultClient
	}
	clone := *raw
	base, _ := url.Parse(baseURL)
	transport := raw.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	clone.Transport = apiTransport{base: base, next: &catalogueTransport{next: newReadTransport(transport), store: &catalogueStore{}}}
	redirect := clone.CheckRedirect
	clone.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		path, _ := r.Context().Value(playlistPathKey{}).(string)
		if err := validateDestination(r.URL, base, path); err != nil {
			return err
		}
		// Preserve net/http's default bound even with a custom redirect callback.
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if redirect != nil {
			if err := redirect(r, via); err != nil {
				return err
			}
		}
		return validateDestination(r.URL, base, path)
	}
	return &clone
}

func (c *Client) resolveItemsNext(current, next, expected string) (string, error) {
	previous, err := url.Parse(current)
	if err != nil {
		return "", err
	}
	reference, err := url.Parse(next)
	if err != nil {
		return "", fmt.Errorf("invalid playlist items next URL")
	}
	target := previous.ResolveReference(reference)
	base, _ := url.Parse(c.baseURL)
	initial, _ := url.Parse(expected)
	if err := validateDestination(target, base, initial.EscapedPath()); err != nil {
		return "", err
	}
	return target.String(), nil
}

func playlistContext(ctx context.Context, endpoint string) context.Context {
	u, _ := url.Parse(endpoint)
	return context.WithValue(ctx, playlistPathKey{}, u.EscapedPath())
}
