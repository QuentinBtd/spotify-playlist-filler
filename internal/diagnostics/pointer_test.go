package diagnostics

import (
	"fmt"
	"testing"

	"github.com/zmb3/spotify/v2"
)

func TestSDKPointerStatusClassification(t *testing.T) {
	for _, tt := range []struct {
		status int
		kind   string
	}{{403, "http"}, {401, "oauth"}, {400, "http"}, {404, "http"}} {
		t.Run(fmt.Sprint(tt.status), func(t *testing.T) {
			original := &spotify.Error{Status: tt.status, Message: "synthetic private response"}
			err := fmt.Errorf("wrapped: %w", original)
			status, kind := classify(err)
			if status != tt.status || kind != tt.kind {
				t.Fatalf("status=%d kind=%s; want %d/%s", status, kind, tt.status, tt.kind)
			}
		})
	}
}
