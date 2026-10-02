package spotifyauth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	sdk "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2"
)

var scopes = []string{sdk.ScopeUserReadPrivate, sdk.ScopePlaylistReadPrivate, sdk.ScopePlaylistReadCollaborative, sdk.ScopePlaylistModifyPublic, sdk.ScopePlaylistModifyPrivate}

func scopeKey() string {
	s := append([]string(nil), scopes...)
	sort.Strings(s)
	return strings.Join(s, " ")
}

type tokenRecord struct {
	Version  int           `json:"version"`
	ClientID string        `json:"client_id"`
	Scopes   string        `json:"scopes"`
	Token    *oauth2.Token `json:"token"`
}
type tokenCache struct{ dir, name, id string }

func newTokenCache(id string) (*tokenCache, error) {
	dir := os.Getenv("SPF_TOKEN_CACHE")
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, errors.New("resolve token cache directory")
		}
		dir = filepath.Join(base, "spotify-playlist-filler")
	}
	sum := sha256.Sum256([]byte(id + "\n" + scopeKey()))
	return &tokenCache{dir: dir, name: hex.EncodeToString(sum[:]) + ".spf-token.json", id: id}, nil
}
func (c *tokenCache) validate() error {
	// Cleaning .. can erase a symlink component and validate a different
	// directory from the one used by OpenRoot. Reject before cleaning.
	for _, component := range strings.Split(filepath.ToSlash(c.dir), "/") {
		if component == ".." {
			return errors.New("unsafe token cache path")
		}
	}
	absolute, err := filepath.Abs(c.dir)
	if err != nil {
		return errors.New("unsafe token cache path")
	}
	for path := absolute; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return errors.New("inspect token cache directory")
		}
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("unsafe token cache directory")
			}
			if runtime.GOOS != "windows" && ((path == absolute && info.Mode().Perm() != 0700) || (path != absolute && info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0)) {
				return errors.New("insecure token cache directory permissions")
			}
		}
		if filepath.Dir(path) == path {
			break
		}
	}
	info, err := os.Lstat(filepath.Join(absolute, c.name))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return errors.New("inspect token cache file")
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("unsafe token cache file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		return errors.New("insecure token cache file permissions")
	}
	return nil
}
func (c *tokenCache) remove() error {
	if err := c.validate(); err != nil {
		return err
	}
	root, err := os.OpenRoot(c.dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return errors.New("open token cache directory")
	}
	defer root.Close()
	if err := root.Remove(c.name); err != nil && !os.IsNotExist(err) {
		return errors.New("remove invalid token cache")
	}
	return nil
}
func validTokenFields(token *oauth2.Token) bool {
	return token != nil && token.AccessToken != "" && token.RefreshToken != "" && !token.Expiry.IsZero() && strings.EqualFold(token.TokenType, "Bearer")
}
func (c *tokenCache) save(token *oauth2.Token) error {
	if !validTokenFields(token) {
		return errors.New("invalid OAuth token fields")
	}

	if err := c.validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(c.dir, 0700); err != nil {
		return errors.New("create token cache directory")
	}
	if err := c.validate(); err != nil {
		return err
	}
	data, err := json.Marshal(tokenRecord{Version: 1, ClientID: c.id, Scopes: scopeKey(), Token: token})
	if err != nil {
		return errors.New("encode token cache")
	}
	root, err := os.OpenRoot(c.dir)
	if err != nil {
		return errors.New("open token cache directory")
	}
	defer root.Close()
	name, err := randomState()
	if err != nil {
		return errors.New("create token cache temporary name")
	}
	name = ".spf-token-" + name
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("create token cache temporary file")
	}
	defer root.Remove(name)
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("write token cache")
	}
	if err := c.validate(); err != nil {
		return err
	}
	if err := root.Rename(name, c.name); err != nil {
		return errors.New("replace token cache")
	}
	return nil
}
func (c *tokenCache) load() (*oauth2.Token, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(c.dir, c.name))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("read token cache")
	}
	var record tokenRecord
	if json.Unmarshal(data, &record) != nil || record.Version != 1 || record.ClientID != c.id || record.Scopes != scopeKey() || record.Token == nil {
		return nil, errors.New("invalid token cache metadata")
	}
	if !validTokenFields(record.Token) {
		return nil, errors.New("invalid token cache fields")
	}
	return record.Token, nil
}
