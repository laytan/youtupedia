package search

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/laytan/youtupedia/internal/stem"
	"github.com/laytan/youtupedia/internal/store"
	"golang.org/x/sync/errgroup"
)

var (
	Queries        *store.Queries
	SearchRoutines = 20
)

type Result struct {
	Video   store.Video
	Results []store.Transcript
}

// Channel retrieves all the videos for the given channel, calling Video on each of them.
// The results are sorted based on the published time of the video.
func Channel(ctx context.Context, ch *store.Channel, query string) (res []Result, err error) {
	videos, err := Queries.VideosOfChannel(
		ctx,
		ch.ID,
	) // TODO: paginate or channel that fetches in chunks if this takes much memory?
	if err != nil {
		return nil, fmt.Errorf("retrieving channel videos: %w", err)
	}

	var group errgroup.Group
	group.SetLimit(SearchRoutines)
	var mu sync.Mutex
	for _, vid := range videos {
		vid := vid
		group.Go(func() error {
			results, err := Video(&vid, query)
			if err != nil {
				return fmt.Errorf("searching: %w", err)
			}

			tsc := make([]store.Transcript, 0, len(results))
			for _, r := range results {
				ts, err := Queries.Transcript(ctx, r)
				if err != nil {
					return fmt.Errorf("fetching transcript: %w", err)
				}

				tsc = append(tsc, ts)
			}

			if len(results) > 0 {
				mu.Lock()
				defer mu.Unlock()
				res = append(res, Result{
					Video:   vid,
					Results: tsc,
				})
			}

			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, fmt.Errorf("iterating videos: %w", err)
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].Video.PublishedAt.Before(res[j].Video.PublishedAt)
	})

	return res, nil
}

// Video searches for the query inside the video's searchable_transcript.
// Returning the IDs of the matching transcripts.
//
// Optimized to be fast, this is done in O(n) time where n is the length of the searchable_transcript.
//
// The query and the transcript is stemmed using the stem package, so different "styles" of the same word
// will match.
//
// If the match is on the boundary of a transcript (so part is on transcript/line 1 and other part on 2),
// The second transcript's ID is returned.
func Video(vid *store.Video, query string) (res []int64, err error) {
	var curr int64
	var inMeta bool
	var matching int
	var ids int
	runes := []rune(stem.StemLine(query))
	for i, ch := range vid.SearchableTranscript {
		if matching == len(runes) {
			res = append(res, curr)
			matching = 0
		}

		if ch == '~' {
			if inMeta {
				inMeta = false

				id, err := strconv.ParseInt(vid.SearchableTranscript[ids+1:i], 10, 64)
				if err != nil {
					return nil, fmt.Errorf("could not parse id string: %w", err)
				}
				curr = id

				// Treat as space, because the captions would be split like that.
				if runes[matching] == ' ' {
					matching++
				} else {
					matching = 0
				}
			} else {
				inMeta = true
				ids = i
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
