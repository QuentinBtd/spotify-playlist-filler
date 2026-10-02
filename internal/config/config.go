// Package config loads the existing YAML format and command-line options.
package config

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
)

type Config struct {
	ReadConcurrency int        `yaml:"-"`
	OAuthPort       int        `yaml:"-"`
	LogLevel        string     `yaml:"log_level"`
	Deprecated      []string   `yaml:"-"`
	Verbose         bool       `yaml:"verbose"`
	SpotifyID       string     `yaml:"spotify_id"`
	SpotifySecret   string     `yaml:"spotify_secret"`
	Playlists       []Playlist `yaml:"playlists"`
}

type Playlist struct {
	Name          string   `yaml:"name"`
	ID            string   `yaml:"uri"`
	ShuffleOrder  bool     `yaml:"shuffle_order"`
	Artists       []Artist `yaml:"artists"`
	SkippedAlbums []Album  `yaml:"albums_to_skip"`
}

type Artist struct {
	Name                string  `yaml:"name"`
	ID                  string  `yaml:"uri"`
	SkippedAlbums       []Album `yaml:"albums_to_skip"`
	UseNameInsteadOfURI bool    `yaml:"use_name_instead_of_uri"`
}

type Album struct {
	Name string `yaml:"name"`
	ID   string `yaml:"uri"`
}

// ParseArgs defaults to config.yaml, falling back to an existing config.yml
// only when no path was supplied and config.yaml is absent.
func ParseArgs(args []string, output io.Writer) (string, error) {
	flags := flag.NewFlagSet("spotify-playlist-filler", flag.ContinueOnError)
	flags.SetOutput(output)
	path := flags.String("config", "config.yaml", "Config path (SPF_CONFIG override; default falls back to config.yml when absent)")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	explicit := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			explicit = true
		}
	})
	if flags.NArg() > 1 || (explicit && flags.NArg() != 0) {
		return "", fmt.Errorf("use either -config PATH or one positional config path")
	}
	if flags.NArg() == 1 {
		*path = flags.Arg(0)
	} else if !explicit {
		if value := os.Getenv("SPF_CONFIG"); value != "" {
			return value, nil
		}
		if value := os.Getenv("CONFIG"); value != "" {
			return value, nil
		}
		// Lstat distinguishes an absent default from a dangling symlink: an
		// existing but unreadable/broken YAML path must not be silently masked.
		if _, err := os.Lstat(*path); os.IsNotExist(err) {
			if _, legacyErr := os.Lstat("config.yml"); !os.IsNotExist(legacyErr) {
				*path = "config.yml"
			}
		}
	}
	return *path, nil
}

