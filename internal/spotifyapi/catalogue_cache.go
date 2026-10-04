package spotifyapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/logging"
)

const maxCatalogueEntry = 128 << 10

var catalogueDiskMu sync.Mutex
var errCacheUnavailable = errors.New("catalogue cache storage unavailable")
var errInvalidCache = errors.New("invalid catalogue cache entry")

type catalogueRecord struct {
	Version    int             `json:"version"`
	Key        string          `json:"key"`
	Saved      time.Time       `json:"saved"`
	Body       json.RawMessage `json:"body"`
	Generation string          `json:"generation,omitempty"`
	Parent     string          `json:"parent,omitempty"`
}
type catalogueStore struct {
	dir, namespace string
	ttl            time.Duration
	mu             sync.Mutex
}
type catalogueContextKey struct{}
type catalogueTraversal struct {
	path         string
	visited      map[string]bool
	parent       string
	prepared     bool
	snapshot     map[string][]byte
	started      bool
	count, total int
	expected     string
}
type catalogueTransport struct {
	next  http.RoundTripper
	store *catalogueStore
}

// EnableCatalogueCache must be called before concurrent use. User and application
// identity are required; the token is deliberately not part of the namespace.
func (c *Client) EnableCatalogueCache(dir string, ttl time.Duration, application, user string, market ...string) error {

	if ttl < 0 || ttl > 30*24*time.Hour || application == "" || user == "" {
		return errors.New("invalid catalogue cache settings")
	}
	if ttl != 0 && dir == "" {
		base, e := os.UserCacheDir()
		if e != nil {
			return errors.New("resolve catalogue cache directory")
		}
		dir = filepath.Join(base, "spotify-playlist-filler", "catalogue")
	}
	s := &catalogueStore{dir: dir, ttl: ttl, namespace: application + "\x00" + user + "\x00" + c.baseURL + "\x00sdk-default-market-v1"}
	if len(market) > 0 {
		s.namespace += "\x00country=" + strings.Join(market, "\x00")
	}
	if ttl != 0 {
		if e := s.validate(""); e != nil {
			return e
		}
	}
	guard := c.http.Transport.(apiTransport)
	guard.next = &catalogueTransport{next: guard.next.(*catalogueTransport).next, store: s}
	c.http.Transport = guard
	return nil
}
func catalogueContext(ctx context.Context, endpoint string) context.Context {
	u, _ := url.Parse(endpoint)
	return context.WithValue(ctx, catalogueContextKey{}, &catalogueTraversal{path: u.EscapedPath(), visited: make(map[string]bool)})
}
func (s *catalogueStore) key(r *http.Request) string {
	return s.digest(r.URL.EscapedPath() + "?" + r.URL.Query().Encode())
}

func (s *catalogueStore) digest(value string) string {
	sum := sha256.Sum256([]byte(s.namespace + "\x00" + value))
	return hex.EncodeToString(sum[:])
}

