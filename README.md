# Spotify Playlist Filler

Synchronize Spotify playlists with the tracks from configured artists' albums.
Existing tracks outside that catalogue are removed. **This is a synchronization
tool, not an append-only importer: back up important playlists before running it.**

## Requirements and setup

- Install mise following its official getting-started documentation
  (`https://mise.jdx.dev/getting-started.html`) and put `mise` on your `PATH`.
  Use an official package manager or release archive; when downloading a release
  archive, verify its SHA-256 against that release's official `SHASUMS256.txt`.
- Go **1.27.1** is installed and selected by the repository's **`.mise.toml`**.
  `go.mod` retains Go 1.26 as the module's minimum, not the development pin.
- Bash and standard filesystem utilities must be available on the system for
  tasks. The race detector also needs a supported host and a system C compiler
  (GCC on Linux or Xcode Command Line Tools on macOS). Mise installs Go here,
  not those operating-system prerequisites. On Windows use WSL for these tasks;
  cross-building Windows executables from Linux/macOS needs no Windows compiler.
- A Spotify developer application and an account allowed to modify the playlists.
- Register the chosen **`http://127.0.0.1:PORT/callback`** (default PORT: 8080)
  in the **Spotify Developer dashboard**, under your application's redirect URIs.
  Use the exact chosen port and explicit loopback IP, not `localhost`.
- For initial authorization (or reauthorization), run on the same machine as your
  browser with that local TCP port available. Cached runs need neither.

From the repository root, review `.mise.toml` before trusting it, then install
its pinned toolchain (no shell activation is needed for `mise run`):

```sh
mise trust .mise.toml
mise install
mise tasks
cp config.example.yaml config.yaml
# Edit config.yaml: select your own playlists and artists.
export SPF_SPOTIFY_ID='your-client-id'
export SPF_SPOTIFY_SECRET='your-client-secret'
mise run build
./bin/spotify-playlist-filler -config config.yaml
```

Open the printed Spotify authorization URL in your browser on the first run.
Subsequent runs reuse the private local token cache and automatically refresh
expired access tokens, without opening the callback listener. Interactive login
expires after five minutes;
Ctrl+C cancels login or processing. The callback server binds only to loopback
and closes after login. The account must be authorized by your developer app;
Spotify's app access restrictions and API availability still apply.

`--help` / `-h` prints usage without reading configuration or contacting Spotify.
A single positional config path also works:

```sh
mise exec -- go run ./cmd/spotify-playlist-filler -config /path/to/config.yaml
./bin/spotify-playlist-filler /path/to/custom.yml
```

## Configuration

The default is `config.yaml`. If it is absent, an existing `config.yml` is used
for compatibility. `config.yaml` wins when both exist; read or decode failures
never fall back. Explicit `-config PATH` or a legacy positional path is used
exactly as supplied, including `.yml` paths; custom errors are not masked.
`SPF_CONFIG` selects an explicit path for the CLI or `mise run run` (see Development below).

Canonical YAML uses lowercase keys:

