package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"io/ioutil"
	"strconv"
	"context"
	"net/http"
	"math/rand"
	"time"
	"strings"

	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"gopkg.in/yaml.v2"
	"golang.org/x/oauth2/clientcredentials"
)

type Config struct {
	Verbose					bool				`yaml:"verbose"`
	SpotifyId				string				`yaml:"SPOTIFY_ID"`
	SpotifySecret			string				`yaml:"SPOTIFY_SECRET"`
	PlaylistsToFill			[]PlaylistsToFill	`yaml:"playlists"`
}

type PlaylistsToFill struct {
	Name					string				`yaml:"name"`
	Uri						spotify.ID			`yaml:"uri"`
	ShuffleOrder			bool				`yaml:"shuffle_order"`
	Artists					[]Artist			`yaml:"artists"`
	SkippedAlbums			[]SkippedAlbums		`yaml:"albums_to_skip"`
}

type Artist struct {
	Name					string				`yaml:"name"`
	Uri						spotify.ID			`yaml:"uri"`
	SkippedAlbums	  	  	[]SkippedAlbums		`yaml:"albums_to_skip"`
	UseNameInsteadOfUri		bool				`yaml:"use_name_instead_of_uri"`
}

type SkippedAlbums struct {
	Name 					string 				`yaml:"name"`
	Uri  					spotify.ID 			`yaml:"uri"`
}

type ItemInfos struct {
	Uri						spotify.ID
	Name					string
}

type PackOf100 struct {
	Pack 					[]ItemInfos
}

const redirectURI = "http://localhost:8080/callback"

var (
	config Config
	auth  = spotifyauth.New(spotifyauth.WithRedirectURL(redirectURI), spotifyauth.WithScopes(spotifyauth.ScopeUserReadPrivate, spotifyauth.ScopePlaylistModifyPublic, spotifyauth.ScopePlaylistModifyPrivate, spotifyauth.ScopeUserLibraryRead, spotifyauth.ScopeUserLibraryModify))
	ch    = make(chan *spotify.Client)
	state = RandomString(32)
	SkippedAlbumsIdList []ItemInfos
)


func main() {
	// first start an HTTP server
	http.HandleFunc("/callback", completeAuth)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Got request for:", r.URL.String())
	})
	go func() {
		err := http.ListenAndServe(":8080", nil)
		if err != nil {
			log.Fatal(err)
		}
	}()

	url := auth.AuthURL(state)
	fmt.Println("Please log in to Spotify by visiting the following page in your browser:", url)

	// wait for auth to complete
	client := <-ch

	// use the client to make calls that require authorization
	user, err := client.CurrentUser(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("You are logged in as:", user.ID)

	// Load config
	configPath := flag.String("config", "config.yml", "Config path")
	yamlFile, err := ioutil.ReadFile(*configPath)
	if err != nil {
		log.Printf("yamlFile.Get err   #%v ", err)
	}
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		log.Fatalf("Unmarshal: %v", err)
	}

	log.Printf("ID %s", config.SpotifyId)

	envSpotifyId := os.Getenv("SPOTIFY_ID")
	envSpotifySecret := os.Getenv("SPOTIFY_SECRET")
	envSpfVerbose := os.Getenv("SPF_VERBOSE")

	if envSpotifyId != "" {
		config.SpotifyId = envSpotifyId
	}
	if envSpotifySecret != "" {
		config.SpotifySecret = envSpotifySecret
	}
	if envSpfVerbose != "" {
		config.Verbose, _ = strconv.ParseBool(envSpfVerbose)
	}


	// ---

	// ctx := context.Background()
	credentialsConfig := &clientcredentials.Config{
		ClientID:     config.SpotifyId,
		ClientSecret: config.SpotifySecret,
		TokenURL:     spotifyauth.TokenURL,
	}
	
	log.Printf("%s", credentialsConfig.ClientID)
	if err != nil {
		log.Fatalf("couldn't get token: %v", err)
	}


	for _, playlist := range config.PlaylistsToFill{
		log.Printf("Processing playlist \"%s\" with uri \"%s\"", playlist.Name, playlist.Uri)
		SkippedAlbumsIdList = getAlbumsToSkip(playlist)
		currentTracksList, _ := getPlaylistCurrentTracks(client, playlist.Uri)
		newTracksList, _ := getTracksToAdd(client, playlist)
		tracksToRemove := getTracksToRemove(currentTracksList, newTracksList)
		if playlist.ShuffleOrder {
			log.Printf("Found %d tracks to add and %d tracks to remove", len(newTracksList), len(tracksToRemove))
			log.Printf("Shuffling tracks is enabled, all tracks will be removed, then added in shuffled order")
			removeTracks(client, playlist.Uri, currentTracksList)
			newTracksList = shuffleTracks(newTracksList)
		} else {
			newTracksList = cleanTracksToAdd(currentTracksList, newTracksList)
			log.Printf("Found %d tracks to add and %d tracks to remove", len(newTracksList), len(tracksToRemove))
			removeTracks(client, playlist.Uri, tracksToRemove)
		}
		addTracks(client, playlist.Uri, newTracksList)
	}
}

