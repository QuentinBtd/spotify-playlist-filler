package spotifyauth

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheAcceptedPathSaveLoadRemove(t *testing.T) {
	for _, kind := range []string{"absolute", "relative", "dot", "repeated-separator", "dotdot-in-name"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			t.Chdir(base)
			dir := filepath.Join(base, "tokens")
			override := dir
			switch kind {
			case "relative":
				override = "tokens"
			case "dot":
				override = "." + string(os.PathSeparator) + "tokens"
			case "repeated-separator":
				override = base + string(os.PathSeparator) + string(os.PathSeparator) + "tokens"
			case "dotdot-in-name":
				dir = filepath.Join(base, "tokens..backup")
				override = "tokens..backup"
			}
			t.Setenv("SPF_TOKEN_CACHE", override)
			cache, err := newTokenCache("test-id")
			if err != nil {
				t.Fatal(err)
			}
			token := validToken()
			if err := cache.save(token); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, cache.name)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("save did not use expected directory: %v", err)
			}
			// A fresh cache models reuse on the next invocation.
			reopened, err := newTokenCache("test-id")
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := reopened.load()
			if err != nil || loaded == nil || loaded.AccessToken != token.AccessToken || loaded.RefreshToken != token.RefreshToken || !loaded.Expiry.Equal(token.Expiry) {
				t.Fatalf("saved token not reloaded: token=%v error=%v", loaded != nil, err)
			}
			if err := reopened.remove(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("remove did not use saved directory: %v", err)
			}
			if loaded, err := reopened.load(); err != nil || loaded != nil {
				t.Fatalf("removed token still loaded: token=%v error=%v", loaded != nil, err)
			}
		})
	}
}

func TestCacheRejectsDotDotForEveryOperation(t *testing.T) {
	for _, kind := range []string{"absolute", "relative", "terminal", "missing-component"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			t.Chdir(base)
			dir, path := cacheFixture(t, validToken())
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			child := filepath.Join(dir, "child")
			if err := os.Mkdir(child, 0700); err != nil {
				t.Fatal(err)
			}
			sep := string(os.PathSeparator)
			override := dir + sep + "child" + sep + ".."
			switch kind {
			case "absolute":
				override += sep + "."
			case "relative":
				relative, err := filepath.Rel(base, dir)
				if err != nil {
					t.Fatal(err)
				}
				override = relative + sep + "child" + sep + ".."
			case "missing-component":
				override = dir + sep + "missing" + sep + ".."
			}
			t.Setenv("SPF_TOKEN_CACHE", override)
			cache, err := newTokenCache("test-id")
			if err != nil {
				t.Fatal(err)
			}
			if token, err := cache.load(); err == nil || token != nil {
				t.Errorf("load accepted dotdot: token=%v error=%v", token != nil, err)
			}
			if err := cache.save(validToken()); err == nil {
				t.Error("save accepted dotdot")
			}
			if err := cache.remove(); err == nil {
				t.Error("remove accepted dotdot")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("rejected operations changed existing cache: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
				t.Fatalf("rejected operations created missing component: %v", err)
			}
		})
	}
}
