package spotifyapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/diagnostics"
)

type quotaMetadataKey struct{}
type quotaMetadata struct {
	Reason  string    `json:"reason"`
	Seconds int64     `json:"retry_after_seconds"`
	At      time.Time `json:"retry_at"`
}
type replayBody struct {
	io.Reader
	io.Closer
}

// Inspect only a bounded prefix and preserve the original body/close semantics.
// Reason is an enum, never an arbitrary error message or server text.
func quotaDetails(resp *http.Response) quotaMetadata {
	var meta quotaMetadata
	if resp.StatusCode != 429 {
		return meta
	}
	if delay, valid := retryDelay(resp.Header.Get("Retry-After")); valid {
		meta.Seconds = int64(delay / time.Second)
		meta.At = time.Now().Add(delay).UTC()
	}
	prefix, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	resp.Body = replayBody{Reader: io.MultiReader(bytes.NewReader(prefix), resp.Body), Closer: resp.Body}
	var body struct {
		Error struct {
			Reason string `json:"reason"`
		} `json:"error"`
	}
	if json.Unmarshal(prefix, &body) == nil && body.Error.Reason == "QUOTA_EXCEEDED" {
		meta.Reason = "QUOTA_EXCEEDED"
	}
	return meta
}
func (s *catalogueStore) cooldownKey() string { return s.digest("cooldown-v1") }
func (s *catalogueStore) cooldown() (quotaMetadata, error) {
	if s.ttl == 0 {
		return quotaMetadata{}, nil
	}
	copy := catalogueStore{dir: s.dir, namespace: s.namespace, ttl: 30 * 24 * time.Hour}
	data, e := copy.load(s.cooldownKey())
	if errors.Is(e, errInvalidCache) {
		return quotaMetadata{}, nil
	}
	if e != nil {
		return quotaMetadata{}, e
	}
	var meta quotaMetadata
	if json.Unmarshal(data, &meta) != nil || meta.Seconds <= 30 || meta.Seconds > int64(30*24*time.Hour/time.Second) || meta.At.After(time.Now().Add(30*24*time.Hour)) {
		return quotaMetadata{}, nil
	}
	if meta.Reason != "QUOTA_EXCEEDED" {
		meta.Reason = ""
	}
	return meta, nil
}
func (m quotaMetadata) err() error {
	return &diagnostics.StatusError{Err: diagnostics.ErrRateLimited, Status: 429, Reason: m.Reason, RetryAfterSeconds: m.Seconds, RetryAt: m.At}
}
