package main

import (
	"errors"
	"net"
	"net/http"
	"os"
	"testing"
)

type loopbackOnlyTransport struct{ base http.RoundTripper }

func (transport loopbackOnlyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	host := r.URL.Hostname()
	if host != "localhost" && !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("tests prohibit non-loopback network requests")
	}
	return transport.base.RoundTrip(r)
}
func TestMain(m *testing.M) {
	// Ignore ambient application settings; every fixture uses synthetic values.
	for _, name := range []string{"SPF_SPOTIFY_ID", "SPF_SPOTIFY_SECRET", "SPOTIFY_ID", "SPOTIFY_SECRET", "SPF_CONFIG", "CONFIG", "SPF_LOG_LEVEL", "SPF_VERBOSE", "SPF_OAUTH_PORT", "SPF_TOKEN_CACHE", "SPF_CACHE_DIRECTORY", "SPF_CACHE_TTL"} {
		os.Unsetenv(name)
	}
	// Even a regression in cache validation must never reach real OAuth/API hosts.
	http.DefaultTransport = loopbackOnlyTransport{base: http.DefaultTransport}
	dir, err := os.MkdirTemp("", "spf-test-catalogue-")
	if err != nil {
		panic(err)
	}
	os.Setenv("SPF_CACHE_DIRECTORY", dir+"/catalogue")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
