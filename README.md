# Spotify Playlist Filler

Synchronize Spotify playlists with the tracks from configured artists' albums.
Existing tracks outside that catalogue are removed. **This is a synchronization
tool, not an append-only importer: back up important playlists before running it.**

## Requirements and setup

- Go 1.26 or newer to build from source.
- A Spotify developer application and an account allowed to modify the playlists.
- Register **`http://127.0.0.1:8080/callback`** in your application's redirect URIs.
  Spotify requires an explicit loopback IP, not `localhost`.
- Run on the same machine as your browser, with local TCP port 8080 available.

```sh
cp config.example.yml config.yml
# Edit config.yml: select your own playlists and artists.
export SPOTIFY_ID='your-client-id'
export SPOTIFY_SECRET='your-client-secret'
make build
./bin/spotify-playlist-filler -config config.yml
```

Open the printed Spotify authorization URL in your browser. Tokens are held in
memory only; each invocation requires login. Login expires after five minutes;
Ctrl+C cancels login or processing. The callback server binds only to loopback
and closes after login. The account must be authorized by your developer app;
Spotify's app access restrictions and API availability still apply.

`--help` / `-h` prints usage without reading configuration or contacting Spotify.
A single positional config path also works:

```sh
go run ./cmd/spotify-playlist-filler -config /path/to/config.yml
./bin/spotify-playlist-filler /path/to/config.yml
```

## Configuration

The YAML keys remain compatible with the former `config.yml`:

```yaml
SPOTIFY_ID: ""     # Prefer the environment variable
SPOTIFY_SECRET: "" # Prefer the environment variable
verbose: false
playlists:
  - name: "My playlist"
    uri: "3RiBOmtagQlYUd2XOeWPUd" # Bare Spotify ID, not a URL or spotify: URI
    shuffle_order: false
    artists:
      - name: "Rick Astley" # Optional when uri is supplied
        uri: "0gxyHStUsqpMadRV0Di1Qt"
        use_name_instead_of_uri: false
        albums_to_skip:
          - name: "Beautiful Life" # Informational only; exclusion uses uri
            uri: "3IqiZzsC1gef7qgvCXTqTj"
      - name: "Imagine Dragons"
        use_name_instead_of_uri: true
    albums_to_skip:
      - name: "Rick Astley - 50"
        uri: "7IW3NEq3Fxtm7FhOcosnBy"
```

Nonempty `SPOTIFY_ID`, `SPOTIFY_SECRET` and `SPF_VERBOSE` environment variables
override YAML values. `SPF_VERBOSE` accepts Go boolean values such as `true` and
`false`; invalid values fail validation. Verbose mode prints playlist progress.
The config file is required even when credentials come from the environment.
Never commit credentials; local `config.yml`, `.env` files and binaries are ignored.

Each playlist needs an ID and at least one artist. Artists need either an ID or,
when `use_name_instead_of_uri` is true, an exact artist name. Album exclusions
require IDs. Empty `playlists` is a no-op. Unknown or duplicate YAML keys are
rejected to prevent typos from silently changing a destructive operation.

### Synchronization behavior and safety

- Fetch every page of playlist tracks, artist albums and album tracks before
  modifying that playlist. Search also checks every result page for an exact name.
- Album exclusions from artists and the playlist form one playlist-wide union,
  preserving the previous behavior. Album `name` is informational, not a filter.
- Desired tracks are deduplicated by Spotify track ID in discovery order, not by
  recording or title. Existing duplicates of retained tracks are left untouched.
- Without shuffling, keep existing desired tracks, remove obsolete tracks and
  append missing tracks. With shuffling, remove all supported existing tracks
  then add the unique desired list in randomized order.
- Local files, null/unavailable tracks and podcast episodes are ignored and left
  untouched; shuffling only replaces supported Spotify music tracks.
- Mutations use batches of at most 100 track IDs. Stop on the first error; do not
  retry writes automatically, since a failed response can still represent a
  completed operation. Rate limits are reported instead of retried indefinitely.
- A failed read or a missing searched artist aborts before writes to that playlist.
  Earlier playlists may already have been updated. Writes are **not atomic**:
  a failed batch can leave a partially updated playlist. Inspect it before rerunning.
- A successfully retrieved empty catalogue (including all albums excluded) still
  produces an empty desired track list and removes existing supported tracks.

## Project layout

```text
cmd/spotify-playlist-filler/  CLI and application wiring
internal/config/             YAML, environment overrides and arguments
internal/app/                synchronization, exclusions, diff/shuffle/batches
internal/spotifyapi/         Spotify SDK adapter and pagination
internal/spotifyauth/        interactive OAuth and loopback callback lifecycle
```

The module path is `github.com/QuentinBtd/spotify-playlist-filler`. The existing
Spotify SDK version is intentionally retained; API compatibility and real-account
access must be verified separately with an authorized Spotify application.
Tests use in-memory catalogues and local HTTP fixtures, never a real playlist.

## Development and builds

```sh
make fmt
make check       # tests, vet and gofmt check
make race        # go test -race ./...
make build       # bin/spotify-playlist-filler
make build-all   # Linux/macOS/Windows, amd64 and arm64, under build/
make clean       # remove only local build and coverage artifacts
```

`GO` can override the Go executable; `CONFIG` changes the `make run` config path.
`make exec` remains an alias for `make run`. `bash build.sh [binary-name]` works
from any working directory and no longer deletes the global Go build cache.
Cross-builds are limited to the six explicitly supported desktop/server targets.

## Migration from the original layout

- `src/main.go` is replaced by `cmd/spotify-playlist-filler`; use
  `go run ./cmd/spotify-playlist-filler`, not `go run src/main.go`.
- The formerly committed root binary is removed; use `make build` and `bin/`.
- `config.yml` is now a local ignored file; copy `config.example.yml` on a fresh
  checkout. Existing YAML keys including `uri` and `playlists` are unchanged.
- The old README's `PLAYLISTS_TO_FILL` key was never implemented. Use `playlists`;
  strict validation now reports that incorrect key rather than ignoring it.
- Config flags are now actually parsed; `-config PATH` and a positional path work.
- Change the registered OAuth callback from `http://localhost:8080/callback` to
  `http://127.0.0.1:8080/callback` before logging in.
- Read/config failures no longer continue silently. Missing artists stop processing
  instead of potentially deleting that artist's tracks. Infinite retries are removed.
- Go 1.26 is the declared minimum, retained from the modernization work in progress.
