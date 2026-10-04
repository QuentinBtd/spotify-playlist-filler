package spotifyapi

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/diagnostics"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/logging"
)

func TestCatalogueCorruptEntryWarningIsSanitized(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"items":[],"next":null,"offset":0,"limit":%d,"total":0}`, fixtureCatalogueLimit(r))
	}))
	defer s.Close()
	dir := t.TempDir() + "/cache"
	c := newClient(s.Client(), s.URL+"/")
	if e := c.EnableCatalogueCache(dir, time.Hour, "app", "user"); e != nil {
		t.Fatal(e)
	}
	if _, e := c.AlbumTracks(context.Background(), "a"); e != nil {
		t.Fatal(e)
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, entries[0].Name()), []byte("PRIVATE_CORRUPTION"), 0600); e != nil {
		t.Fatal(e)
	}
	var logs bytes.Buffer
	ctx := logging.WithContext(context.Background(), slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if _, e := c.AlbumTracks(ctx, "a"); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "status=invalid") || strings.Contains(logs.String(), "PRIVATE") || strings.Contains(logs.String(), dir) {
		t.Fatalf("invalid-cache diagnostic: %s", &logs)
	}
}
func TestCatalogueQuotaWithFullStoragePreservesDiagnostic(t *testing.T) {
	dir := t.TempDir() + "/cache"
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(filepath.Join(dir, "filler.json"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if e := f.Truncate(64 << 20); e != nil {
		t.Fatal(e)
	}
	f.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "80982")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"status":429,"reason":"QUOTA_EXCEEDED","message":"PRIVATE_BODY"}}`)
	}))
	defer s.Close()
	c := newClient(s.Client(), s.URL+"/")
	if e := c.EnableCatalogueCache(dir, time.Hour, "app", "user"); e != nil {
		t.Fatal(e)
	}
	_, e = c.AlbumTracks(context.Background(), "a")
	if fields := fmt.Sprint(diagnostics.Fields(e)); !strings.Contains(fields, "http_status 429") || !strings.Contains(fields, "reason QUOTA_EXCEEDED") {
		t.Fatalf("lost 429 diagnostic: %s", fields)
	}
}
