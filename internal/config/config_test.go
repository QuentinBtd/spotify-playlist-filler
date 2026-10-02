package config

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAndArguments(t *testing.T) {
	p := filepath.Join(t.TempDir(), "custom.yml")
	err := os.WriteFile(p, []byte("verbose: false\nSPOTIFY_ID: file-id\nSPOTIFY_SECRET: file-secret\nplaylists:\n  - name: Test\n    uri: playlist\n    shuffle_order: true\n    artists:\n      - name: Artist\n        uri: artist\n        use_name_instead_of_uri: true\n        albums_to_skip:\n          - uri: excluded\n"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-config", p}, {p}} {
		path, err := ParseArgs(args, &bytes.Buffer{})
		if err != nil || path != p {
			t.Fatalf("path=%q err=%v", path, err)
		}
		env := map[string]string{"SPOTIFY_ID": "env-id", "SPF_VERBOSE": "true"}
		cfg, err := Load(path, func(k string) (string, bool) { v, ok := env[k]; return v, ok })
		if err != nil {
			t.Fatal(err)
		}
		if cfg.SpotifyID != "env-id" || cfg.SpotifySecret != "file-secret" || !cfg.Verbose || !cfg.Playlists[0].ShuffleOrder || !cfg.Playlists[0].Artists[0].UseNameInsteadOfURI {
			t.Fatalf("bad config: %+v", cfg)
		}
	}
	var output bytes.Buffer
	if _, err := ParseArgs([]string{"-h"}, &output); !errors.Is(err, flag.ErrHelp) || !strings.Contains(output.String(), "-config") {
		t.Fatalf("help: %v %s", err, output.String())
	}
}
