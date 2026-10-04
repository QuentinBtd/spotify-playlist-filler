package spotifyapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Successful synthetic pages report the size actually requested by the client.
func fixtureCatalogueLimit(r *http.Request) int {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 {
		return 50
	}
	return limit
}

func fixtureRepeatedItems(id string, count int) string {
	return strings.TrimSuffix(strings.Repeat(fmt.Sprintf(`{"id":%q},`, id), count), ",")
}
