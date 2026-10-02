package main

import (
	"github.com/QuentinBtd/spotify-playlist-filler/internal/diagnostics"
	"log/slog"
)

// This is the only synchronization ERROR emitter. Never pass the raw error to slog.
func logSynchronizationFailure(logger *slog.Logger, err error, priorPlaylistCompleted bool) {
	message := "sync failed; inspect playlists before rerunning"
	if !priorPlaylistCompleted && diagnostics.Unchanged(err) {
		message = "sync failed; playlist unchanged"
	}
	fields := append([]any{"stage", "synchronization"}, diagnostics.Fields(err)...)
	logger.Error(message, fields...)
}
