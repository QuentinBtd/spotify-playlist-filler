// Package spotifyapi adapts the Spotify SDK to the playlist application.
package spotifyapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/app"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/logging"
	"github.com/zmb3/spotify/v2"
)

type Client struct {
	client  *spotify.Client
	http    *http.Client
	baseURL string
}

// New shares the authenticated transport and timeout with the catalogue SDK.
// Playlist requests use /items; SDK retries stay disabled. The shared guarded
// transport provides bounded GET-only 429 cooldown/retries.
func New(httpClient *http.Client) *Client {
	return newClient(httpClient, "https://api.spotify.com/v1/")
}

func newClient(httpClient *http.Client, baseURL string) *Client {
	safe := secureClient(httpClient, baseURL)
	return &Client{client: spotify.New(safe, spotify.WithBaseURL(baseURL), spotify.WithRetry(false)), http: safe, baseURL: baseURL}
}

var _ app.Catalog = (*Client)(nil)

// CurrentUser uses the same guarded OAuth client as playlist and catalogue reads.
func (c *Client) CurrentUser(ctx context.Context) (*spotify.PrivateUser, error) {
	return c.client.CurrentUser(ctx)
}

func (c *Client) PlaylistTracks(ctx context.Context, id spotify.ID) ([]spotify.ID, error) {
	tracks, _, err := c.PlaylistTracksWithCount(ctx, id)
	return tracks, err
}

// PlaylistTracksWithCount counts all received entries, including unsupported items.
// An incomplete read returns neither tracks nor a usable count.
func (c *Client) PlaylistTracksWithCount(ctx context.Context, id spotify.ID) ([]spotify.ID, int, error) {
	logger := logging.FromContext(ctx)
	skipped, pages, total := 0, 0, 0
	var result []spotify.ID
	visited := make(map[string]bool)
	for next := c.playlistItemsURL(id); next != ""; {
		u, err := url.Parse(next)
		if err != nil {
			return nil, 0, err
		}
		// Normalize origin and query spelling only for cycle detection. The
		// original returned query is still used for the actual request.
		key := origin(u) + u.EscapedPath() + "?" + u.Query().Encode()
		if visited[key] {
			return nil, 0, fmt.Errorf("playlist items pagination cycle")
		}
		visited[key] = true
		var page playlistItemsPage
		if err := c.playlistRequest(ctx, http.MethodGet, next, nil, &page, http.StatusOK); err != nil {
			return nil, 0, err
		}
		pages++
		total += len(page.Items)
		logger.Debug("playlist items page", "page", pages, "items", len(page.Items))
		for _, entry := range page.Items {
			item := entry.Item
			if entry.IsLocal || item == nil || item.IsLocal || item.Type != "track" || item.ID == "" {
				skipped++
				continue
			}
			result = append(result, item.ID)
		}
		if page.Next == "" {
			break
		}
		next, err = c.resolveItemsNext(next, page.Next, c.playlistItemsURL(id))
		if err != nil {
			return nil, 0, err
		}
	}
	if skipped > 0 {
		logger.Warn("skipped unsupported playlist items", "count", skipped)
	}
	return result, total, nil
}

func (c *Client) ArtistAlbums(ctx context.Context, id spotify.ID) (result []app.Album, err error) {
	ctx, finish := observeStatus(catalogueContext(ctx, c.baseURL+"artists/"+url.PathEscape(string(id))+"/albums"))
	defer func() { err = finish(err) }()
	logger := logging.FromContext(ctx)
	pages := 1
	logger.Debug("artist albums page read started", "page", pages, "cumulative_albums", 0)
	page, err := c.client.GetArtistAlbums(ctx, id, nil, spotify.Limit(10))
	if err != nil {
		return nil, err
	}
	for {
		for _, album := range page.Albums {
			result = append(result, app.Album{ID: album.ID})
		}
		logger.Debug("artist albums page", "page", pages, "albums", len(page.Albums), "cumulative_albums", len(result))
		if page.Next != "" {
			logger.Debug("artist albums page read started", "page", pages+1, "cumulative_albums", len(result))
		}
		err = c.client.NextPage(ctx, page)
		if errors.Is(err, spotify.ErrNoMorePages) {
			return result, nil
		}
		if err != nil {
			return nil, err
		}
		pages++
	}
}

