// Command spotify-playlist-filler synchronizes playlists with artists' catalogues.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/app"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/logging"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/spotifyapi"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/spotifyauth"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) (runErr error) {
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	stage := "arguments"
	var configError error
	defer func() {
		if runErr != nil {
			if configError != nil {
				logger.Error("run failed", "stage", stage, "error", configError)
			} else if errors.Is(runErr, app.ErrPlaylistCapacity) {
				logger.Error("sync incomplete; capacity skips left unchanged", "stage", stage)
			} else if stage == "synchronization" {
				logger.Error("sync failed; inspect playlists before rerunning", "stage", stage)
			} else {
				logger.Error("run failed", "stage", stage)
			}
		}
	}()
	path, err := config.ParseArgs(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	stage = "configuration"
	cfg, err := config.Load(path, os.LookupEnv)
	if err != nil {
		configError = err
		// Keep path errors intact for callers, but never log arbitrary SPF_CONFIG input.
		var pathError *os.PathError
		if errors.As(err, &pathError) {
			configError = fmt.Errorf("read config: %w", pathError.Err)
		}
		return err
	}
	var level slog.Level
	// Load has already validated the canonical lowercase level.
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	logger = slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))
	for _, setting := range cfg.Deprecated {
		logger.Warn("deprecated setting", "setting", setting)
	}
	logger.Info("sync started", "playlists", len(cfg.Playlists))
	if len(cfg.Playlists) == 0 {
		logger.Info("sync complete", "playlists", 0)
		return nil
	}
	stage = "login"
	logger.Info("login started")
	client, err := spotifyauth.LoginWithPort(ctx, cfg.SpotifyID, cfg.SpotifySecret, cfg.OAuthPort, stdout)
	if err != nil {
		return err
	}
	ctx = logging.WithContext(ctx, logger)
	stage = "read current user"
	catalog := spotifyapi.New(client)
	_, err = catalog.CurrentUser(ctx)
	if err != nil {
		return fmt.Errorf("read current Spotify user: %w", err)
	}
	logger.Info("login complete")
	stage = "synchronization"
	var capacityErrors []error
	for index, playlist := range cfg.Playlists {
		playlistCtx := logging.WithContext(ctx, logger.With("playlist", index+1))
		logger.Info("configured playlist", "playlist", index+1, "artists", len(playlist.Artists))
		if err := app.FillWithConcurrency(playlistCtx, catalog, playlist, cfg.ReadConcurrency); err != nil {
			if errors.Is(err, app.ErrPlaylistCapacity) {
				capacityErrors = append(capacityErrors, fmt.Errorf("playlist %d: %w", index+1, err))
				continue
			}
			return fmt.Errorf("fill playlist %q (%s): %w", playlist.Name, playlist.ID, err)
		}
		logger.Info("playlist sync complete", "playlist", index+1)
	}
	logger.Info("sync complete", "playlists", len(cfg.Playlists), "skipped", len(capacityErrors))
	return errors.Join(capacityErrors...)
}
