package spotifyapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/zmb3/spotify/v2"
)

// Only the fields needed for music synchronization are decoded. A pointer
// distinguishes a null/unavailable item, without the SDK's legacy union decoder.
type playlistItemsPage struct {
	Next  string `json:"next"`
	Items []struct {
		IsLocal bool `json:"is_local"`
		Item    *struct {
			ID      spotify.ID `json:"id"`
			Type    string     `json:"type"`
			IsLocal bool       `json:"is_local"`
		} `json:"item"`
	} `json:"items"`
}

// UnmarshalJSON validates just the page envelope and item presence, leaving
// unrelated Spotify fields alone. Explicit item:null remains an unavailable item.
func (p *playlistItemsPage) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return fmt.Errorf("playlist items page must be an object")
	}
	var entries []json.RawMessage
	raw, exists := fields["items"]
	if !exists {
		return fmt.Errorf("playlist items page is missing items")
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return err
	}
	if entries == nil {
		return fmt.Errorf("playlist items must be a non-null array")
	}
	for _, entry := range entries {
		var wrapper map[string]json.RawMessage
		if err := json.Unmarshal(entry, &wrapper); err != nil {
			return err
		}
		itemRaw, exists := wrapper["item"]
		if !exists {
			return fmt.Errorf("playlist items entry is missing item")
		}
		if isJSONNull(wrapper["is_local"]) {
			return fmt.Errorf("playlist items is_local must be a boolean")
		}
		if !isJSONNull(itemRaw) {
			var item map[string]json.RawMessage
			if err := json.Unmarshal(itemRaw, &item); err != nil {
				return err
			}
			if isJSONNull(item["type"]) || isJSONNull(item["is_local"]) {
				return fmt.Errorf("playlist item type/is_local must not be null")
			}
		}
	}
	var decoded playlistItemsPage
	nextRaw, exists := fields["next"]
	if !exists {
		return fmt.Errorf("playlist items page is missing next")
	}
	if !bytes.Equal(bytes.TrimSpace(nextRaw), []byte("null")) {
		var next string
		if err := json.Unmarshal(nextRaw, &next); err != nil {
			return err
		}
		if next == "" {
			return fmt.Errorf("playlist items next must be a URL or null")
		}
		if _, err := url.Parse(next); err != nil {
			return fmt.Errorf("playlist items next is malformed")
		}
		decoded.Next = next
	}
	if err := json.Unmarshal(raw, &decoded.Items); err != nil {
		return err
	}
	*p = decoded
	return nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func (c *Client) playlistItemsURL(id spotify.ID) string {
	return c.baseURL + "playlists/" + url.PathEscape(string(id)) + "/items"
}

// playlistRequest deliberately makes exactly one request: neither reads nor
// mutations retry rate limits or fall back to the deprecated /tracks contract.
func (c *Client) playlistRequest(ctx context.Context, method, endpoint string, body any, result any, status int) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(playlistContext(ctx, endpoint), method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != status {
		var response struct {
			Error spotify.Error `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil || response.Error.Message == "" {
			response.Error.Message = fmt.Sprintf("spotify: HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		}
		response.Error.Status = resp.StatusCode
		return response.Error
	}
	if result != nil {
		decoder := json.NewDecoder(resp.Body)
		if err := decoder.Decode(result); err != nil {
			return err
		}
		var trailing json.RawMessage
		if err := decoder.Decode(&trailing); err != io.EOF {
			return fmt.Errorf("Spotify API response must contain exactly one JSON document")
		}
		return nil
	}
	return nil
}
