package app

import (
	"fmt"
	"github.com/zmb3/spotify/v2"
	"reflect"
	"testing"
)

func TestTrackPlanDeduplicatesAndDiffs(t *testing.T) {
	current := []spotify.ID{"a", "b", "b"}
	desired := []spotify.ID{"b", "c", "c", ""}
	remove, add := planTracks(current, desired, false)
	if !reflect.DeepEqual(remove, []spotify.ID{"a"}) || !reflect.DeepEqual(add, []spotify.ID{"c"}) {
		t.Fatalf("remove=%v add=%v", remove, add)
	}
	remove, add = planTracks(current, desired, true)
	if !reflect.DeepEqual(remove, []spotify.ID{"a", "b"}) || len(add) != 2 {
		t.Fatalf("shuffle remove=%v add=%v", remove, add)
	}
	seen := map[spotify.ID]bool{}
	for _, id := range add {
		seen[id] = true
	}
	if !seen["b"] || !seen["c"] {
		t.Fatalf("shuffle changed tracks: %v", add)
	}
	if !reflect.DeepEqual(current, []spotify.ID{"a", "b", "b"}) || !reflect.DeepEqual(desired, []spotify.ID{"b", "c", "c", ""}) {
		t.Fatal("mutated inputs")
	}
}

func TestBatchBoundaries(t *testing.T) {
	for _, n := range []int{0, 1, 99, 100, 101, 200, 201} {
		ids := make([]spotify.ID, n)
		for i := range ids {
			ids[i] = spotify.ID(fmt.Sprint(i))
		}
		batches := trackBatches(ids)
		var joined []spotify.ID
		for _, batch := range batches {
			if len(batch) == 0 || len(batch) > 100 {
				t.Fatalf("n=%d size=%d", n, len(batch))
			}
			joined = append(joined, batch...)
		}
		if len(batches) != (n+99)/100 || len(joined) != n {
			t.Fatalf("n=%d batches=%d tracks=%d", n, len(batches), len(joined))
		}
		if n > 0 && !reflect.DeepEqual(joined, ids) {
			t.Fatal("order changed")
		}
	}
}