func (c *Client) AlbumTracks(ctx context.Context, id spotify.ID) (result []spotify.ID, err error) {
	ctx, finish := observeStatus(catalogueContext(ctx, c.baseURL+"albums/"+url.PathEscape(string(id))+"/tracks"))
	defer func() { err = finish(err) }()
	logger := logging.FromContext(ctx)
	pages := 1
	logger.Debug("album tracks page read started", "page", pages, "cumulative_tracks", 0)
	page, err := c.client.GetAlbumTracks(ctx, id, spotify.Limit(50))
	if err != nil {
		return nil, err
	}
	for {
		for _, track := range page.Tracks {
			if track.ID != "" {
				result = append(result, track.ID)
			}
		}
		logger.Debug("album tracks page", "page", pages, "tracks", len(page.Tracks), "cumulative_tracks", len(result))
		if page.Next != "" {
			logger.Debug("album tracks page read started", "page", pages+1, "cumulative_tracks", len(result))
		}
		err = c.client.NextPage(ctx, page)
		if errors.Is(err, spotify.ErrNoMorePages) {
			return result, nil
		}
		if err != nil {
			return nil, err
		}
		pages++
	}
}

func (c *Client) SearchArtist(ctx context.Context, name string) (id spotify.ID, err error) {
	ctx, finish := observeStatus(catalogueContext(ctx, c.baseURL+"search"))
	defer func() { err = finish(err) }()
	offset := 0
	for {
		// Search returns a nested artists object; SDK NextPage expects a raw page.
		result, err := c.client.Search(ctx, name, spotify.SearchTypeArtist, spotify.Offset(offset), spotify.Limit(10))
		if err != nil {
			return "", err
		}
		if result.Artists == nil {
			return "", nil
		}
		for _, artist := range result.Artists.Artists {
			if artist.Name == name {
				return artist.ID, nil
			}
		}
		page := result.Artists
		if page.Next == "" {
			return "", nil
		}
		next := page.Offset + page.Limit
		if next <= offset {
			return "", fmt.Errorf("artist search returned invalid pagination")
		}
		offset = next
	}
}

func (c *Client) AddTracks(ctx context.Context, id spotify.ID, tracks ...spotify.ID) error {
	if len(tracks) > 100 {
		return fmt.Errorf("add playlist items: maximum 100 tracks per request")
	}
	uris := make([]string, len(tracks))
	for i, track := range tracks {
		uris[i] = "spotify:track:" + string(track)
	}
	body := struct {
		URIs []string `json:"uris"`
	}{URIs: uris}
	var snapshot struct {
		ID string `json:"snapshot_id"`
	}
	return c.playlistRequest(ctx, http.MethodPost, c.playlistItemsURL(id), body, &snapshot, http.StatusCreated)
}

func (c *Client) RemoveTracks(ctx context.Context, id spotify.ID, tracks ...spotify.ID) error {
	if len(tracks) > 100 {
		return fmt.Errorf("remove playlist items: maximum 100 tracks per request")
	}
	type playlistURI struct {
		URI string `json:"uri"`
	}
	items := make([]playlistURI, len(tracks))
	for i, track := range tracks {
		items[i].URI = "spotify:track:" + string(track)
	}
	body := struct {
		Items []playlistURI `json:"items"`
	}{Items: items}
	var snapshot struct {
		ID string `json:"snapshot_id"`
	}
	return c.playlistRequest(ctx, http.MethodDelete, c.playlistItemsURL(id), body, &snapshot, http.StatusOK)
}
