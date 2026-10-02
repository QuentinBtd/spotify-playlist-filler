package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnsafePlaylistConfigRejected(t *testing.T) {
	for _, data := range []string{
		"playlists:\n  - artists: [{uri: artist}]\n",
		"playlists:\n  - uri: playlist\n",
		"playlists:\n  - uri: playlist\n    artists: [{name: artist}]\n",
		"playlists:\n  - uri: playlist\n    artists: [{use_name_instead_of_uri: true}]\n",
		"PLAYLISTS_TO_FILL: [{uri: playlist}]\n",
		"playlists:\n  - uri: playlist\n    artists: [{uri: artist}]\n    albums_to_skip: [{name: MissingID}]\n",
	} {
		path := filepath.Join(t.TempDir(), "config.yml")
		if err := os.WriteFile(path, []byte("SPOTIFY_ID: id\nSPOTIFY_SECRET: secret\n"+data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path, func(string) (string, bool) { return "", false }); err == nil {
			t.Fatalf("unsafe config accepted: %s", data)
		}
	}
}

func TestExampleConfigWithEnvironmentCredentials(t *testing.T) {
	cfg, err := Load("../../config.example.yaml", func(key string) (string, bool) {
		if key == "SPOTIFY_ID" || key == "SPOTIFY_SECRET" {
			return "test-credential", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Playlists) != 1 || len(cfg.Playlists[0].Artists) != 2 || cfg.Playlists[0].ShuffleOrder {
		t.Fatalf("unexpected example: %+v", cfg.Playlists)
	}
}
