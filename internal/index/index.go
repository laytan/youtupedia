package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"log"
	"strconv"
	"strings"

	"github.com/laytan/youtupedia/internal/store"
	"github.com/laytan/youtupedia/internal/tube"
	"golang.org/x/sync/errgroup"
)

var (
	Queries *store.Queries
	Yt      *tube.Client
	Db      *sql.DB

	ErrAlreadyIndexed = errors.New("already indexed")
)

// IndexChannel iterates through all videos of the given channel.
// Calling IndexVideo on each of them.
//
// If the iteration gets to a video that is already indexed, ErrAlreadyIndexed is returned.
//
// If during this process, the YouTube quota is exceeded,
// a store.Failure is created with type store.FailureTypePageQuota and the token of the failed page in its Data.
//
// Indexing is done using 2 goroutines for increased speed, this could be higher (the process is not very taxing).
// But we might get banned/blocked by YouTube.
func IndexChannel(ctx context.Context, channel *store.Channel) error {
	lastVideo, err := Queries.LastVideo(ctx, channel.ID)
	hasLastVideo := err == nil
	err = Yt.EachPlaylistItemPage(
		channel.VideosListID,
		func(pi *tube.ResPlaylistItems, token string, err error) (bool, error) {
			if err != nil {
				if errors.Is(err, tube.ErrQuotaExceeded) {
					log.Println(
						"[WARN]: quota exceeded, adding page we left off at to the failures table",
					)
					if err := Queries.CreateFailure(ctx, store.CreateFailureParams{
						ChannelID: channel.ID,
						Data:      token,
						Type:      string(store.FailureTypePageQuota),
					}); err != nil {
						return false, fmt.Errorf("creating quota failure: %w", err)
					}
				} else {
					return false, fmt.Errorf("unexpected error page: %w", err)
				}
			}

			group, ctx := errgroup.WithContext(ctx)
			group.SetLimit(2) // Have to be careful with this so we don't get banned/blocked.

			for _, vid := range pi.Items {
				vid := vid
				group.Go(func() error {
					// Once we see 'lastVideo' in the page, return false (already done from here).
					if hasLastVideo && vid.ContentDetails.VideoId == lastVideo.ID {
						return fmt.Errorf("video %s: %w", lastVideo.ID, ErrAlreadyIndexed)
					}

					// Check if the errgroup has gotten an error, in that case don't index.
					select {
					case <-ctx.Done():
						return nil
					default:
						log.Printf(
							"[INFO]: indexing %q - %q",
							vid.ContentDetails.VideoId,
							vid.Snippet.Title,
						)
						if err := IndexVideo(ctx, channel.ID, vid); err != nil {
							return fmt.Errorf(
								"indexing %s failed: %w",
								vid.ContentDetails.VideoId,
								err,
							)
						}
						log.Printf(
							"[INFO]: indexed %q - %q",
							vid.ContentDetails.VideoId,
							vid.Snippet.Title,
						)

						return nil
					}
				})
			}

			if err := group.Wait(); err != nil {
				if errors.Is(err, ErrAlreadyIndexed) {
					// TODO: this also cancels in progress indexing of non-indexed items.
					log.Printf("[INFO]: found already indexed video, stopping this: %v", err)
					return false, nil
				} else {
					return false, err
				}
			}

			return true, nil
		},
	)
	if err != nil {
		return err
	}

	return nil
}

// TODO: update doc comment.
//
// IndexVideo retrieves YouTube captions for the given video and parses it.
// A store.Video is created in the database, with multiple store.Transcript entries connected.
//
// If the video has captions disabled, or they can't be found, a store.Failure is created
// of type store.FailureTypeNoCaptions and no error is returned.
//
// The store.Video has either tube.TypeManual or tube.TypeAuto (preferring manual captions).
func IndexVideo(ctx context.Context, channelId string, video tube.PlaylistItem) error {
	videoId := video.ContentDetails.VideoId
	captions, typ, err := Yt.Captions(videoId)
	if err != nil {
		if errors.Is(err, tube.ErrNoCaptions) {
			log.Printf("[WARN]: no captions for %q, adding to failures: %v", videoId, err)

			if err := Queries.CreateFailure(ctx, store.CreateFailureParams{
				ChannelID: channelId,
				Data:      videoId,
				Type:      string(store.FailureTypeNoCaptions),
			}); err != nil {
				return fmt.Errorf("can't create failure for video %q: %w", videoId, err)
			}

			return nil
		} else if errors.Is(err, tube.ErrUnavailable) {
			log.Printf("[WARN]: %v", err)
			return nil
		} else {
			return fmt.Errorf("retrieving captions for %q: %w", videoId, err)
		}
	}

	tx, err := Db.Begin()
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer tx.Rollback() // Rollback, ignore error which is returned if tx is committed.

	qtx := Queries.WithTx(tx)

	published, err := tube.ParsePublishedTime(video.ContentDetails.VideoPublishedAt)
	if err != nil {
		return err
	}

	var t store.TranscriptType
	switch typ {
	case tube.TypeManual:
		t = store.TubeManual
	case tube.TypeAuto:
		t = store.TubeAuto
	default:
		panic("unreachable")
	}

	searchable := strings.Builder{}
	for _, entry := range captions.Entries {
		txt := html.UnescapeString(entry.Text)
		start := strconv.Itoa(int(entry.Start))
		searchable.WriteString("~")
		searchable.WriteString(start)
		searchable.WriteString("~")
		searchable.WriteString(txt)
	}

	if err = qtx.CreateVideo(ctx, store.CreateVideoParams{
		ID:                   videoId,
		ChannelID:            channelId,
		PublishedAt:          published,
		Title:                video.Snippet.Title,
		Description:          video.Snippet.Description,
		ThumbnailUrl:         tube.HighestResThumbnail(video.Snippet.Thumbnails).Url,
		SearchableTranscript: searchable.String(),
		TranscriptType:       string(t),
	}); err != nil {
		return fmt.Errorf("creating video %q: %w", videoId, err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

// Channel fetches the channel from the database,
// If it does not exists, the YouTube API is used to retrieve it
// and create a new channel in the database.
func Channel(ctx context.Context, id string) (*store.Channel, error) {
	if ch, err := Queries.Channel(ctx, id); err == nil {
		return &ch, nil
	}

	info, err := Yt.ChannelInfo(id)
	if err != nil {
		return nil, fmt.Errorf("getting channel info through API: %w", err)
	}

	ch, err := Queries.CreateChannel(ctx, store.CreateChannelParams{
		ID:           info.Id,
		Title:        info.Snippet.Title,
		VideosListID: info.ContentDetails.RelatedPlaylists.Uploads,
		ThumbnailUrl: tube.HighestResThumbnail(info.Snippet.Thumbnails).Url,
	})
	if err != nil {
		return nil, fmt.Errorf("creating channel in database: %w", err)
	}

	return &ch, nil
}
