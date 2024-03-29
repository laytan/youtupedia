package search

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/laytan/youtupedia/internal/stem"
	"github.com/laytan/youtupedia/internal/store"
	"golang.org/x/sync/errgroup"
)

var (
	Queries        *store.Queries
	SearchRoutines = 20
	MaxResults     = 100
)

type Result struct {
	Video   store.Video
	Results []store.Transcript
	ids     []int64
}

// Channel retrieves all the videos for the given channel, calling Video on each of them.
// The results are sorted based on the published time of the video.
func Channel(ctx context.Context, ch *store.Channel, query string) (res []Result, err error) {
	// Retrieves the videos that contain all the words we query.
	// These are optimistic matches, because they have to be in order,
	// and they can span the metadata boundaries, and we have to return the exact part of the transcripts.
	stemmedQuery := stem.StemLine(query)
	videos, err := Queries.VideosOfChannelWithWords(ctx, ch.ID, strings.Split(stemmedQuery, " "))
	if err != nil {
		return nil, fmt.Errorf("retrieving channel videos: %w", err)
	}

	log.Printf("[INFO]: searching through %d optimistic video matches", len(videos))
	var group errgroup.Group
	group.SetLimit(SearchRoutines)
	var mu sync.Mutex
	for _, vid := range videos {
		vid := vid
		group.Go(func() error {
			results, err := Video(&vid, stemmedQuery)
			if err != nil {
				return fmt.Errorf("searching: %w", err)
			}

			if len(results) == 0 {
				return nil
			}

			mu.Lock()
			defer mu.Unlock()

			res = append(res, Result{
				Video:   vid,
				Results: nil,
				ids:     results,
			})
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, fmt.Errorf("iterating videos: %w", err)
	}

	sort.Slice(res, func(i, j int) bool {
		return res[j].Video.PublishedAt.Before(res[i].Video.PublishedAt)
	})

	log.Printf("[INFO]: there were %d actual video matches, capping to %d", len(res), MaxResults)
	if len(res) > MaxResults {
		res = res[:MaxResults]
	}

	// Flatten all resulting transcripts into one slice of ids,
	// so we can do 1 query for all transcripts.
	all := make([]int64, 0, len(res))
	for _, r := range res {
		all = append(all, r.ids...)
	}

	log.Printf("[INFO]: retrieving %d matched captions/lines", len(all))
	ts, err := Queries.TranscriptsByIds(ctx, all)
	if err != nil {
		return nil, fmt.Errorf("querying transcripts: %w", err)
	}

	// The results from the query are in the same order of the given ids to the query.
	// So, because its ordered, we can do the following to efficiently put them back
	// with the video that they belong.
	var curr int
	for i := range res {
		res[i].Results = ts[curr : curr+len(res[i].ids)]
		curr += len(res[i].ids)
	}

	return res, nil
}

// Video searches for the query inside the video's searchable_transcript.
// Returning the IDs of the matching transcripts.
//
// Optimized to be fast, this is done in O(n) time where n is the length of the searchable_transcript.
//
// The transcript is stemmed using the stem package, so different "styles" of the same word
// will match. NOTE: you must stem the input query yourself.
//
// If the match is on the boundary of a transcript (so part is on transcript/line 1 and other part on 2),
// The second transcript's ID is returned.
func Video(vid *store.Video, query string) (res []int64, err error) {
	var inMeta bool
	var matching int
	var idStart int
	var idEnd int
	runes := []rune(query)
	for i, ch := range vid.SearchableTranscript {
		if matching == len(runes) {
			id, err := strconv.ParseInt(vid.SearchableTranscript[idStart:idEnd], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("could not parse id string: %w", err)
			}

			res = append(res, id)
			matching = 0
		}

		if ch == '~' {
			if inMeta {
				inMeta = false
				idEnd = i
			} else {
				inMeta = true
				idStart = i + 1
			}
			continue
		}

		if inMeta {
			continue
		}

		if runes[matching] == ch {
			matching++
		} else {
			matching = 0
		}
	}

	return res, nil
}
