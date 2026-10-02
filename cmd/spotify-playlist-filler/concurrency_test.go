package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvalidReadConcurrencyStopsBeforeLogin(t *testing.T) {
	t.Setenv("SPF_READ_CONCURRENCY", "PRIVATE_VALUE")
	t.Setenv("SPF_SPOTIFY_ID", "fixture")
	t.Setenv("SPF_SPOTIFY_SECRET", "fixture")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("playlists: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, logs bytes.Buffer
	err := run(context.Background(), []string{"-config", path}, &out, &logs)
	if err == nil || out.Len() != 0 || strings.Contains(logs.String(), "login started") || strings.Contains(logs.String(), "PRIVATE_VALUE") {
		t.Fatalf("configuration did not fail safely before auth: err=%v out=%s logs=%s", err, &out, &logs)
	}
}
