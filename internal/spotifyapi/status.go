package spotifyapi

import (
	"context"
	"errors"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/diagnostics"
	"sync/atomic"
)

type responseStatusKey struct{}

// Each call owns its status slot, including all pages. No shared client state.
func observeStatus(ctx context.Context) (context.Context, func(error) error) {
	status := new(atomic.Int32)
	quota := new(quotaMetadata)
	ctx = context.WithValue(ctx, quotaMetadataKey{}, quota)
	ctx = context.WithValue(ctx, responseStatusKey{}, status)
	return ctx, func(err error) error {
		var known *diagnostics.StatusError
		if errors.As(err, &known) && known.Status != 0 {
			return err
		}
		if err != nil && status.Load() >= 100 {
			return &diagnostics.StatusError{Err: err, Status: int(status.Load()), Reason: quota.Reason, RetryAfterSeconds: quota.Seconds, RetryAt: quota.At}
		}
		return err
	}
}
