import spotipy
from spotipy.oauth2 import SpotifyOAuth
from logger.logger import Logger
from configuration.configuration import Configuration

playlistsToFill = Configuration().get("PLAYLISTS_TO_FILL")

logger = Logger("Spotify-Playlist-Filler")

authCache = '.spotipyoauthcache'
authScope = 'user-library-read playlist-read-private playlist-modify-private playlist-read-collaborative playlist-modify-public' 
SPOTIPY_REDIRECT_URI = 'http://localhost:8080'

spotify = spotipy.Spotify(auth_manager=SpotifyOAuth(redirect_uri=SPOTIPY_REDIRECT_URI,scope=authScope, cache_path=authCache))

for playlist in playlistsToFill:
    logger.info("Filling playlist: " + playlist['name'])
    playlist_uri = playlist['uri']
    tracks_uris_list = []
    ### Get the current playlist tracks
    offset = 0
    remote_playlist_items = spotify.playlist_items(playlist_uri,limit=100,offset=offset)
    remote_playlist_tracks_uris = []
    less_100_tracks = False
    while not less_100_tracks:
        if len(remote_playlist_items['items']) < 100:
            less_100_tracks = True
        for track in remote_playlist_items['items']:
            remote_playlist_tracks_uris.append(track['track']['uri'])
        offset=offset+100
        remote_playlist_items = spotify.playlist_items(playlist_uri,limit=100,offset=offset)
    logger.debug("There are %s tracks in playlist" % (len(remote_playlist_tracks_uris)))
    ### Add the tracks to the playlist
    albums_added = []
    for artist in playlist['artists']:
        if artist['use_name_instead_of_uri']:
            results = spotify.search(q='artist:' + artist['name'], type='artist')
            items = results['artists']['items']
            artist_uri = items[0]['uri']
            logger.debug("Searching by artist name")
        else:
            artist_uri = artist['uri']

        ignored_albums_list = []
        if "ignored_albums" in artist:
            for ignored_album in artist['ignored_albums']:
                ignored_albums_list.append(ignored_album['uri'])
                logger.debug("Ignoring %s album" % (ignored_album['uri']))

        results = spotify.artist_albums(artist_uri, album_type='album')
        albums = results['items']
        while results['next']:
            results = spotify.next(results)
            albums.extend(results['items'])
        i = 0
        for album in albums:
            album_uri = album['uri']
            album_name = album['name']
            if album_uri not in albums_added:
                albums_added.append(album_uri)
                if album_uri in ignored_albums_list:
                    logger.debug("Ignored album : %s " % album_name)
                for track in spotify.album_tracks(album_uri)['items']:
                    track_uri = track['uri']
                    track_name = track['name']
                    if track_uri in remote_playlist_tracks_uris:
                        logger.debug("Ignoring track %s : already in playlist" % track_name)
                    else:
                        logger.debug("Adding track %s to playlist" % track_name)
                        tracks_uris_list.append(track_uri)
                        remote_playlist_tracks_uris.append(track_uri)
            else:
                logger.info("Ignoring album %s : already in playlist" % album_name)
    i = 0
    iMax=99
    print(len(tracks_uris_list))
    while i < len(tracks_uris_list):
        if iMax > len(tracks_uris_list):
            iMax = len(tracks_uris_list)-1
        #print(tracks_uris_list[i:iMax])
        tracks_uris_to_add = tracks_uris_list[i:iMax]
        spotify.playlist_add_items(playlist_uri, tracks_uris_to_add)
        i=i+100
        iMax=iMax+100
    # if len(tracks_uris_list) > 0:
    #     spotify.playlist_add_items(playlist_uri, tracks_uris_list)
