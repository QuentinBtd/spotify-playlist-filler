package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadConcurrencyConfig(t *testing.T) {
	for _, tc := range []struct {
		yaml, env string
		want      int
		bad       bool
	}{
		{"", "", 3, false}, {"read_concurrency: 1\n", "", 1, false}, {"read_concurrency: 8\n", "", 8, false},
		{"read_concurrency: 2\n", "4", 4, false}, {"read_concurrency: 0\n", "", 0, true}, {"read_concurrency: 9\n", "", 0, true},
		{"read_concurrency: null\n", "", 0, true}, {"read_concurrency: 2.5\n", "", 0, true}, {"read_concurrency: \"2\"\n", "", 0, true},
		{"", "PRIVATE_VALUE", 0, true}, {"", "0", 0, true}, {"", "9", 0, true}, {"", "-1", 0, true},
	} {
		t.Run(tc.yaml+tc.env, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("spotify_id: fixture\nspotify_secret: fixture\nplaylists: []\n"+tc.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path, func(k string) (string, bool) { return tc.env, k == "SPF_READ_CONCURRENCY" && tc.env != "" })
			if tc.bad {
				if err == nil {
					t.Fatal("accepted invalid read concurrency")
				}
				if strings.Contains(err.Error(), "PRIVATE_VALUE") {
					t.Fatal("config value echoed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			// Reflection lets the RED test report missing configuration behavior,
			// rather than a compilation failure for the new field.
			value := reflect.ValueOf(cfg).FieldByName("ReadConcurrency")
			if !value.IsValid() || int(value.Int()) != tc.want {
				t.Fatalf("read concurrency default/override missing: %+v", cfg)
			}
		})
	}
}