// Reject unclean paths before filepath.Abs can erase components. Recheck before
// each operation, and use os.Root to confine file operations after validation.
func (s *catalogueStore) validate(name string) error {
	for _, part := range strings.Split(filepath.ToSlash(s.dir), "/") {
		if part == ".." {
			return errors.New("unsafe catalogue cache path")
		}
	}
	abs, e := filepath.Abs(s.dir)
	if e != nil {
		return errors.New("unsafe catalogue cache path")
	}
	for p := abs; ; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e != nil && !os.IsNotExist(e) {
			return errors.New("inspect catalogue cache directory")
		}
		if e == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("unsafe catalogue cache directory")
			}
			if runtime.GOOS != "windows" && ((p == abs && info.Mode().Perm() != 0700) || (p != abs && info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0)) {
				return errors.New("insecure catalogue cache directory")
			}
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	if name == "" {
		return nil
	}
	info, e := os.Lstat(filepath.Join(abs, name))
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return errors.New("inspect catalogue cache file")
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		return errors.New("unsafe catalogue cache file")
	}
	return nil
}
func (s *catalogueStore) load(key string) ([]byte, error) {
	rec, err := s.loadRecord(key)
	return rec.Body, err
}
func (s *catalogueStore) loadRecord(key string) (catalogueRecord, error) {
	if s.ttl == 0 {
		return catalogueRecord{}, nil
	}
	name := key + ".spf-catalogue.json"
	if e := s.validate(name); e != nil {
		return catalogueRecord{}, e
	}
	root, e := os.OpenRoot(s.dir)
	if os.IsNotExist(e) {
		return catalogueRecord{}, nil
	}
	if e != nil {
		return catalogueRecord{}, errors.New("open catalogue cache")
	}
	defer root.Close()
	f, e := root.Open(name)
	if os.IsNotExist(e) {
		return catalogueRecord{}, nil
	}
	if e != nil {
		return catalogueRecord{}, errors.New("read catalogue cache")
	}
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, maxCatalogueEntry+1))
	if e != nil {
		return catalogueRecord{}, errors.New("read catalogue cache")
	}
	if len(data) > maxCatalogueEntry {
		return catalogueRecord{}, errInvalidCache
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var rec catalogueRecord
	if d.Decode(&rec) != nil || rec.Version != 1 || rec.Key != key || rec.Saved.IsZero() || rec.Saved.After(time.Now()) {
		return catalogueRecord{}, errInvalidCache
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return catalogueRecord{}, errInvalidCache
	}
	if time.Since(rec.Saved) >= s.ttl {
		return catalogueRecord{}, nil
	}
	return rec, nil
}
func (s *catalogueStore) save(key string, body []byte) error {
	return s.saveRecord(catalogueRecord{Version: 1, Key: key, Saved: time.Now(), Body: body})
}
func (s *catalogueStore) saveRecord(rec catalogueRecord) error {
	key := rec.Key
	name := key + ".spf-catalogue.json"
	if e := s.validate(name); e != nil {
		return e
	}
	if e := os.MkdirAll(s.dir, 0700); e != nil {
		return errors.New("create catalogue cache")
	}
	if e := s.validate(name); e != nil {
		return e
	}
	data, e := json.Marshal(rec)
	if e != nil || len(data) > maxCatalogueEntry {
		return errors.New("catalogue cache entry too large")
	}
	root, e := os.OpenRoot(s.dir)
	if e != nil {
		return errors.New("open catalogue cache")
	}
	defer root.Close()
	// Serialize disk accounting across instances in this process. Atomic separate
	// entries never replace a shared index or lose other callers' checkpoints.
	catalogueDiskMu.Lock()
	defer catalogueDiskMu.Unlock()
	if e := root.Mkdir(".spf-catalogue-lock", 0700); e != nil {
		return errCacheUnavailable
	}
	defer root.Remove(".spf-catalogue-lock")
	listing, e := root.Open(".")
	if e != nil {
		return errors.New("inspect catalogue cache size")
	}
	entries, e := listing.ReadDir(-1)
	listing.Close()
	if e != nil {
		return errors.New("inspect catalogue cache size")
	}
	var total int64
	count := 0
	for _, entry := range entries {
		if entry.Name() == name || entry.Name() == ".spf-catalogue-lock" {
			continue
		}
		info, e := entry.Info()
		if e != nil {
			return errors.New("inspect catalogue cache size")
		}
		if info.Mode().IsRegular() {
			total += info.Size()
			count++
		}
	}
	if count >= 4096 || total+int64(len(data)) > 64<<20 {
		return errCacheUnavailable
	}
	var random [16]byte
	if _, e := rand.Read(random[:]); e != nil {
		return errors.New("create catalogue cache temporary name")
	}
	temp := ".spf-catalogue-" + hex.EncodeToString(random[:])
	f, e := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return errors.New("create catalogue cache entry")
	}
	defer root.Remove(temp)
	_, we := f.Write(data)
	se := f.Sync()
	ce := f.Close()
	if we != nil || se != nil || ce != nil {
		return errors.New("write catalogue cache entry")
	}
	if e := s.validate(name); e != nil {
		return e
	}
	if root.Rename(temp, name) != nil {
		return errors.New("replace catalogue cache entry")
	}
	return nil
}
func cachedResponse(r *http.Request, body []byte) *http.Response {
	return &http.Response{StatusCode: 200, Status: "200 OK", Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: r}
}

