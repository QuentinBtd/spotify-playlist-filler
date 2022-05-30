# Spotify Playlist Filler

## How to use it 

### 1. Create config file

In `config.yml` file, you can set the following parameters:

```yaml
```yaml
SPOTIFY_SECRET: "Your Spotify Secret"
SPOTIFY_ID: "Your Spotify ID"
PLAYLISTS_TO_FILL:
  - name: "Playlist Filler Test"
    uri: 3RiBOmtagQlYUd2XOeWPUd
    artists:
      - name: "Rick Astley" # Optional, except if you use 'use_name_instead_of_uri: true'
        uri: 0gxyHStUsqpMadRV0Di1Qt
        use_name_instead_of_uri: false 
        albums_to_skip: # Optional
          - name: "Beautiful Life"
            uri: 3IqiZzsC1gef7qgvCXTqTj
      - name: "Imagine Dragons" # Optional, except if you use 'use_name_instead_of_uri: true'
        use_name_instead_of_uri: true # Will use the artist's name instead of the artist's uri by searching it on Spotify
    albums_to_skip: # You can set almbums to skip here
      - name: "Rick Astley - 50"
        uri: 7IW3NEq3Fxtm7FhOcosnBy
verbose: false
```

`SPOTIFY_ID` and `SPOTIFY_SECRET` can be set in environment variables.

`SPF_VERBOSE` environment variable is the equivalent of `verbose` parameter.

### 2. Run the tool

```shell
./spotify-playlist-filler config.yml
```

### Roadmap
- [ ] Add check of config file
- [ ] Add check of Spotify credentials
- [ ] Add check of Spotify playlist access
- [ ] Add the possibility to load config file from a file with path (using [Viper](https://github.com/spf13/viper))
- [ ] Use `verbose` option (do nothing for now)
- [ ] Add the possibility to add albums and tracks to a playlist without using artist name
- [ ] Clean code
- [ ] Improve documentation
...

