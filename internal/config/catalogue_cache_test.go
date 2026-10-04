package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCatalogueCacheSettings(t *testing.T) {
	for _, tc := range []struct {
		yaml  string
		env   map[string]string
		valid bool
		ttl   time.Duration
		dir   string
	}{
		{"", nil, true, 24 * time.Hour, ""},
		{"cache_ttl: 0s\ncache_directory: /synthetic/cache\n", nil, true, 0, "/synthetic/cache"},
		{"cache_ttl: 2h\n", map[string]string{"SPF_CACHE_TTL": "1h", "SPF_CACHE_DIRECTORY": "/fixture"}, true, time.Hour, "/fixture"},
		{"cache_ttl: -1h\n", nil, false, 0, ""},
		{"cache_ttl: 721h\n", nil, false, 0, ""},
		{"cache_ttl: 24\n", nil, false, 0, ""},
		{"cache_ttl: null\n", nil, false, 0, ""},
		{"", map[string]string{"SPF_CACHE_TTL": ""}, false, 0, ""},
		{"cache_directory: ''\n", nil, false, 0, ""},
	} {
		t.Run(tc.yaml, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "fixture.yaml")
			if e := os.WriteFile(p, []byte("spotify_id: synthetic\nspotify_secret: synthetic\n"+tc.yaml), 0600); e != nil {
				t.Fatal(e)
			}
			cfg, e := Load(p, func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok })
			if (e == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, e)
			}
			if tc.valid && (cfg.CacheTTL != tc.ttl || cfg.CacheDirectory != tc.dir) {
				t.Fatalf("settings=%+v", cfg)
			}
		})
	}
}