```yaml
spotify_id: ""     # Prefer SPF_SPOTIFY_ID
spotify_secret: "" # Prefer SPF_SPOTIFY_SECRET
log_level: info     # debug, info, warn or error
read_concurrency: 3 # 1..8; SPF_READ_CONCURRENCY overrides this
cache_ttl: 24h       # Duration 0s..720h; SPF_CACHE_TTL overrides; 0s disables
# cache_directory: /private/path/catalogue # Optional SPF_CACHE_DIRECTORY override
oauth_port: 8080  # Optional; callback port, integer 1..65535
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

`SPF_SPOTIFY_ID` and `SPF_SPOTIFY_SECRET` override YAML and deprecated credential
environment aliases whenever set; an explicitly empty canonical credential fails
validation rather than falling back. A nonempty `SPF_LOG_LEVEL` overrides
`log_level`. Levels are `debug`, `info`, `warn` and `error` (default: `info`),
validated before authentication, callback listeners or playlist mutations.
Logs use Go's structured text logger on **stderr**: info reports synchronization,
login, completed playlist counts and artist/album read stages before and after
requests. Numeric artist/album indices identify work without logging names or IDs.
Debug adds page and cumulative catalogue counts; warn reports capacity skips,
rate-limit waits, deprecated settings and skipped unsupported items. Error reports
failure once. `supported_tracks` counts music tracks eligible for synchronization;
`total_items` includes every received playlist entry (including null/local/episodes).
Raw upstream errors, credentials, tokens, Authorization headers and config contents
are not logged. Runtime failures report their stage rather than potentially
sensitive upstream details; synchronization failures advise inspecting playlists.
The interactive authorization URL remains on **stdout** at every log level,
because it is a functional login prompt, not telemetry.
The config file is required even when credentials come from the environment.
Never commit credentials; local `config.yaml`, `config.yml`, `.env` files and
binaries are ignored.

Each playlist needs an ID and at least one artist. Artists need either an ID or,
when `use_name_instead_of_uri` is true, an exact artist name. Album exclusions
require IDs. Empty `playlists` is a no-op. Unknown or duplicate YAML keys are
rejected to prevent typos from silently changing a destructive operation.

### Migration from legacy settings

For an existing config, rename uppercase credential keys to lowercase and replace
verbose mode with a level (do not keep both credential spellings):

```yaml
spotify_id: ""      # Supply via SPF_SPOTIFY_ID instead
spotify_secret: ""  # Supply via SPF_SPOTIFY_SECRET instead
log_level: info      # Use debug for detailed counts
# Keep your existing oauth_port and playlists entries.
```

```sh
export SPF_SPOTIFY_ID='your-client-id'
export SPF_SPOTIFY_SECRET='your-client-secret'
SPF_CONFIG='/path/to/config.yaml' SPF_LOG_LEVEL=debug mise run run
```

Deprecated `SPOTIFY_ID` / `SPOTIFY_SECRET` environment variables and uppercase
YAML keys remain accepted for migration. Canonical credential environment names
win over legacy environment names, which otherwise override YAML. Having both
canonical and legacy YAML keys for either credential is an error, including empty,
null or identical values; remove the legacy key. Unknown/duplicate fields and
malformed config are still rejected, without echoing their contents.

Deprecated YAML `verbose` and nonempty `SPF_VERBOSE` map `true` to `debug` and
`false` to `info` **only when neither explicit `log_level` nor nonempty
`SPF_LOG_LEVEL` is supplied**. `SPF_VERBOSE` overrides YAML `verbose` and accepts
Go boolean values; invalid values fail without echoing the value. Prefer levels.
Deprecated `CONFIG` remains a config-path fallback when `SPF_CONFIG` is empty or
unset; nonempty `SPF_CONFIG` wins. The direct CLI's explicit `-config`/positional
path wins over either environment path. Mise preserves its existing env-path
forwarding as `-config PATH` and forwards extra arguments unchanged. Deprecated
settings generate warnings (filtered when `log_level: error`); help remains clean
and does not read config or authorize. Standard OS variables such as `HOME` and
`XDG_CACHE_HOME` keep their normal names.

### OAuth callback port

Set `oauth_port: 9000` in YAML or use a nonempty environment override:

```sh
SPF_OAUTH_PORT=9000 mise run run
# With a custom config path (spaces and .yml are supported):
SPF_CONFIG='/path/to/custom.yml' SPF_OAUTH_PORT=9000 mise run run
```

`SPF_OAUTH_PORT` takes precedence over YAML; an empty value leaves YAML unchanged.
The default is 8080. Invalid values (including zero or ports above 65535) stop
before login or playlist changes, with no configured value echoed in the error.
Only the port is configurable: the listener remains `127.0.0.1` and the redirect
is always `http://127.0.0.1:PORT/callback`. Register that chosen URI in your Spotify
Developer dashboard **before first login** and open the printed URL in a browser
on the same host. Changing ports does not invalidate healthy cached OAuth
authorization; cached runs open no callback listener.

### Persistent OAuth cache

