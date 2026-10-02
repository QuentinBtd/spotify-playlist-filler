package spotifyauth

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
)

func TestLoginWithPortValidatesBeforeCachedAuthorization(t *testing.T) {
	_, path := cacheFixture(t, validToken())
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, port := range []int{-1, 0, 65536} {
		var output bytes.Buffer
		client, err := LoginWithPort(context.Background(), "test-id", "synthetic-secret", port, &output)
		if client != nil || err == nil || err.Error() != "OAuth callback port must be an integer from 1 to 65535" || output.Len() != 0 {
			t.Fatalf("invalid port=%d: err=%v", port, err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("invalid callback port changed healthy cache")
	}
}

func TestLoginWithPortCanceledBeforeCache(t *testing.T) {
	t.Setenv("SPF_TOKEN_CACHE", t.TempDir()+"/tokens")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client, err := LoginWithPort(ctx, "test-id", "synthetic-secret", 9000, nil)
	if client != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled login: %v", err)
	}
	if _, err := os.Stat(os.Getenv("SPF_TOKEN_CACHE")); !os.IsNotExist(err) {
		t.Fatal("canceled login touched cache")
	}
}
