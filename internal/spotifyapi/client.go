// Package spotifyapi adapts the Spotify SDK to the playlist application.
package spotifyapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/app"
	"github.com/zmb3/spotify/v2"
)

type Client struct{ client *spotify.Client }

func New(client *spotify.Client) *Client { return &Client{client: client} }

var _ app.Catalog = (*Client)(nil)

func (c *Client) PlaylistTracks(ctx context.Context, id spotify.ID) ([]spotify.ID, error) {
	// v2.2.0's union decoder requires nonstandard track/episode boolean flags.
	// The track endpoint decodes the standard type discriminator reliably.
	page, err := c.client.GetPlaylistTracks(ctx, id)
	if err != nil {
		return nil, err
	}
	var result []spotify.ID
	for {
		for _, item := range page.Tracks {
			if item.IsLocal || item.Track.Type != "track" {
				continue
			}
			if item.Track.ID != "" {
				result = append(result, item.Track.ID)
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
	_, err := c.client.AddTracksToPlaylist(ctx, id, tracks...)
	return err
}

func (c *Client) RemoveTracks(ctx context.Context, id spotify.ID, tracks ...spotify.ID) error {
	_, err := c.client.RemoveTracksFromPlaylist(ctx, id, tracks...)
	return err
}
