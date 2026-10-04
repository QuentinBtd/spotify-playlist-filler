package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigErrors(t *testing.T) {
	emptyEnv := func(string) (string, bool) { return "", false }
	if _, err := Load(filepath.Join(t.TempDir(), "missing"), emptyEnv); err == nil || !strings.Contains(err.Error(), "read config") {
		t.Fatalf("missing: %v", err)
	}
	for _, tc := range []struct{ name, data, env, want string }{
		{"yaml", "playlists: [", "", "decode config"},
		{"bool", "SPOTIFY_ID: id\nSPOTIFY_SECRET: secret\n", "not-bool", "SPF_VERBOSE"},
		{"credentials", "SPOTIFY_ID: id\n", "", "SPOTIFY_SECRET"},
		{"blank-id", "SPOTIFY_ID: '  '\nSPOTIFY_SECRET: secret\n", "", "SPOTIFY_ID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yml")
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path, func(k string) (string, bool) {
				if k == "SPF_VERBOSE" && tc.env != "" {
					return tc.env, true
				}
				return "", false
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %s; got %v", tc.want, err)
			}
		})
	}
	for _, args := range [][]string{{"a", "b"}, {"-config", "a", "b"}} {
		if _, err := ParseArgs(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted ambiguous arguments %v", args)
		}
	}
}
