package spotifyauth

import (
	"os"
	"path/filepath"
	"testing"
)

// Local filesystem-only regression probe; no HTTP or real credentials.
func TestIndependentReviewRejectsSymlinkDotDotCachePath(t *testing.T) {
	base := t.TempDir()
	lexical := filepath.Join(base, "lexical")
	physical := filepath.Join(base, "physical")
	for _, dir := range []string{lexical, physical, filepath.Join(physical, "child"), filepath.Join(lexical, "tokens")} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	actual := filepath.Join(physical, "tokens")
	if err := os.Mkdir(actual, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(actual, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(physical, "child"), filepath.Join(lexical, "link")); err != nil {
		t.Fatal(err)
	}
	// Do not use filepath.Join for this: retaining .. is the input being tested.
	t.Setenv("SPF_TOKEN_CACHE", lexical+"/link/../tokens")
	cache, err := newTokenCache("test-id")
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.save(validToken()); err != nil {
		t.Logf("Unsafe path correctly rejected: %v", err)
		return
	}
	_, physicalErr := os.Stat(filepath.Join(actual, cache.name))
	_, lexicalErr := os.Stat(filepath.Join(lexical, "tokens", cache.name))
	loaded, loadErr := cache.load()
	info, err := os.Stat(actual)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("unsafe path accepted: actual directory mode=%04o; physical file exists=%v; checked lexical file missing=%v; reload token present=%v, error=%v", info.Mode().Perm(), physicalErr == nil, os.IsNotExist(lexicalErr), loaded != nil, loadErr)
}
