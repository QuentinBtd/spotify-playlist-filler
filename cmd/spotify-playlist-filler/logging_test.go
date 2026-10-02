package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunLoggingLevelsAndDeprecation(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		t.Run(level, func(t *testing.T) {
			t.Setenv("SPF_LOG_LEVEL", level)
			t.Setenv("SPF_SPOTIFY_ID", "synthetic-client")
			t.Setenv("SPF_SPOTIFY_SECRET", "synthetic-secret")
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("SPOTIFY_ID: legacy-client\nSPOTIFY_SECRET: legacy-secret\nverbose: true\nplaylists: []\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var out, logs bytes.Buffer
			if err := run(context.Background(), []string{"-config", path}, &out, &logs); err != nil {
				t.Fatal(err)
			}
			if out.Len() != 0 {
				t.Fatalf("telemetry on stdout: %s", &out)
			}
			info := level == "debug" || level == "info"
			if strings.Contains(logs.String(), "level=INFO") != info {
				t.Fatalf("wrong filtering: %s", &logs)
			}
			if strings.Contains(logs.String(), "deprecated setting") != (level != "error") {
				t.Fatalf("missing/filtered warning: %s", &logs)
			}
			for _, secret := range []string{"synthetic-client", "synthetic-secret", "legacy-client", "legacy-secret"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatal("credential leaked")
				}
			}
		})
	}
}

func TestRunMalformedConfigLogsAreSanitized(t *testing.T) {
	for _, text := range []string{
		"spotify_id: id\nspotify_secret: {SENSITIVE_VALUE: 1}\n",
		"spotify_id: canonical\nSPOTIFY_ID: SENSITIVE_VALUE\nspotify_secret: secret\n",
		"spotify_id: id\nspotify_secret: secret\nSENSITIVE_VALUE: true\n",
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		var out, logs bytes.Buffer
		if err := run(context.Background(), []string{"-config", path}, &out, &logs); err == nil {
			t.Fatal("accepted malformed config")
		}
		if out.Len() != 0 || strings.Count(logs.String(), "level=ERROR") != 1 || strings.Contains(logs.String(), "SENSITIVE_VALUE") {
			t.Fatalf("unsafe config log: %s", &logs)
		}
	}
}

func TestRunConfigPathEnvLogsAreSanitized(t *testing.T) {
	t.Setenv("SPF_CONFIG", filepath.Join(t.TempDir(), "SENSITIVE_PATH_VALUE"))
	var out, logs bytes.Buffer
	if err := run(context.Background(), nil, &out, &logs); err == nil {
		t.Fatal("accepted missing file")
	}
	if out.Len() != 0 || strings.Count(logs.String(), "level=ERROR") != 1 || strings.Contains(logs.String(), "SENSITIVE_PATH_VALUE") {
		t.Fatalf("unsafe path env log: %s", &logs)
	}
}

func TestRunInvalidLevelFailsOnceBeforeLogin(t *testing.T) {
	t.Setenv("SPF_LOG_LEVEL", "synthetic-secret")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("spotify_id: id\nspotify_secret: secret\nplaylists: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, logs bytes.Buffer
	err := run(context.Background(), []string{"-config", path}, &out, &logs)
	if err == nil || out.Len() != 0 || strings.Count(logs.String(), "level=ERROR") != 1 || strings.Contains(logs.String(), "synthetic-secret") {
		t.Fatalf("unsafe failure err=%v out=%s logs=%s", err, &out, &logs)
	}
}
