package search

import (
	"context"
	"fmt"
	"log"
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
	MaxResults     = 100
)

type Result struct {
	Video   store.Video
	Results []ResultLine
}

// Channel retrieves all the videos for the given channel, calling Video on each of them.
// The results are sorted based on the published time of the video.
func Channel(ctx context.Context, ch *store.Channel, query string) (res []Result, err error) {
	videos, err := Queries.VideosOfChannel(ctx, ch.ID)
	if err != nil {
		return nil, fmt.Errorf("retrieving channel videos: %w", err)
	}

    rQuery := []rune(stem.StemLine(query))

	log.Printf("[INFO]: searching through %d videos", len(videos))
	var group errgroup.Group
	group.SetLimit(SearchRoutines)
	var mu sync.Mutex
	for _, vid := range videos {
		vid := vid
		group.Go(func() error {
			results, err := Video(&vid, rQuery)
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
				Results: results,
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

	return res, nil
}

type ResultLine struct {
	Text    string
	Stemmed string
	Start   int
}

// TODO: update comment.
//
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
func Video(vid *store.Video, query []rune) (res []ResultLine, err error) {
	var lines []ResultLine
	var inMeta bool
	var metaStart int
	var metaEnd int
	var currStart int
	for i, ch := range vid.SearchableTranscript {
		if ch == '~' {
			if inMeta {
				inMeta = false

				metaEnd = i
				start, err := strconv.Atoi(vid.SearchableTranscript[metaStart:metaEnd])
				if err != nil {
					return nil, fmt.Errorf("parsing start time: %w", err)
				}
				currStart = start
			} else {
				if i > 1 {
					txt := vid.SearchableTranscript[metaEnd+1 : i]
					stemmed := stem.StemLine(txt)
					lines = append(lines, ResultLine{
						Text:    txt,
						Stemmed: stemmed,
						Start:   currStart,
					})
				}

				inMeta = true
				metaStart = i + 1
			}
			continue
		}
	}

	var matching int
	for _, line := range lines {
		for _, ch := range line.Stemmed {
			if matching == len(query) {
				res = append(res, line)
				matching = 0
			}

			if query[matching] == ch {
				matching++
			} else {
				matching = 0
			}
		}

        if query[matching] == ' ' {
            matching++
        } else {
            matching = 0
        }
	}

	return res, nil
}
