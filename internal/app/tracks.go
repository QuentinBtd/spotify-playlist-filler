package app

import (
	"github.com/zmb3/spotify/v2"
	"math/rand/v2"
)

func uniqueTracks(ids []spotify.ID) []spotify.ID {
	seen := make(map[spotify.ID]bool, len(ids))
	var result []spotify.ID
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}

func difference(a, b []spotify.ID) []spotify.ID {
	set := make(map[spotify.ID]bool, len(b))
	for _, id := range b {
		set[id] = true
	}
	var result []spotify.ID
	for _, id := range a {
		if !set[id] {
			result = append(result, id)
		}
	}
	return result
}

// planTracks computes a complete plan before any writes. It never changes its inputs.
func planTracks(current, desired []spotify.ID, shuffle bool) (remove, add []spotify.ID) {
	current = uniqueTracks(current)
	desired = uniqueTracks(desired)
	if shuffle {
		rand.Shuffle(len(desired), func(i, j int) { desired[i], desired[j] = desired[j], desired[i] })
		return current, desired
	}
	return difference(current, desired), difference(desired, current)
}

func trackBatches(ids []spotify.ID) [][]spotify.ID {
	var result [][]spotify.ID
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		result = append(result, ids[start:end])
	}
	return result
}
