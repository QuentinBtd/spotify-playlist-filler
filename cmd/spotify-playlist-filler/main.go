// Command spotify-playlist-filler synchronizes playlists with artists' catalogues.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/QuentinBtd/spotify-playlist-filler/internal/app"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/config"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/spotifyapi"
	"github.com/QuentinBtd/spotify-playlist-filler/internal/spotifyauth"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "spotify-playlist-filler:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	path, err := config.ParseArgs(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	cfg, err := config.Load(path, os.LookupEnv)
	if err != nil {
		return err
	}
	if len(cfg.Playlists) == 0 {
		fmt.Fprintln(stdout, "No playlists configured; nothing to do.")
		return nil
	}
	client, err := spotifyauth.Login(ctx, cfg.SpotifyID, cfg.SpotifySecret, stdout)
	if err != nil {
		return err
	}
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return fmt.Errorf("read current Spotify user: %w", err)
	}
	fmt.Fprintln(stdout, "You are logged in as:", user.ID)
	catalog := spotifyapi.New(client)
	for _, playlist := range cfg.Playlists {
		if cfg.Verbose {
			fmt.Fprintf(stderr, "Processing playlist %q (%s)\n", playlist.Name, playlist.ID)
		}
		if err := app.Fill(ctx, catalog, playlist); err != nil {
			return fmt.Errorf("fill playlist %q (%s): %w", playlist.Name, playlist.ID, err)
		}
		fmt.Fprintf(stdout, "Updated playlist %q (%s)\n", playlist.Name, playlist.ID)
	}
	return nil
}
