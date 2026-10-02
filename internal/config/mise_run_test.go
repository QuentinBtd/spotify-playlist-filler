package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMiseRunConfigSemantics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mise tasks require Bash; use WSL on Windows")
	}
	data, err := os.ReadFile("../../.mise.toml")
	if err != nil {
		t.Fatal(err)
	}
	section := strings.SplitN(string(data), "[tasks.run]", 2)[1]
	script := strings.SplitN(section, "'''", 3)[1]
	dir := t.TempDir()
	goStub := filepath.Join(dir, "go")
	if err := os.WriteFile(goStub, []byte("#!/usr/bin/env bash\nprintf '%s\\n' \"$@\" > \"$CAPTURE\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, config string
		extra, want  []string
	}{
		{"default", "", nil, []string{"run", "./cmd/spotify-playlist-filler"}},
		{"custom config", "path with spaces/custom.yml", []string{"--help"}, []string{"run", "./cmd/spotify-playlist-filler", "-config", "path with spaces/custom.yml", "--help"}},
		{"positional", "", []string{"legacy.yml"}, []string{"run", "./cmd/spotify-playlist-filler", "legacy.yml"}},
		{"explicit flag", "", []string{"-config", "explicit.yaml"}, []string{"run", "./cmd/spotify-playlist-filler", "-config", "explicit.yaml"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capture := filepath.Join(dir, "args")
			args := append([]string{"-c", script, "task"}, tc.extra...)
			cmd := exec.Command("bash", args...)
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "CONFIG="+tc.config, "CAPTURE="+capture)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("run script: %v %s", err, out)
			}
			data, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != strings.Join(tc.want, "\n")+"\n" {
				t.Fatalf("args=%q want=%q", data, tc.want)
			}
		})
	}
}