The default directory is `os.UserCacheDir()/spotify-playlist-filler` (normally
`$XDG_CACHE_HOME/spotify-playlist-filler` or `~/.cache/spotify-playlist-filler` on
Linux, `~/Library/Caches/spotify-playlist-filler` on macOS and
`%LocalAppData%/spotify-playlist-filler` on Windows). `SPF_TOKEN_CACHE` overrides
this **directory**, not an individual file; an empty value selects the default:

```sh
export SPF_TOKEN_CACHE="$HOME/.local/state/spf-oauth"
./bin/spotify-playlist-filler -config config.yaml
```

Each `<sha256>.spf-token.json` file is keyed by application client ID and the
canonical requested scope set. Its explicit versioned metadata records those
values, not the client secret. Metadata describes this application's authorization
request, not verified token claims or a decoded JWT. The cache contains access and
refresh tokens, their type and the access-token expiry. `SPF_SPOTIFY_SECRET` is still
required by the existing configuration contract and used for OAuth, but is **never
persisted in the cache**. Tokens and OAuth response bodies are not logged.

**One account per client/scope cache.** To switch accounts, stop running SPF, remove
that cache's `*.spf-token.json` file and authorize again, or select a separate
`SPF_TOKEN_CACHE` directory. Removing a local cache does not revoke the application
on Spotify; use your Spotify account's app settings to revoke access. Run only one
CLI process per cache at a time: requests within one process serialize refresh,
but this is not an interprocess credential store or distributed lock.

Files are **plaintext, not encrypted**. On Unix the dedicated directory must be
0700 and the token file 0600; newly created files use these modes and atomic
same-directory replacement after syncing the temporary file. Existing insecure
permissions, symlinks (including ancestor symlinks), non-regular token files and
non-sticky group/world-writable ancestors are rejected rather than repaired.
Use a private location owned by the running user. Cache corruption or filesystem
errors stop the run; fix permissions or deliberately remove the bad cache instead
of expecting a silent fallback. Do not place caches in shared directories, source
control, backups accessible to other users or container build contexts. Generated
cache and temporary filenames are excluded by `.gitignore` and `.dockerignore`.
Windows modes do not enforce Unix privacy: secure the directory with user-only
Windows ACLs yourself. Network filesystems and Windows do not provide identical
Unix rename/permission guarantees; use a local private filesystem.

Refresh responses may rotate the refresh token; the replacement is persisted
before an authenticated request is allowed. If no replacement is returned, the
previous refresh token is retained. A persistence failure is surfaced and stops
further requests in that process; there is no silent in-memory-only success.
Transport failures, HTTP 5xx and other non-`invalid_grant` refresh errors stop the
run without deleting the cache or prompting for login; retry the command later.
OAuth token-endpoint redirects are refused and exchanges/refreshes are bounded
by a 30-second HTTP timeout and the application's cancellation context.

