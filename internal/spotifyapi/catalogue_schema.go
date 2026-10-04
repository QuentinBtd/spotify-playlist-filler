package spotifyapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
)

func catalogueQueryInt(q url.Values, key string, fallback int) (int, error) {
	values, exists := q[key]
	if !exists {
		return fallback, nil
	}
	if len(values) != 1 || values[0] == "" {
		return 0, errors.New("invalid catalogue query count")
	}
	n, err := strconv.Atoi(values[0])
	if err != nil || n < 0 {
		return 0, errors.New("invalid catalogue query count")
	}
	return n, nil
}

// Validate every consumed page, including cached pages and search pages that
// precede an early exact match. Full catalogue traversals must end at total.
func validateCatalogueChain(r *http.Request, body []byte, traversal *catalogueTraversal) error {
	var page struct {
		Artists json.RawMessage   `json:"artists"`
		Items   []json.RawMessage `json:"items"`
		Offset  int               `json:"offset"`
		Total   int               `json:"total"`
		Next    *string           `json:"next"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return err
	}
	if page.Artists != nil {
		if err := json.Unmarshal(page.Artists, &page); err != nil {
			return err
		}
	}
	if page.Offset != traversal.count || (traversal.started && page.Total != traversal.total) {
		return errors.New("inconsistent catalogue page chain")
	}
	if traversal.expected != "" {
		expected, err := url.Parse(traversal.expected)
		if err != nil || expected.Query().Encode() != r.URL.Query().Encode() {
			return errors.New("catalogue request differs from next page")
		}
	}
	count := traversal.count + len(page.Items)
	if page.Next == nil && count != page.Total {
		return errors.New("incomplete catalogue aggregate")
	}
	traversal.started = true
	traversal.total, traversal.count = page.Total, count
	traversal.expected = ""
	if page.Next != nil {
		traversal.expected = *page.Next
	}
	return nil
}

// Retain only synchronization fields, never arbitrary response bodies. Each page
// is persisted independently, but reusable only as part of a complete bound
// entity chain: callers still follow every next page.
func cleanCataloguePage(r *http.Request, data []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil || fields["error"] != nil {
		return nil, errors.New("invalid catalogue page")
	}
	search := r.URL.Path == "/search" || len(r.URL.Path) >= 7 && r.URL.Path[len(r.URL.Path)-7:] == "/search"
	if search {
		if json.Unmarshal(fields["artists"], &fields) != nil || fields == nil {
			return nil, errors.New("invalid catalogue search page")
		}
	}
	var entries []json.RawMessage
	if json.Unmarshal(fields["items"], &entries) != nil || entries == nil {
		return nil, errors.New("invalid catalogue items")
	}
	items := make([]map[string]string, 0, len(entries))
	for _, entry := range entries {
		var raw map[string]json.RawMessage
		if json.Unmarshal(entry, &raw) != nil || raw == nil {
			return nil, errors.New("invalid catalogue item")
		}
		var id string
		if json.Unmarshal(raw["id"], &id) != nil || id == "" {
			return nil, errors.New("invalid catalogue item ID")
		}
		item := map[string]string{"id": id}
		if search {
			var name string
			if json.Unmarshal(raw["name"], &name) != nil {
				return nil, errors.New("invalid catalogue artist name")
			}
			item["name"] = name
		}
		items = append(items, item)
	}
	var next any
	raw, exists := fields["next"]
	if !exists {
		return nil, errors.New("missing catalogue pagination")
	}
	if !isJSONNull(raw) {
		var text string
		if json.Unmarshal(raw, &text) != nil || text == "" {
			return nil, errors.New("invalid catalogue pagination")
		}
		reference, e := url.Parse(text)
		if e != nil {
			return nil, errors.New("invalid catalogue pagination")
		}
		target := r.URL.ResolveReference(reference)
		if validateDestination(target, r.URL, r.URL.EscapedPath()) != nil {
			return nil, errors.New("unsafe catalogue pagination")
		}
		if target.String() == r.URL.String() {
			return nil, errors.New("catalogue pagination cycle")
		}
		for key := range target.Query() {
			switch key {
			case "limit", "offset", "market", "include_groups", "type", "q":
			default:
				return nil, errors.New("unexpected catalogue pagination setting")
			}
		}
		next = target.String()
	}
	page := map[string]any{"items": items, "next": next}
	counts := make(map[string]int)
	for _, name := range []string{"offset", "limit", "total"} {
		raw, ok := fields[name]
		var n int
		if !ok || isJSONNull(raw) || json.Unmarshal(raw, &n) != nil || n < 0 {
			return nil, errors.New("invalid catalogue pagination count")
		}
		counts[name] = n
		page[name] = n
	}
	offset, limit, total := counts["offset"], counts["limit"], counts["total"]
	query := r.URL.Query()
	requestedOffset, err := catalogueQueryInt(query, "offset", 0)
	if err != nil {
		return nil, err
	}
	requestedLimit, err := catalogueQueryInt(query, "limit", -1)
	if err != nil || limit < 1 || limit != requestedLimit || offset != requestedOffset {
		return nil, errors.New("catalogue pagination does not match request")
	}
	if offset > total || len(items) > limit || len(items) > total-offset {
		return nil, errors.New("incomplete catalogue pagination")
	}
	if next == nil {
		if len(items) != total-offset {
			return nil, errors.New("incomplete catalogue pagination")
		}
	} else {
		if len(items) != limit || len(items) >= total-offset {
			return nil, errors.New("incomplete nonterminal catalogue pagination")
		}
		target, _ := url.Parse(next.(string))
		nextQuery := target.Query()
		nextOffset, e := catalogueQueryInt(nextQuery, "offset", -1)
		if e != nil || nextOffset != offset+len(items) {
			return nil, errors.New("invalid catalogue pagination progression")
		}
		for _, name := range []string{"limit", "market", "include_groups", "type", "q"} {
			if !reflect.DeepEqual(query[name], nextQuery[name]) {
				return nil, errors.New("changed catalogue pagination setting")
			}
		}
	}
	if search {
		return json.Marshal(map[string]any{"artists": page})
	}
	return json.Marshal(page)
}