// Load applies environment overrides after decoding the file.
func Load(path string, lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{OAuthPort: 8080, ReadConcurrency: 3}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config %q: %w", path, err)
	}
	raw := struct {
		Config          `yaml:",inline"`
		OAuthPort       interface{} `yaml:"oauth_port"`
		ReadConcurrency interface{} `yaml:"read_concurrency"`
		LegacyID        string      `yaml:"SPOTIFY_ID"`
		LegacySecret    string      `yaml:"SPOTIFY_SECRET"`
	}{Config: cfg, OAuthPort: 8080, ReadConcurrency: 3}
	if err := yaml.UnmarshalStrict(data, &raw); err != nil {
		// Decoder diagnostics can contain credential values or arbitrary YAML keys.
		return cfg, fmt.Errorf("decode config: invalid YAML, unknown/duplicate fields or invalid field types")
	}
	var keys map[string]interface{}
	if err := yaml.Unmarshal(data, &keys); err != nil {
		return cfg, fmt.Errorf("decode config: expected a YAML mapping")
	}
	for _, pair := range [][2]string{{"spotify_id", "SPOTIFY_ID"}, {"spotify_secret", "SPOTIFY_SECRET"}} {
		_, canonical := keys[pair[0]]
		_, legacy := keys[pair[1]]
		if canonical && legacy {
			return cfg, fmt.Errorf("conflicting YAML keys %s and %s; keep only %s", pair[0], pair[1], pair[0])
		}
	}
	cfg = raw.Config
	concurrency, ok := raw.ReadConcurrency.(int)
	if value, exists := lookup("SPF_READ_CONCURRENCY"); exists && value != "" {
		concurrency, err = strconv.Atoi(value)
		ok = err == nil
		for _, digit := range value {
			if digit < '0' || digit > '9' {
				ok = false
			}
		}
	}
	if !ok || concurrency < 1 || concurrency > 8 {
		return cfg, fmt.Errorf("read_concurrency / SPF_READ_CONCURRENCY must be an integer from 1 to 8")
	}
	cfg.ReadConcurrency = concurrency
	for _, name := range []string{"SPOTIFY_ID", "SPOTIFY_SECRET", "verbose"} {
		if _, exists := keys[name]; exists {
			cfg.Deprecated = append(cfg.Deprecated, name+" YAML")
		}
	}
	for _, name := range []string{"SPOTIFY_ID", "SPOTIFY_SECRET", "SPF_VERBOSE", "CONFIG"} {
		if value, exists := lookup(name); exists && value != "" {
			cfg.Deprecated = append(cfg.Deprecated, name+" environment")
		}
	}
	if cfg.SpotifyID == "" {
		cfg.SpotifyID = raw.LegacyID
	}
	if cfg.SpotifySecret == "" {
		cfg.SpotifySecret = raw.LegacySecret
	}
	port, source := raw.OAuthPort, "oauth_port"
	if value, ok := lookup("SPF_OAUTH_PORT"); ok && value != "" {
		port, source = value, "SPF_OAUTH_PORT"
	}
	// Validate even explicit YAML null; only an absent key uses 8080.
	var text string
	switch value := port.(type) {
	case int:
		text = strconv.Itoa(value)
	case string:
		text = value
	}
	valid := text != ""
	for _, digit := range text {
		if digit < '0' || digit > '9' {
			valid = false
		}
	}
	selected, parseErr := strconv.Atoi(text)
	if !valid || parseErr != nil || selected < 1 || selected > 65535 {
		return cfg, fmt.Errorf("%s must be an integer from 1 to 65535", source)
	}
	cfg.OAuthPort = selected
	if value, ok := lookup("SPOTIFY_ID"); ok && value != "" {
		cfg.SpotifyID = value
	}
	if value, ok := lookup("SPOTIFY_SECRET"); ok && value != "" {
		cfg.SpotifySecret = value
	}
	if value, ok := lookup("SPF_SPOTIFY_ID"); ok {
		cfg.SpotifyID = value
	}
	if value, ok := lookup("SPF_SPOTIFY_SECRET"); ok {
		cfg.SpotifySecret = value
	}
	_, explicitLevel := keys["log_level"]
	if value, ok := lookup("SPF_LOG_LEVEL"); ok && value != "" {
		cfg.LogLevel, explicitLevel = value, true
	}
	if !explicitLevel {
		if value, ok := lookup("SPF_VERBOSE"); ok && value != "" {
			cfg.Verbose, err = strconv.ParseBool(value)
			if err != nil {
				return cfg, fmt.Errorf("SPF_VERBOSE must be a boolean")
			}
		}
		cfg.LogLevel = "info"
		if cfg.Verbose {
			cfg.LogLevel = "debug"
		}
	}
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return cfg, fmt.Errorf("log_level / SPF_LOG_LEVEL must be debug, info, warn or error")
	}
	cfg.Verbose = cfg.LogLevel == "debug"
	if strings.TrimSpace(cfg.SpotifyID) == "" {
		return cfg, fmt.Errorf("spotify_id / SPF_SPOTIFY_ID is required")
	}
	if strings.TrimSpace(cfg.SpotifySecret) == "" {
		return cfg, fmt.Errorf("spotify_secret / SPF_SPOTIFY_SECRET is required")
	}
	for i, playlist := range cfg.Playlists {
		if strings.TrimSpace(playlist.ID) == "" {
			return cfg, fmt.Errorf("playlists[%d].uri is required", i)
		}
		if len(playlist.Artists) == 0 {
			return cfg, fmt.Errorf("playlists[%d].artists must not be empty", i)
		}
		for j, artist := range playlist.Artists {
			if artist.UseNameInsteadOfURI {
				if strings.TrimSpace(artist.Name) == "" {
					return cfg, fmt.Errorf("playlists[%d].artists[%d].name is required for name search", i, j)
				}
			} else if strings.TrimSpace(artist.ID) == "" {
				return cfg, fmt.Errorf("playlists[%d].artists[%d].uri is required", i, j)
			}
			for _, album := range artist.SkippedAlbums {
				if strings.TrimSpace(album.ID) == "" {
					return cfg, fmt.Errorf("playlists[%d].artists[%d].albums_to_skip: uri is required", i, j)
				}
			}
		}
		for _, album := range playlist.SkippedAlbums {
			if strings.TrimSpace(album.ID) == "" {
				return cfg, fmt.Errorf("playlists[%d].albums_to_skip: uri is required", i)
			}
		}
	}
	return cfg, nil
}