func getPlaylistCurrentTracks(client *spotify.Client, playlistUri spotify.ID) ([]ItemInfos, error) {
	ctx := context.Background()

	log.Printf("Getting tracks already in playlist %s", playlistUri)

	items, err := client.GetPlaylistItems(ctx, playlistUri)
	if err != nil {
		log.Fatalf("couldn't get playlist items: %v", err)
		return nil, err
	}

	var tracksList []ItemInfos

	for page := 1; ; page++ {
		try := 0
		// log.Printf("  Page %d has %d tracks", page, len(items.Items))
		for _, track := range items.Items {
			tracksList = append(tracksList,
				ItemInfos{
					Uri:	fromUriToID(track.Track.Track.URI),
					Name:	track.Track.Track.Name,
				},
			)
		}
		loop:
		err = client.NextPage(ctx, items)
		if err == spotify.ErrNoMorePages {
			break
		}
		if err != nil {
			try = +1
			log.Printf("%s", err)
			if try > 3 {
				return nil, err
			}
			log.Printf("Waiting for 30 seconds")
			time.Sleep(30 * time.Second)
			goto loop
		}
	}

	return tracksList, nil
}

func getAlbumsTracks(client *spotify.Client, albumUri spotify.ID) ([]ItemInfos, error) {
	ctx := context.Background()

	// log.Printf("Getting album %s", albumUri)

	items, err := client.GetAlbumTracks(ctx, albumUri)

	var tracksList []ItemInfos

	for page := 1; ; page++ {
		for _, track := range items.Tracks {
			tracksList = append(tracksList,
				ItemInfos{
					Uri:	fromUriToID(track.URI),
					Name:	track.Name,
				},
			)
		}
		loop:
		err = client.NextPage(ctx, items)
		if err == spotify.ErrNoMorePages {
			break
		}
		if err != nil {
			log.Printf("%s", err)
			log.Printf("Waiting for 30 seconds")
			time.Sleep(30 * time.Second)
			goto loop
		}
	}
	return tracksList, nil
}

func getTracksToAdd(client *spotify.Client, playlist PlaylistsToFill) ([]ItemInfos, error) {
	ctx := context.Background()
	var tracksList []ItemInfos

	artists := playlist.Artists

	log.Printf("Getting tracks to add...")

	for _, artist := range artists {
		var infoToShow string
		if artist.Name != "" {
			infoToShow = artist.Name
		} else {
			infoToShow = string(artist.Uri)
		}
		log.Printf("Getting artist \"%s\" albums...", infoToShow)
		var artistUri spotify.ID
		if artist.UseNameInsteadOfUri {
			artistUri = searchArtistUri(client, artist.Name)
			if artistUri == "" {
				log.Printf("Couldn't find artist \"%s\"", artist.Name)
				continue
			}
		} else {
			artistUri = artist.Uri
		}
		albums, err := client.GetArtistAlbums(ctx, artistUri, nil)
		if err != nil {
			log.Fatal(err)
		}
		for _, album := range albums.Albums {
			if idInSlice(album.ID, SkippedAlbumsIdList) { // SkippedAlbumsIdList is global variable
				log.Printf("Album \"%s\" Skipped", album.Name)
			} else {
				_tracksList, err := getAlbumsTracks(client, album.ID)
				if err != nil {
					log.Fatal(err)
				}
				for _, track := range _tracksList {
					tracksList = append(tracksList,
						ItemInfos{
							Uri:	track.Uri,
							Name:	track.Name,
						},
					)
				}
				time.Sleep(200 * time.Millisecond)
			}
		}
	}
	return tracksList, nil
}

func getAlbumsToSkip(playlist PlaylistsToFill) ([]ItemInfos) {
	var albumsToSkip []ItemInfos
	for _, artist := range playlist.Artists {
		for _, album := range artist.SkippedAlbums {
			albumsToSkip = append(albumsToSkip, 
				ItemInfos{
					Uri:	album.Uri,
					Name:	album.Name,
				},
			)
		}
	}
	for _, album := range playlist.SkippedAlbums {
		albumsToSkip = append(albumsToSkip, 
			ItemInfos{
				Uri:	album.Uri,
				Name:	album.Name,
			},
		)
	}
	return albumsToSkip
}