// snapshot validates and pins a complete entity before any cached page is
// exposed. A miss anywhere selects live reads for the entire traversal. Unique
// predecessor generations reject partial rewrites and concurrent writer mixes;
// pinned bodies cannot change underneath the consuming SDK traversal.
func (s *catalogueStore) snapshot(r *http.Request) (map[string][]byte, error) {
	if s.ttl == 0 {
		return nil, nil
	}
	pages := make(map[string][]byte)
	chain := &catalogueTraversal{}
	current := r.Clone(r.Context())
	parent, size := "", 0
	for len(pages) < 4096 {
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		key := s.key(current)
		if _, exists := pages[key]; exists {
			return nil, nil
		}
		rec, err := s.loadRecord(key)
		if errors.Is(err, errInvalidCache) {
			logging.FromContext(r.Context()).Warn("catalogue cache", "status", "invalid")
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if rec.Body == nil || rec.Generation == "" || rec.Parent != parent {
			return nil, nil
		}
		size += len(rec.Body)
		if size > 64<<20 {
			return nil, nil
		}
		body, err := cleanCataloguePage(current, rec.Body)
		if err != nil || validateCatalogueChain(current, body, chain) != nil {
			logging.FromContext(r.Context()).Warn("catalogue cache", "status", "invalid")
			return nil, nil
		}
		pages[key] = body
		if chain.expected == "" {
			return pages, nil
		}
		target, err := url.Parse(chain.expected)
		if err != nil || validateDestination(target, r.URL, r.URL.EscapedPath()) != nil {
			return nil, errors.New("unsafe catalogue destination")
		}
		current.URL = target
		parent = rec.Generation
	}
	return nil, nil
}

func (t *catalogueTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	traversal, _ := r.Context().Value(catalogueContextKey{}).(*catalogueTraversal)
	if traversal == nil || r.Method != http.MethodGet {
		return t.next.RoundTrip(r)
	}
	if e := r.Context().Err(); e != nil {
		return nil, e
	}
	if r.URL.EscapedPath() != traversal.path {
		return nil, errors.New("unsafe catalogue destination")
	}
	key := t.store.key(r)
	if traversal.visited[key] {
		return nil, errors.New("catalogue pagination cycle")
	}
	traversal.visited[key] = true
	if !traversal.prepared {
		snapshot, err := t.store.snapshot(r)
		if err != nil {
			return nil, err
		}
		traversal.snapshot, traversal.prepared = snapshot, true
	}
	if traversal.snapshot != nil {
		body, ok := traversal.snapshot[key]
		if !ok {
			return nil, errors.New("catalogue request outside cached snapshot")
		}
		if err := validateCatalogueChain(r, body, traversal); err != nil {
			return nil, err
		}
		logging.FromContext(r.Context()).Debug("catalogue cache", "status", "hit")
		return cachedResponse(r, body), nil
	}
	logging.FromContext(r.Context()).Debug("catalogue cache", "status", "miss")
	cooldown, e := t.store.cooldown()
	if e != nil {
		return nil, e
	}
	if time.Now().Before(cooldown.At) {
		return nil, cooldown.err()
	}
	resp, e := t.next.RoundTrip(r)
	if e != nil {
		return resp, e
	}
	if resp.StatusCode == 429 && t.store.ttl != 0 {
		meta := quotaDetails(resp)
		if meta.Seconds > int64(readWaitBudget/time.Second) && meta.Seconds <= int64(30*24*time.Hour/time.Second) {
			// Within-process merge prevents a shorter in-flight response erasing a later deadline.
			t.store.mu.Lock()
			previous, err := t.store.cooldown()
			if err == nil && meta.At.After(previous.At) {
				data, _ := json.Marshal(meta)
				err = t.store.save(t.store.cooldownKey(), data)
			}
			t.store.mu.Unlock()
			if errors.Is(err, errCacheUnavailable) {
				logging.FromContext(r.Context()).Warn("catalogue cache", "status", "storage_unavailable")
				err = nil
			}
			if err != nil {
				resp.Body.Close()
				return nil, err
			}
		}
	}
	if resp.StatusCode != 200 {
		return resp, nil
	}
	defer resp.Body.Close()
	body, e := io.ReadAll(io.LimitReader(resp.Body, maxCatalogueEntry+1))
	if e != nil {
		return nil, e
	}
	if len(body) > maxCatalogueEntry {
		return nil, errors.New("catalogue page too large")
	}
	body, e = cleanCataloguePage(r, body)
	if e != nil {
		return nil, fmt.Errorf("invalid catalogue page")
	}
	if e := validateCatalogueChain(r, body, traversal); e != nil {
		return nil, e
	}
	if t.store.ttl != 0 && r.Context().Err() == nil {
		var generation [16]byte
		if _, e := rand.Read(generation[:]); e != nil {
			return nil, e
		}
		rec := catalogueRecord{Version: 1, Key: key, Saved: time.Now(), Body: body, Generation: hex.EncodeToString(generation[:]), Parent: traversal.parent}
		traversal.parent = rec.Generation
		if e := t.store.saveRecord(rec); e != nil {
			if !errors.Is(e, errCacheUnavailable) {
				return nil, e
			}
			logging.FromContext(r.Context()).Warn("catalogue cache", "status", "storage_unavailable")
		}
	}
	return cachedResponse(r, body), nil
}