Spotify's [refresh guide](https://developer.spotify.com/documentation/web-api/tutorials/refreshing-tokens)
and [June 18, 2026 announcement](https://developer.spotify.com/blog/2026-06-18-refresh-token-expiration)
describe a **six-month refresh-token lifetime from the original authorization**;
refreshing access tokens does not extend it. This applies to existing apps from
July 20, 2026. SPF relies on Spotify's definitive HTTP 400 `invalid_grant` response,
not guessed token issuance dates: it discards the cache before reauthorization.
During API processing, HTTP 401 also invalidates cached authorization and stops
processing; reauthorization happens on the next invocation, never by retrying a
playlist mutation. Reads that fail authorization abort before that playlist's
writes. A mutation that itself fails may already have been applied: inspect the
playlist before rerunning. HTTP 403 alone is not treated as token revocation.

#### Docker persistence and limits

The release Dockerfile defines a minimal image. When using that image,
authorize with the native CLI first, then bind-mount the same private directory at
runtime and set `SPF_TOKEN_CACHE` to its container path. For example, add these
options to your image's normal `docker run` command:

```sh
--user "$(id -u):$(id -g)" \
--mount "type=bind,src=$HOME/.cache/spotify-playlist-filler,dst=/var/lib/spf-tokens" \
-e SPF_TOKEN_CACHE=/var/lib/spf-tokens
```

The host directory must already exist with mode 0700 and be readable/writable by
the container's runtime UID; pass config and OAuth credentials at runtime, never
through Dockerfile `ARG`, `ENV` or `COPY`. A private initialized named volume also
works, but its mounted cache directory must be owned by that UID with mode 0700
(default 0755 volume roots are rejected). An ephemeral container without a volume
loses its login on removal. Cached runs do not need a published callback port.
When reauthorization is required, run the native CLI again against this same cache
with the container stopped. Ordinary port publishing does not make the
container's loopback-only callback accessible from a host browser; this change
does not add a remote/headless login flow. Docker Desktop bind mounts may not
preserve Unix permissions; use a suitably protected native volume instead. No
Docker runtime or real Spotify account verification was performed for this change.

### Persistent catalogue cache and quota recovery

Catalogue persistence is **enabled by default**, with a **24-hour TTL per page**.
`cache_ttl` / `SPF_CACHE_TTL` accepts a Go duration from `0s` to `720h`; invalid,
negative, null, numeric YAML or explicitly empty environment values are rejected.
Set **`cache_ttl: 0s`** or **`SPF_CACHE_TTL=0s`** to disable both catalogue persistence
and saved cooldowns. Schema/pagination validation and bounded GET retries remain.
`cache_directory` / `SPF_CACHE_DIRECTORY` selects a dedicated directory; explicitly
empty paths are invalid. Without it, SPF uses
`os.UserCacheDir()/spotify-playlist-filler/catalogue`, separate from OAuth records.
An explicitly disabled cache does not access its directory.

Only **artist-album, album-track and artist-search pages** are cached. Successful
empty results are valid checkpoints. Playlist `/items` reads and mutations,
`CurrentUser` and OAuth are **never cached**. Every launch still authenticates,
reads the current user and reads the current playlist. Keys hash the application,
user, current user's country, API origin, SDK-default market behavior and exact
request path/query (including pagination/settings). Neither Authorization headers
nor bearer tokens enter keys, records or logs. Records retain only synchronization
fields: IDs, artist names needed for exact search, counts and validated next URLs.
These are private plaintext catalogue data, not an encrypted credential store.

Each successful page is saved independently and atomically, with a random
page-generation ID and a binding to its predecessor's generation. Before using
any cached page, SPF validates and pins the **entire entity's complete chain**
in memory (bounded to 4,096 pages / 64 MiB). Only a fully valid, still-fresh chain
is reused, with **zero remote catalogue GETs**. If any page is missing, expired,
corrupt, incomplete or generation-incompatible, **all pages of that entity are
fetched live in the same invocation**; cached prefixes are never mixed with live
tails. There are no per-page generation-change errors or manual relaunches.

After a later-page failure or HTTP 429, **other completed entities** (artist album
lists and completed albums) remain reusable. The incomplete entity is refetched
from its first page on retry: **missing-page-only resume is not supported**.
Random predecessor bindings prevent old tails or interleaved concurrent writers
from completing a new partial chain; such chains are misses, potentially requiring
extra reads. Pinned cached bodies cannot change underneath the traversal.
**A page is not a complete catalogue**: mandatory integer counts, request-matching
offsets/limits, unchanged query settings, contiguous progression, stable totals
and terminal completeness remain mandatory before any playlist write.
Search may return the first validated exact name match without reading later
pages, but an incomplete search chain is not reused on the next invocation.
Invalid/truncated/version-mismatched records are misses with sanitized warnings;
expired records are misses, never stale fallback. Error responses are not
catalogue entries. Completion is established by validating the entire bound
chain, not by a separate manifest. This is bounded-age entity reuse, **not a
Spotify snapshot guarantee**: Spotify may change during an all-live traversal.
Deleting this dedicated directory with SPF stopped forces fresh reads.

Unix directories must be 0700 and files 0600. Symlinks (including ancestors),
`..` path components, non-regular target files and insecure permissions fail
closed. New files use private modes, same-directory temporary writes, fsync and
atomic rename through `os.Root`. Use a private local filesystem; Windows users
must enforce user-only ACLs. Entries/responses are bounded to 128 KiB, and disk
writes are capped at 4,096 records / 64 MiB per dedicated directory. No automatic
pruning is performed: when full or another process holds the write lock, validated
fresh reads remain usable but persistence is skipped with `storage_unavailable`.
Other unsafe-path/I/O failures stop processing before playlist writes. After a
crash, stop all SPF processes before removing a leftover `.spf-catalogue-lock`
directory. Separate atomic files cannot lose unrelated concurrent checkpoints;
this is not a distributed single-flight cache or cross-process quota scheduler.
The OAuth cache still requires only one CLI process per authorization cache.

Final HTTP 429 diagnostics expose only allowlisted `reason=QUOTA_EXCEEDED`, valid
integer `retry_after_seconds`, and UTC `retry_at` (when supplied by Spotify).
Arbitrary reasons/messages/bodies are never logged. A catalogue 429 with a delay
**above 30 seconds and at most 30 days** also saves a namespaced cooldown, independent
of page TTL. On restart complete fresh cached entities are served **before** checking cooldown;
only required remote catalogue reads are blocked until that timestamp, immediately
returning a safe 429 rather than sleeping for hours. Missing/invalid Retry-After
or delays above 30 days are not persisted or guessed. Clearing/disabling the cache
also clears/bypasses that protection. It does not cover other applications, users,
other cache directories, OAuth, current-user reads or playlist reads; those still
contact Spotify. It cannot change Spotify's application quota or grant API access.

Catalogue requests use the current documented maxima: **10** for
[artist albums](https://developer.spotify.com/documentation/web-api/reference/get-an-artists-albums)
and [search](https://developer.spotify.com/documentation/web-api/reference/search),
**50** for [album tracks](https://developer.spotify.com/documentation/web-api/reference/get-an-albums-tracks).
Pagination, worker concurrency **3**, the shared per-run album cache, deterministic
merging and the 10,000-item preflight are unchanged. No removed batch endpoints or
replacement applications are used.

#### Docker catalogue persistence

The release Dockerfile uses `scratch` and defaults to UID/GID 65532. At runtime,
mount a private, persistent host directory owned by the selected runtime UID and
set explicit paths (a scratch image need not have an OS cache home):

```sh
--user "$(id -u):$(id -g)" \
--mount "type=bind,src=$HOME/.cache/spotify-playlist-filler,dst=/var/lib/spf" \
-e SPF_TOKEN_CACHE=/var/lib/spf \
-e SPF_CACHE_DIRECTORY=/var/lib/spf/catalogue
```

Initialize the host directory with mode 0700; pass config and credentials only at
runtime. Native authorization can populate the OAuth cache first, as described
above. The catalogue subdirectory is created privately. Removing an ephemeral
container without a volume loses checkpoints and cooldown state. No cache data
belongs in image layers or source control: `.dockerignore` remains deny-by-default
and `.gitignore` excludes generated catalogue records/temporary names. No live
Spotify or Docker runtime test is claimed.

### Synchronization behavior and safety

- `read_concurrency` defaults to **3**, accepts YAML integers from **1 to 8**, and
  has a nonempty `SPF_READ_CONCURRENCY` environment override. Invalid values fail
  before login. Set **1** for sequential reads. A fixed artist worker pool makes
  at most that many catalogue GETs at once; album reads are not nested workers.
  Overlapping artists share one album read. Results merge in configuration order,
  independent of completion order, with global exclusions and album/track dedup.
  First read failure or Ctrl+C cancels and joins all workers before returning;
  that playlist has no writes. Logs may interleave at higher concurrency, and
  per-artist album completion counts include reused shared reads. Concurrency is
  not a fixed request quota or a guarantee of proportional speedup.
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
- Preflight uses the [10,000-song playlist limit](https://community.spotify.com/t5/FAQs/Is-there-a-save-limit-on-Spotify/ta-p/1215217),
  not the current playlist's observed size. Before any removal/addition (including
  shuffle), check unique desired tracks and the projected final item count,
  including preserved duplicates and unsupported entries. Exactly 10,000 is
  allowed. Overflow emits a count-only WARN and skips that playlist unchanged;
  later playlists continue, and the CLI exits nonzero after capacity skips.
  Nothing is truncated, split or created. The production adapter retains raw
  counts from the same paginated read. Legacy Catalog implementations without
  that optional count expose `total_known=false` and only a supported-track lower
  bound; known overflow still skips, but a precise total cannot be guaranteed.
  Concurrent external playlist edits remain outside this preflight guarantee.
- Mutations use batches of at most 100 track IDs. Stop on the first error; do not
  retry writes automatically, since a failed response can still represent a
  completed operation. Only GET rate limits have bounded retries, described below.
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
internal/spotifyapi/         Playlist Items HTTP adapter, catalogue SDK and pagination
internal/spotifyauth/        interactive OAuth and loopback callback lifecycle
```

The module path is `github.com/QuentinBtd/spotify-playlist-filler`.

### Spotify Playlist Items API contract

Playlist reads and writes use **`/playlists/{playlist_id}/items`**, following
Spotify's [February 2026 migration guide](https://developer.spotify.com/documentation/web-api/tutorials/february-2026-migration-guide):

- [GET Playlist Items](https://developer.spotify.com/documentation/web-api/reference/get-playlists-items)
  decodes `items[].item` (not the legacy `items[].track`), checks the item's
  `type`, and follows the returned `next` URL until it is null. Only
  non-local music tracks with an ID participate in synchronization; null items,
  episodes and unknown item types are left untouched. If any page fails, no
  partial playlist-track list is returned. Page objects require a non-null `items`
  array, an explicit `item` field in each entry, and a `next` URL or null.
  Malformed pages, trailing JSON and pagination cycles fail before writes.
- [POST Add Items](https://developer.spotify.com/documentation/web-api/reference/add-items-to-playlist)
  sends `{"uris":["spotify:track:<id>"]}` and expects HTTP 201. No `position`
  is supplied, so tracks append in batch order.
- [DELETE Remove Items](https://developer.spotify.com/documentation/web-api/reference/remove-items-playlist)
  sends `{"items":[{"uri":"spotify:track:<id>"}]}` and expects HTTP 200.
  No positions or snapshot constraint are supplied, preserving removal of all
  occurrences of the specified obsolete track IDs.

Both mutations are limited to 100 track IDs per request; application batching
is unchanged. There is **no automatic fallback to `/tracks` and no mutation
retry**, including for HTTP 429. A shared per-client GET-only layer follows
Spotify's [rate-limit guidance](https://developer.spotify.com/documentation/web-api/concepts/rate-limits):
at most two retries after HTTP 429 with integer `Retry-After` seconds, a one-second
floor for zero, and a cancellable 30-second cooldown-wait budget per request.
Missing/invalid headers or delays above that budget are surfaced without retry,
never shortened; a valid longer delay still blocks other GETs within their wait
budgets. After a cooldown a single recovery probe prevents workers stampeding.
WARN logs contain waiting seconds only. SDK retries remain disabled. Other HTTP
statuses and transport errors are not retried. Spotify HTTP errors retain their
status and message; context cancellation and the authenticated client's 30-second
timeout apply to playlist requests as well as catalogue requests. No numeric
requests-per-second quota is assumed.

The logged-in user must own the playlist or be a collaborator; GET may return
403 otherwise. Existing OAuth scopes for private/collaborative reads and
public/private modifications are retained. Developer-app access restrictions
still apply; changing the route does not grant account or playlist access.

The existing SDK (`github.com/zmb3/spotify/v2 v2.2.0`) remains responsible for
OAuth, current-user, artist, album and search operations. Its playlist methods
still use the legacy contract, so the small Playlist Items adapter shares the
same guarded HTTP client rather than rewriting SDK URLs or responses. The
client is a shallow clone of the authenticated client, preserving its OAuth
transport, timeout and redirect policy without mutating the supplied client.
Before OAuth runs, requests and redirects are confined to the configured API
origin (`https://api.spotify.com` in production); Playlist Items pagination and
redirects must also keep the same playlist `/items` path. Safe relative `next`
URLs are resolved without rewriting their query; redirects are bounded at ten.
This is a playlist-endpoint migration, not a claim that every SDK operation is
compatible with all current Development Mode restrictions. Other catalogue or
search restrictions need separate verification with your authorized app.

Tests use in-memory catalogues and synthetic local HTTP fixtures, never a real
playlist. **No live Spotify account/API verification has been performed for this
migration**; access and real-account behavior must be verified separately.

## Development and builds

All tools and task implementations are centralized in `.mise.toml`; Go is the
only managed tool (its standard tools include `gofmt` and `go vet`). There is no
separate Makefile or cross-build script to keep in sync.

```sh
mise tasks           # list tasks and descriptions
mise run help        # same list; mise run also shows it by default
mise run fmt         # format packages; report changed files or already formatted
mise run test        # go test ./...
mise run vet         # go vet ./...
mise run check       # tests, vet and fail-safe gofmt check; does not modify files
mise run race        # go test -race ./...; requires system C compiler
mise run build       # bin/spotify-playlist-filler
mise run build-all   # six cross-builds under build/
mise run clean       # remove only bin/, build/ and coverage.out
mise run run         # default config.yaml (legacy config.yml fallback); may start login
```

`mise run exec` is an alias for `mise run run`. Override the config path with
`SPF_CONFIG=/path/to/config.yaml mise run run` (or any existing `.yml` path).
With `SPF_CONFIG` and its deprecated fallback unset or empty, the CLI applies its normal default/fallback and
accepts `mise run run -- -config PATH` or `mise run run -- PATH`.
Extra CLI arguments are forwarded:
`mise run run -- --help` prints usage without reading configuration or contacting
Spotify. For other direct Go commands use `mise exec -- go ...` to select the
pinned toolchain without shell activation; the former `GO` override is removed.
Tasks always run at the configuration/repository root, even from a subdirectory.

Cross-builds use `VERSION` in their names and `CGO_ENABLED=0`, with `-trimpath`:
`build/spotify-playlist-filler-<VERSION>-<os>-<arch>` (plus `.exe` on Windows).
Targets are Linux, macOS (`darwin`) and Windows, each for amd64 and arm64.
An optional filename prefix is accepted: `mise run build-all custom-name`.
The package being compiled remains `./cmd/spotify-playlist-filler`; invalid names
or more than one argument are rejected. `clean` never removes source files,
configuration, dependencies or the global Go cache.

### Migration from Make and the shell build script

Replace former `make <target>` commands with `mise run <target>` after
`mise trust .mise.toml` and `mise install`. Replace `bash build.sh [binary-name]`
with `mise run build-all [binary-name]`; both old files are removed. Existing
`CONFIG` usage remains a deprecated fallback; the `exec` alias remains supported. No GitHub Actions workflow
is added by this migration; CI automation is deferred.

## Migration from the original layout

- `src/main.go` is replaced by `cmd/spotify-playlist-filler`; use
  `go run ./cmd/spotify-playlist-filler`, not `go run src/main.go`.
- The formerly committed root binary is removed; use `mise run build` and `bin/`.
- Both `config.yaml` and legacy `config.yml` are local ignored files. Copy
  `config.example.yaml` to `config.yaml` on a fresh checkout; existing `config.yml`
  still works automatically when `config.yaml` is absent. Playlist YAML keys
  including `uri` and `playlists` are unchanged; credential keys are now lowercase.
- The old README's `PLAYLISTS_TO_FILL` key was never implemented. Use `playlists`;
  strict validation now reports that incorrect key rather than ignoring it.
- Config flags are now actually parsed; `-config PATH` and a positional path work.
- Change the registered OAuth callback from `http://localhost:8080/callback` to
  `http://127.0.0.1:8080/callback` before logging in.
- Read/config failures no longer continue silently. Missing artists stop processing
  instead of potentially deleting that artist's tracks. Infinite retries are removed.
- Go 1.26 is the declared minimum, retained from the modernization work in progress.
