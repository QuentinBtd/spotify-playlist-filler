package spotifyauth

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
	// Even a regression in cache validation must never reach real OAuth/API hosts.
	http.DefaultTransport = loopbackOnlyTransport{base: http.DefaultTransport}
	os.Exit(m.Run())
}
