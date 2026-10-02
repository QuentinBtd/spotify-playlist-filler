package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"github.com/zmb3/spotify/v2"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestFailureClassificationAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		kind   string
		status int
	}{
		{"sdk value", spotify.Error{Message: "SECRET_BODY", Status: 403}, "http", 403},
		{"invalid request", spotify.Error{Message: "SECRET_BODY", Status: 400}, "http", 400},
		{"transport", &url.Error{Op: "Get", URL: "https://SECRET_HOST/?token=SECRET_TOKEN", Err: errors.New("SECRET_BODY")}, "transport", 0},
		{"timeout", fmt.Errorf("SECRET_BODY: %w", context.DeadlineExceeded), "timeout", 0},
		{"cancel", fmt.Errorf("SECRET_BODY: %w", context.Canceled), "canceled", 0},
		{"schema", errors.New("SECRET_BODY"), "invalid_response", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Wrap(WithPlaylist(context.Background(), 4), tc.err, ArtistAlbums, 2, true)
			if !errors.Is(err, tc.err) {
				t.Fatal("lost original identity")
			}
			want := []any{"operation", "artist_albums", "failure_kind", tc.kind}
			if tc.status != 0 {
				want = append(want, "http_status", tc.status)
			}
			want = append(want, "playlist", 4, "artist", 2)
			got := Fields(fmt.Errorf("PRIVATE_ARTIST: %w", err))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("fields=%v want=%v", got, want)
			}
			if strings.Contains(fmt.Sprint(got), "SECRET_") {
				t.Fatal("raw error leaked")
			}
		})
	}
}
