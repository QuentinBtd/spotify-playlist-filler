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
// Playlist requests use the current /items contract; SDK retries stay disabled.
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
	logger := logging.FromContext(ctx)
	skipped, pages := 0, 0
	var result []spotify.ID
	visited := make(map[string]bool)
	for next := c.playlistItemsURL(id); next != ""; {
		u, err := url.Parse(next)
		if err != nil {
			return nil, err
		}
		// Normalize origin and query spelling only for cycle detection. The
		// original returned query is still used for the actual request.
		key := origin(u) + u.EscapedPath() + "?" + u.Query().Encode()
		if visited[key] {
			return nil, fmt.Errorf("playlist items pagination cycle")
		}
		visited[key] = true
		var page playlistItemsPage
		if err := c.playlistRequest(ctx, http.MethodGet, next, nil, &page, http.StatusOK); err != nil {
			return nil, err
		}
		pages++
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
			return nil, err
		}
	}
	if skipped > 0 {
		logger.Warn("skipped unsupported playlist items", "count", skipped)
	}
	return result, nil
}

func (c *Client) ArtistAlbums(ctx context.Context, id spotify.ID) ([]app.Album, error) {
	page, err := c.client.GetArtistAlbums(ctx, id, nil)
	if err != nil {
		return nil, err
	}
	var result []app.Album
	for {
		for _, album := range page.Albums {
			result = append(result, app.Album{ID: album.ID})
		}
		err = c.client.NextPage(ctx, page)
		if errors.Is(err, spotify.ErrNoMorePages) {
			return result, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

func (c *Client) AlbumTracks(ctx context.Context, id spotify.ID) ([]spotify.ID, error) {
	page, err := c.client.GetAlbumTracks(ctx, id)
	if err != nil {
		return nil, err
	}
	var result []spotify.ID
	for {
		for _, track := range page.Tracks {
			if track.ID != "" {
				result = append(result, track.ID)
			}
		}
		err = c.client.NextPage(ctx, page)
		if errors.Is(err, spotify.ErrNoMorePages) {
			return result, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

func (c *Client) SearchArtist(ctx context.Context, name string) (spotify.ID, error) {
	offset := 0
	for {
		// Search returns a nested artists object; SDK NextPage expects a raw page.
		result, err := c.client.Search(ctx, name, spotify.SearchTypeArtist, spotify.Offset(offset))
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
