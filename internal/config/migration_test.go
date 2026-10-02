package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizedStrictMigrationErrors(t *testing.T) {
	for _, tc := range []struct{ data, want string }{
		{"spotify_id: canonical\nSPOTIFY_ID: SENSITIVE_VALUE\nspotify_secret: secret\n", "conflicting"},
		{"spotify_id: id\nspotify_secret: canonical\nSPOTIFY_SECRET: SENSITIVE_VALUE\n", "conflicting"},
		{"spotify_id: null\nSPOTIFY_ID: SENSITIVE_VALUE\nspotify_secret: secret\n", "conflicting"},
		{"spotify_id: id\nspotify_secret: {SENSITIVE_VALUE: 1}\n", "decode config"},
		{"spotify_id: id\nspotify_secret: secret\nSENSITIVE_VALUE: true\n", "decode config"},
		{"spotify_id: id\nspotify_id: SENSITIVE_VALUE\nspotify_secret: secret\n", "decode config"},
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(path, func(string) (string, bool) { return "", false })
		if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "SENSITIVE_VALUE") {
			t.Fatalf("unsafe or unclear error: %v", err)
		}
	}
}

func TestLogLevelValidationAndVerboseCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, yaml     string
		env            map[string]string
		debug, invalid bool
	}{
		{name: "default"},
		{name: "legacy yaml", yaml: "verbose: true\n", debug: true},
		{name: "legacy env", env: map[string]string{"SPF_VERBOSE": "true"}, debug: true},
		{name: "explicit yaml wins", yaml: "log_level: warn\nverbose: true\n", env: map[string]string{"SPF_VERBOSE": "SENSITIVE_VALUE"}},
		{name: "explicit env wins", yaml: "log_level: error\n", env: map[string]string{"SPF_LOG_LEVEL": "debug"}, debug: true},
		{name: "invalid env", env: map[string]string{"SPF_LOG_LEVEL": "SENSITIVE_VALUE"}, invalid: true},
		{name: "invalid bool", env: map[string]string{"SPF_VERBOSE": "SENSITIVE_VALUE"}, invalid: true},
		{name: "invalid yaml", yaml: "log_level: SENSITIVE_VALUE\n", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("spotify_id: id\nspotify_secret: secret\n"+tc.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path, func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok })
			if tc.invalid {
				if err == nil || strings.Contains(err.Error(), "SENSITIVE_VALUE") {
					t.Fatalf("unsafe validation: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Verbose != tc.debug {
				t.Fatal("wrong effective debug level")
			}
		})
	}
}

func TestConfigEnvironmentPathPriority(t *testing.T) {
	t.Setenv("CONFIG", "legacy config.yml")
	t.Setenv("SPF_CONFIG", "canonical config.yaml")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "canonical config.yaml"},
		{[]string{"-config", "explicit.yaml"}, "explicit.yaml"},
		{[]string{"positional.yml"}, "positional.yml"},
	} {
		got, err := ParseArgs(tc.args, &strings.Builder{})
		if err != nil || got != tc.want {
			t.Fatalf("path=%q err=%v", got, err)
		}
	}
	t.Setenv("SPF_CONFIG", "")
	got, err := ParseArgs(nil, &strings.Builder{})
	if err != nil || got != "legacy config.yml" {
		t.Fatalf("legacy path=%q err=%v", got, err)
	}
}

func TestCanonicalCredentialsOverrideLegacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("spotify_id: file-id\nspotify_secret: file-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"SPF_SPOTIFY_ID": "canonical-id", "SPF_SPOTIFY_SECRET": "canonical-secret", "SPOTIFY_ID": "old-id", "SPOTIFY_SECRET": "old-secret"}
	cfg, err := Load(path, func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SpotifyID != "canonical-id" || cfg.SpotifySecret != "canonical-secret" {
		t.Fatal("canonical credentials did not win")
	}
}