func getTracksToRemove(currentTracksList []ItemInfos, newTracksList []ItemInfos) ([]ItemInfos) {

	log.Printf("Getting tracks to remove...")

	var tracksToRemove []ItemInfos
	for _, track := range currentTracksList {
		if !idInSlice(track.Uri, newTracksList) {
			tracksToRemove = append(tracksToRemove, track)
		}
	}
	return tracksToRemove
}

func removeTracks(client *spotify.Client, playlistUri spotify.ID, tracksToRemove []ItemInfos) {
	ctx := context.Background()

	log.Printf("Removing tracks from playlist %s", playlistUri)

	packs := splitByPackOf100(tracksToRemove)
	for _, pack := range packs {
		loop:
		var list []spotify.ID
		list = nil
		for _, item := range pack.Pack {
			list = append(list, spotify.ID(item.Uri))
		}
		_, err := client.RemoveTracksFromPlaylist(ctx, playlistUri, list ...)
		if err != nil {
			log.Printf("%s", err)
			log.Printf("Waiting for 30 seconds")
			time.Sleep(30 * time.Second)
			goto loop
		}
	}
}

func addTracks(client *spotify.Client, playlistUri spotify.ID, tracksToAdd []ItemInfos) {
	ctx := context.Background()

	log.Printf("Adding tracks to playlist %s", playlistUri)

	packs := splitByPackOf100(tracksToAdd)
	for _, pack := range packs {
		loop:
		var list []spotify.ID
		list = nil
		for _, item := range pack.Pack {
			list = append(list, spotify.ID(item.Uri))
		}
		_, err := client.AddTracksToPlaylist(ctx, playlistUri, list ...)
		if err != nil {
			log.Printf("%s", err)
			log.Printf("Waiting for 30 seconds")
			time.Sleep(30 * time.Second)
			goto loop
		}		
	}
}

func searchArtistUri(client *spotify.Client, artistName string) (spotify.ID) {
	ctx := context.Background()
	searchResults, err := client.Search(ctx, artistName, spotify.SearchTypeArtist)
	if err != nil {
		log.Fatal(err)
	}
	for _, item := range searchResults.Artists.Artists {
		if item.Name == artistName {
			return item.ID
		}
	}
	return ""
}

func cleanTracksToAdd(currentTracksList []ItemInfos, tracksToAdd []ItemInfos) ([]ItemInfos) {
	var cleanedTracksToAdd []ItemInfos
	for _, track := range tracksToAdd {
		if !idInSlice(track.Uri, currentTracksList) {
			cleanedTracksToAdd = append(cleanedTracksToAdd, track)
		}
	}
	return cleanedTracksToAdd
}

func shuffleTracks(tracksList []ItemInfos) ([]ItemInfos) {
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(tracksList), func(i, j int) { tracksList[i], tracksList[j] = tracksList[j], tracksList[i] })
	return tracksList
}

func idInSlice(id spotify.ID, list []ItemInfos) bool {
    for _, item := range list {
        if item.Uri == id {
            return true
        }
    }
    return false
}

func fromUriToID(uri spotify.URI) spotify.ID {
	result := strings.Split(string(uri), ":")
	return spotify.ID(spotify.ID(result[len(result)-1]))
}

func splitByPackOf100(list []ItemInfos) ([]PackOf100) {
	i := 0
	listLenght := len(list)
	var pack []ItemInfos
	var packOf100 []PackOf100
	for i < listLenght {
		n := 0
		pack = nil
		for n < 100 && i < listLenght {
			pack = append(pack,
				ItemInfos{
					Uri:	list[i].Uri,
					Name:	list[i].Name,
				},
			)
			i++
			n++
		}
		packOf100 = append(packOf100,
			PackOf100{
				Pack:	pack,
			},
		)
	}
	return packOf100
}

func completeAuth(w http.ResponseWriter, r *http.Request) {
	tok, err := auth.Token(r.Context(), state, r)
	if err != nil {
		http.Error(w, "Couldn't get token", http.StatusForbidden)
		log.Fatal(err)
	}
	if st := r.FormValue("state"); st != state {
		http.NotFound(w, r)
		log.Fatalf("State mismatch: %s != %s\n", st, state)
	}

	// use the token to get an authenticated client
	client := spotify.New(auth.Client(r.Context(), tok))
	fmt.Fprintf(w, "Login Completed!")
	ch <- client
}

func RandomString(n int) string {
    var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
 
    s := make([]rune, n)
    for i := range s {
        s[i] = letters[rand.Intn(len(letters))]
    }
    return string(s)
}
