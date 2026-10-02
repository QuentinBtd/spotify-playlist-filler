package spotifyapi

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/app"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type diagnosticError interface {
	DiagnosticFields() []any
	PlaylistUnchanged() bool
}

func TestSafeCatalogueDiagnostics(t *testing.T) {
	for _, status := range []int{403, 400, 404, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					writes.Add(1)
				}
				if strings.HasPrefix(r.URL.Path, "/artists/") {
					w.WriteHeader(status)
					fmt.Fprintf(w, `{"error":{"status":%d,"message":"SECRET_BODY token=SECRET_TOKEN"}}`, status)
					return
				}
				fmt.Fprint(w, `{"items":[],"next":null}`)
			}))
			defer server.Close()
			err := app.FillWithConcurrency(context.Background(), newClient(server.Client(), server.URL+"/"), config.Playlist{ID: "p", Name: "PRIVATE_PLAYLIST", Artists: []config.Artist{{ID: "a", Name: "PRIVATE_ARTIST"}}}, 3)
			assertDiagnostic(t, err, "artist_albums", "http", status, true)
			if writes.Load() != 0 {
				t.Fatal("read failure mutated playlist")
			}
		})
	}
}
func assertDiagnostic(t *testing.T, err error, operation, kind string, status int, unchanged bool) {
	t.Helper()
	var d diagnosticError
	if !errors.As(err, &d) {
		t.Fatalf("missing safe diagnostic fields (generic synchronization failure): %T", err)
	}
	fields := d.DiagnosticFields()
	m := map[string]any{}
	for i := 0; i < len(fields); i += 2 {
		m[fields[i].(string)] = fields[i+1]
	}
	if m["operation"] != operation || m["failure_kind"] != kind || (status != 0 && m["http_status"] != status) || d.PlaylistUnchanged() != unchanged {
		t.Fatalf("wrong safe fields: %v unchanged=%v", fields, d.PlaylistUnchanged())
	}
	for _, s := range []string{"SECRET_BODY", "SECRET_TOKEN", "PRIVATE_PLAYLIST", "PRIVATE_ARTIST", "http://"} {
		if strings.Contains(fmt.Sprint(fields), s) {
			t.Fatal("unsafe field leaked")
		}
	}
	if status == 0 && m["http_status"] != nil {
		t.Fatalf("invented status: %v", fields)
	}
}
func TestSafeCatalogueTimeoutCanceled(t *testing.T) {
	for _, mode := range []string{"timeout", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			entered := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/artists/") {
					close(entered)
					<-r.Context().Done()
					return
				}
				fmt.Fprint(w, `{"items":[],"next":null}`)
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			raw := server.Client()
			if mode == "timeout" {
				raw.Timeout = 30 * time.Millisecond
			} else {
				go func() { <-entered; cancel() }()
			}
			err := app.Fill(ctx, newClient(raw, server.URL+"/"), config.Playlist{ID: "p", Artists: []config.Artist{{ID: "a"}}})
			assertDiagnostic(t, err, "artist_albums", mode, 0, true)
			if mode == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation identity")
			}
		})
	}
}
func TestSafeMutationDiagnosticsNoRetry(t *testing.T) {
	for _, method := range []string{"POST", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					writes.Add(1)
					w.WriteHeader(429)
					fmt.Fprint(w, `{"error":{"status":429,"message":"SECRET_BODY"}}`)
					return
				}
				if strings.HasPrefix(r.URL.Path, "/artists/") {
					fmt.Fprint(w, `{"items":[{"id":"album"}],"next":null}`)
					return
				}
				if strings.HasPrefix(r.URL.Path, "/albums/") {
					fmt.Fprint(w, `{"items":[{"id":"new"}],"next":null}`)
					return
				}
				if method == "DELETE" {
					fmt.Fprint(w, `{"items":[{"item":{"id":"old","type":"track"}}],"next":null}`)
				} else {
					fmt.Fprint(w, `{"items":[],"next":null}`)
				}
			}))
			defer server.Close()
			err := app.Fill(context.Background(), newClient(server.Client(), server.URL+"/"), config.Playlist{ID: "p", Artists: []config.Artist{{ID: "a"}}})
			op := "add_items"
			if method == "DELETE" {
				op = "remove_items"
			}
			assertDiagnostic(t, err, op, "http", 429, false)
			if !reflect.DeepEqual(writes.Load(), int32(1)) {
				t.Fatal("mutation retried")
			}
		})
	}
}
