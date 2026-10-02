// Package diagnostics projects errors into a fixed, non-sensitive log schema.
package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zmb3/spotify/v2"
	"golang.org/x/oauth2"
	"net"
)

type Operation string

const (
	ArtistAlbums Operation = "artist_albums"
	AlbumTracks  Operation = "album_tracks"
	SearchArtist Operation = "search_artist"
	ReadPlaylist Operation = "read_playlist"
	AddItems     Operation = "add_items"
	RemoveItems  Operation = "remove_items"
)

type indexKey struct{}

func WithPlaylist(ctx context.Context, index int) context.Context {
	return context.WithValue(ctx, indexKey{}, index)
}

// StatusError preserves observed response metadata without replacing error identity.
type StatusError struct {
	Err    error
	Status int
	OAuth  bool
}

func (e *StatusError) Error() string { return e.Err.Error() }
func (e *StatusError) Unwrap() error { return e.Err }

type Failure struct {
	Err              error
	operation        Operation
	playlist, artist int
	unchanged        bool
}

func (e *Failure) Error() string           { return e.Err.Error() }
func (e *Failure) Unwrap() error           { return e.Err }
func (e *Failure) PlaylistUnchanged() bool { return e.unchanged }
func Wrap(ctx context.Context, err error, op Operation, artist int, unchanged bool) error {
	if err == nil {
		return nil
	}
	playlist, _ := ctx.Value(indexKey{}).(int)
	return &Failure{Err: err, operation: op, playlist: playlist, artist: artist, unchanged: unchanged}
}
func (e *Failure) DiagnosticFields() []any {
	status, kind := classify(e.Err)
	fields := []any{"operation", string(e.operation), "failure_kind", kind}
	if status >= 100 && status <= 599 {
		fields = append(fields, "http_status", status)
	}
	if e.playlist > 0 {
		fields = append(fields, "playlist", e.playlist)
	}
	if e.artist > 0 {
		fields = append(fields, "artist", e.artist)
	}
	return fields
}
func classify(err error) (int, string) {
	status := 0
	oauth := false
	var observed *StatusError
	if errors.As(err, &observed) {
		status = observed.Status
		oauth = observed.OAuth
	}
	// Preserve both SDK error representations through wrapped errors.
	var sdk spotify.Error
	var sdkPointer *spotify.Error
	if status == 0 {
		if errors.As(err, &sdk) {
			status = sdk.Status
		} else if errors.As(err, &sdkPointer) && sdkPointer != nil {
			status = sdkPointer.Status
		}
	}
	var retrieve *oauth2.RetrieveError
	if errors.As(err, &retrieve) {
		oauth = true
		if retrieve.Response != nil && status == 0 {
			status = retrieve.Response.StatusCode
		}
	}
	if errors.Is(err, context.Canceled) {
		return status, "canceled"
	}
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
		return status, "timeout"
	}
	if oauth || status == 401 {
		return status, "oauth"
	}
	if status >= 400 {
		return status, "http"
	}
	var syntax *json.SyntaxError
	var types *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &types) {
		return status, "invalid_response"
	}
	if errors.As(err, &network) {
		return status, "transport"
	}
	// Catalogue validation/pagination failures have no trustworthy HTTP status.
	return status, "invalid_response"
}
func Fields(err error) []any {
	var failure *Failure
	if errors.As(err, &failure) {
		return failure.DiagnosticFields()
	}
	status, kind := classify(err)
	fields := []any{"failure_kind", kind}
	if status >= 100 && status <= 599 {
		fields = append(fields, "http_status", status)
	}
	return fields
}
func Unchanged(err error) bool {
	var failure *Failure
	return errors.As(err, &failure) && failure.PlaylistUnchanged()
}
