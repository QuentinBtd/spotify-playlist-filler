package config

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestDefaultConfigYAMLAndLegacyFallback(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		legacy     bool
		want       string
		loadError  bool
	}{
		{"missing", "", false, "config.yaml", true},
		{"legacy", "", true, "config.yml", false},
		{"yaml wins", "SPOTIFY_ID: fixture\nSPOTIFY_SECRET: fixture\n", true, "config.yaml", false},
		{"malformed yaml not masked", "[invalid", true, "config.yaml", true},
		{"unreadable yaml directory not masked", "directory", true, "config.yaml", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if tc.legacy {
				if err := os.WriteFile("config.yml", []byte("SPOTIFY_ID: fixture\nSPOTIFY_SECRET: fixture\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.yaml == "directory" {
				if err := os.Mkdir("config.yaml", 0700); err != nil {
					t.Fatal(err)
				}
			} else if tc.yaml != "" {
				if err := os.WriteFile("config.yaml", []byte(tc.yaml), 0600); err != nil {
					t.Fatal(err)
				}
			}
			path, err := ParseArgs(nil, io.Discard)
			if err != nil || path != tc.want {
				t.Fatalf("path=%q err=%v want=%q", path, err, tc.want)
			}
			_, err = Load(path, func(string) (string, bool) { return "", false })
			if (err != nil) != tc.loadError {
				t.Fatalf("load err=%v", err)
			}
			for _, args := range [][]string{{"-config", "custom.yml"}, {"custom.yml"}, {"-config", "config.yaml"}} {
				explicit, err := ParseArgs(args, io.Discard)
				if err != nil || explicit != args[len(args)-1] {
					t.Fatalf("explicit path=%q err=%v", explicit, err)
				}
				if explicit == "custom.yml" {
					_, err = Load(explicit, func(string) (string, bool) { return "", false })
					if err == nil || !strings.Contains(err.Error(), "custom.yml") {
						t.Fatalf("custom error masked: %v", err)
					}
				}
			}
		})
	}
}
