package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOAuthPortDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("SPOTIFY_ID: fixture\nSPOTIFY_SECRET: fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OAuthPort != 8080 {
		t.Fatalf("default OAuthPort=%d, want 8080", cfg.OAuthPort)
	}
}

func TestOAuthPortSelection(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, env string
		want            int
		source          string
	}{
		{"yaml", "oauth_port: 9001\n", "", 9001, ""},
		{"env", "", "9002", 9002, ""},
		{"precedence", "oauth_port: 9001\n", "9002", 9002, ""},
		{"override invalid yaml", "oauth_port: 0\n", "9002", 9002, ""},
		{"minimum", "oauth_port: 1\n", "", 1, ""},
		{"maximum", "oauth_port: 65535\n", "", 65535, ""},
		{"yaml null", "oauth_port: null\n", "", 0, "oauth_port"},
		{"yaml empty", "oauth_port: ''\n", "", 0, "oauth_port"},
		{"yaml zero", "oauth_port: 0\n", "", 0, "oauth_port"},
		{"yaml negative", "oauth_port: -1\n", "", 0, "oauth_port"},
		{"yaml overflow", "oauth_port: 65536\n", "", 0, "oauth_port"},
		{"yaml malformed", "oauth_port: private-port-value\n", "", 0, "oauth_port"},
		{"yaml float", "oauth_port: 9001.5\n", "", 0, "oauth_port"},
		{"yaml bool", "oauth_port: true\n", "", 0, "oauth_port"},
		{"yaml list", "oauth_port: [private-port-value]\n", "", 0, "oauth_port"},
		{"env malformed", "", "private-port-value", 0, "SPF_OAUTH_PORT"},
		{"env zero", "", "0", 0, "SPF_OAUTH_PORT"},
		{"env negative", "", "-1", 0, "SPF_OAUTH_PORT"},
		{"env overflow", "", "999999999999999999999999", 0, "SPF_OAUTH_PORT"},
		{"env above range", "", "65536", 0, "SPF_OAUTH_PORT"},
		{"env whitespace", "", " 9001 ", 0, "SPF_OAUTH_PORT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("SPOTIFY_ID: fixture\nSPOTIFY_SECRET: fixture\n"+tc.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path, func(key string) (string, bool) { return tc.env, key == "SPF_OAUTH_PORT" })
			if tc.source != "" {
				if err == nil || !strings.Contains(err.Error(), tc.source+" must be an integer from 1 to 65535") || strings.Contains(err.Error(), "private-port-value") {
					t.Fatalf("unsanitized/nonactionable validation: %v", err)
				}
			} else if err != nil || cfg.OAuthPort != tc.want {
				t.Fatalf("port=%d err=%v", cfg.OAuthPort, err)
			}
		})
	}
}
