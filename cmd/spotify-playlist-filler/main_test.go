package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunHelpWithoutConfigOrLogin(t *testing.T) {
	t.Setenv("SPF_CONFIG", t.TempDir()+"/missing")
	t.Setenv("CONFIG", "legacy-missing")
	t.Setenv("SPF_LOG_LEVEL", "invalid")
	var out, stderr bytes.Buffer
	if err := run(context.Background(), []string{"--help"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "-config") || out.Len() != 0 {
		t.Fatalf("out=%s stderr=%s", &out, &stderr)
	}
}
func TestRunMissingConfigFailsBeforeLogin(t *testing.T) {
	var out, stderr bytes.Buffer
	err := run(context.Background(), []string{"-config", t.TempDir() + "/missing.yml"}, &out, &stderr)
	if err == nil || !strings.Contains(err.Error(), "read config") || out.Len() != 0 {
		t.Fatalf("err=%v out=%s", err, &out)
	}
}
