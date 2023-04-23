package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/csv"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/laytan/youtupedia/internal/stem"
	"github.com/laytan/youtupedia/internal/store"
	"github.com/laytan/youtupedia/internal/tube"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/sync/errgroup"
)

const (
	Key = "AIzaSyCeWESBtlwfuoViLOEqWdUq-V7W4JyG-zc"
)

var (
	queries *store.Queries
	db      *sql.DB
	yt      *tube.Client
)

var ErrAlreadyIndexed = errors.New("already indexed")

// TODO: on startup, check if all channels in database are still up to date?
// TODO: subscribe to uploads webhook, are captions available from minute one?
func main() {
	ctx := context.Background()
	d, err := sql.Open("sqlite3", "db.sqlite")
	if err != nil {
		log.Fatalf("[ERROR]: opening database: %v", err)
	}
	db = d

	queries = store.New(db)

	yt = &tube.Client{Key: Key}

	if len(os.Args) > 2 && os.Args[1] == "index" {
		id := os.Args[2]

		channel := channel(ctx, id)
		log.Printf("[INFO]: index for channel %q", channel.Title)

		lastVideo, err := queries.LastVideo(ctx, channel.ID)
		hasLastVideo := err == nil
		err = yt.EachPlaylistItemPage(
			channel.VideosListID,
			func(pi *tube.ResPlaylistItems, token string, err error) bool {
				if err != nil {
					if errors.Is(err, tube.ErrQuotaExceeded) {
						log.Println(
							"[WARN]: quota exceeded, adding page we left off at to the failures table",
						)
						if err := queries.CreateFailure(ctx, store.CreateFailureParams{
							ChannelID: channel.ID,
							Data:      token,
							Type:      string(store.FailureTypePageQuota),
						}); err != nil {
							log.Printf("[ERROR]: creating failure: %v", err)
						}
					} else {
						log.Printf("[ERROR]: unexpected error retrieving page: %v", err)
					}

					return false
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
							if err := indexVideo(ctx, channel.ID, vid); err != nil {
								return fmt.Errorf(
									"indexing %s failed: %w",
									vid.ContentDetails.VideoId,
									err,
								)
							}

							return nil
						}
					})
				}

				if err := group.Wait(); err != nil {
					if errors.Is(err, ErrAlreadyIndexed) {
						log.Printf("[INFO]: found already indexed video, stopping this: %v", err)
					} else {
						log.Fatal(err)
					}

					return false
				}

				return true
			},
		)
		if err != nil {
			log.Panicf("[ERROR]: playlist %s page: %v", channel.VideosListID, err)
		}

		log.Printf("[INFO]: finished indexing %q", id)
	} else if len(os.Args) > 3 && os.Args[1] == "search" {
		id := os.Args[2]
		query := os.Args[3]

		channel := channel(ctx, id)
		results, err := search(ctx, channel, query)
		if err != nil {
			log.Fatalf("[ERROR]: searching for %q in %q: %v", query, id, err)
		}

		log.Printf("[INFO]: %d matching videos", len(results))
	} else if len(os.Args) > 2 && os.Args[1] == "whisper" { // TODO: allow passing in channel.
		ctx, cancel := context.WithCancel(ctx)
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		go func() {
			select {
			case <-c:
				cancel()
			case <-ctx.Done():
				return
			}
		}()

		channel := os.Args[2]
		verbose := len(os.Args) > 3 && os.Args[3] == "verbose"

		log.Printf("[INFO]: channel %q, verbose %v", channel, verbose)

		failures, err := queries.NoCaptionFailures(ctx, channel)
		if err != nil {
			log.Panicf("[ERROR]: retrieving failures: %v", err)
		}

		// TODO: can the pc handle multiple whisper instances and yt downloads?
		log.Printf("[INFO]: %d failures in db, starting processing", len(failures))
		for _, failure := range failures {
			if err := whisperVideo(ctx, failure.Data, verbose); err != nil {
				log.Fatalf("[ERROR]: whisper failed for failure: %v", err)
			}

			if err := queries.DeleteFailure(ctx, failure.ID); err != nil {
				log.Fatalf("[ERROR]: deleting completed failure: %v", err)
			}
		}

		log.Println("[INFO]: all failures processed")
	} else if len(os.Args) > 2 && os.Args[1] == "stem" {
		inp := os.Args[2]
		fmt.Println(stem.StemLine(inp))
	} else {
		http.Handle("/", http.FileServer(http.Dir("web/static")))

		http.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
			id := "UCsBjURrPoezykLs9EqgamOA"
			query := r.URL.Query().Get("query")
			if len(query) < 4 {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte("Please type at least 4 characters"))
				return
			}

			ch := channel(ctx, id)
			res, err := search(ctx, ch, query)
			if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			t, err := template.ParseFiles("web/templates/results.html")
			if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			if err := t.Execute(w, res); err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		})

		log.Println("[INFO]: Listening on port :8080")
		log.Println(http.ListenAndServe(":8080", nil))
	}
}

func indexVideo(ctx context.Context, channelId string, video tube.PlaylistItem) error {
	videoId := video.ContentDetails.VideoId
	captions, typ, err := yt.Captions(videoId)
	if err != nil {
		if errors.Is(err, tube.ErrNoCaptions) {
			log.Printf("[WARN]: no captions for %q, adding to failures: %v", videoId, err)

			if err := queries.CreateFailure(ctx, store.CreateFailureParams{
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

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer tx.Rollback() // Rollback, ignore error which is returned if tx is committed.

	qtx := queries.WithTx(tx)

	searchable := strings.Builder{}
	for _, entry := range captions.Entries {
		txt := normalizeCaption(entry.Text)
		id, err := qtx.CreateTranscript(ctx, store.CreateTranscriptParams{
			VideoID:  videoId,
			Start:    entry.Start,
			Duration: float64(entry.Dur),
			Text:     txt,
		})
		if err != nil {
			return fmt.Errorf("inserting caption %v: %w", entry, err)
		}

		searchable.WriteString(fmt.Sprintf("~%d~", id))
		searchable.WriteString(stem.StemLine(txt))
	}

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

	err = qtx.CreateVideo(ctx, store.CreateVideoParams{
		ID:                   videoId,
		ChannelID:            channelId,
		PublishedAt:          published,
		Title:                video.Snippet.Title,
		Description:          video.Snippet.Description,
		ThumbnailUrl:         tube.HighestResThumbnail(video.Snippet.Thumbnails).Url,
		SearchableTranscript: searchable.String(),
		TranscriptType:       string(t),
	})
	if err != nil {
		return fmt.Errorf("creating video %q: %w", videoId, err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

func channel(ctx context.Context, id string) store.Channel {
	if ch, err := queries.Channel(ctx, id); err == nil {
		return ch
	}

	info, err := yt.ChannelInfo(id)
	if err != nil {
		log.Fatalf("[ERROR]: getting channel info: %v", err)
	}

	ch, err := queries.CreateChannel(ctx, store.CreateChannelParams{
		ID:           info.Id,
		Title:        info.Snippet.Title,
		VideosListID: info.ContentDetails.RelatedPlaylists.Uploads,
		ThumbnailUrl: tube.HighestResThumbnail(info.Snippet.Thumbnails).Url,
	})
	if err != nil {
		log.Panicf("[ERROR]: creating channel row: %v", err)
	}

	return ch
}

func whisperVideo(ctx context.Context, videoId string, verbose bool) error {
	start := time.Now()
	defer func() {
		matches, err := fs.Glob(os.DirFS("."), videoId+".*")
		if err != nil {
			log.Panicf("[ERROR]: glob for files to delete failed: %v", err)
		}

		for _, match := range matches {
			log.Printf("[INFO]: deleting file %q (cleanup)", match)
			if err := os.Remove(match); err != nil {
				log.Panicf("[ERROR]: could not delete %q: %v", match, err)
			}
		}

		log.Printf("[INFO]: whisper video took %s", time.Since(start))
	}()

	log.Println("[INFO]: checking if video does does not already exist")
	if _, err := queries.Video(ctx, videoId); err == nil {
		return fmt.Errorf("video already in database")
	}

	log.Printf("[INFO]: getting video %q info from API", videoId)
	video, err := yt.Video(videoId)
	if err != nil {
		return fmt.Errorf("getting youtube video info: %w", err)
	}

	if video.IsBroadcast() {
		log.Println("[WARN]: video is a broadcast, can't index, skipping")
		return nil
	}

	log.Printf("[INFO]: downloading audio from video titled %q", video.Snippet.Title)
	cmd := exec.CommandContext(
		ctx,
		"yt-dlp",
		"--ignore-config",
		"--output",
		videoId+".wav",
		"--extract-audio",
		"--audio-format",
		"wav",
		videoId,
	)
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf(
				"yt-dlp exited unsuccessfully with exit code %d and stderr %q: %w",
				exitErr.ExitCode(),
				string(exitErr.Stderr),
				err,
			)
		}

		return fmt.Errorf("exec err during yt-dlp: %w", err)
	}

	log.Println("[INFO]: converting audio to 16 KHz")
	cmd = exec.CommandContext(
		ctx,
		"ffmpeg",
		"-i",
		videoId+".wav",
		"-ar",
		"16000",
		"-ac",
		"1",
		"-c:a",
		"pcm_s16le",
		videoId+".16k.wav",
	)
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf(
				"ffmpeg exited unsuccessfully with exit code %d and stderr %q: %w",
				exitErr.ExitCode(),
				string(exitErr.Stderr),
				err,
			)
		}

		return fmt.Errorf("exec err during ffmpeg: %w", err)
	}

	log.Println("[INFO]: running whisper on the audio")
	cmd = exec.CommandContext(
		ctx,
		"/Users/laytan/projects/whisper.cpp/main",
		"-m",
		"/Users/laytan/projects/whisper.cpp/models/ggml-base.en.bin",
		"-f",
		videoId+".16k.wav",
		"-ocsv",
	)
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf(
				"whisper exited unsuccessfully with exit code %d and stderr %q: %w",
				exitErr.ExitCode(),
				string(exitErr.Stderr),
				err,
			)
		}

		return fmt.Errorf("exec err during whisper: %w", err)
	}

	log.Println("[INFO]: parsing output captions csv")
	fh, err := os.Open(videoId + ".16k.wav.csv")
	if err != nil {
		return fmt.Errorf("could not open %s.16k.wav.csv: %w", videoId, err)
	}
	defer fh.Close()
	r := csv.NewReader(fh)
	r.ReuseRecord = true
	r.FieldsPerRecord = 3
	r.LazyQuotes = true

	// Read and discard header row.
	if _, err := r.Read(); err != nil {
		return fmt.Errorf("reading header row of csv: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer tx.Rollback() // Rollback, ignore error which is returned if tx is committed.
	qtx := queries.WithTx(tx)

	searchable := strings.Builder{}
	for {
		row, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			rerr := err

			log.Printf("[WARN]: reading csv failed, writing failed csv to failed-%s.csv", videoId)
			fh, err := os.Open(videoId + ".16k.wav.csv")
			if err != nil {
				return fmt.Errorf("could not open %s.16k.wav.csv: %w", videoId, err)
			}
			defer fh.Close()
			csv, err := io.ReadAll(fh)
			if err != nil {
				return fmt.Errorf("could not read full csv into memory: %w", err)
			}
			if err := os.WriteFile(fmt.Sprintf("failed-%s.csv", videoId), csv, 0666); err != nil {
				return fmt.Errorf("writing failed csv: %w", err)
			}

			return fmt.Errorf("reading row of csv: %w", rerr)
		}

		startMs, err := strconv.Atoi(row[0])
		if err != nil {
			return fmt.Errorf("reading start ms from string %q in row %v: %w", row[0], row, err)
		}

		endMs, err := strconv.Atoi(row[1])
		if err != nil {
			return fmt.Errorf("reading end ms from string %q in row %v: %w", row[1], row, err)
		}

		durMs := endMs - startMs
		txt := strings.TrimSpace(row[2])

		id, err := qtx.CreateTranscript(ctx, store.CreateTranscriptParams{
			VideoID:  videoId,
			Start:    float64(startMs) / 1000,
			Duration: float64(durMs) / 1000,
			Text:     txt,
		})
		if err != nil {
			return fmt.Errorf("creating transcript entry for row %v: %w", row, err)
		}

		searchable.WriteString(fmt.Sprintf("~%d~", id))
		searchable.WriteString(stem.StemLine(txt))
	}

	published, err := tube.ParsePublishedTime(video.Snippet.PublishedAt)
	if err != nil {
		return err
	}

	// TODO: update if exists (remove existing transcripts).

	if err = qtx.CreateVideo(ctx, store.CreateVideoParams{
		ID:                   videoId,
		ChannelID:            video.Snippet.ChannelId,
		PublishedAt:          published,
		Title:                video.Snippet.Title,
		Description:          video.Snippet.Description,
		ThumbnailUrl:         tube.HighestResThumbnail(video.Snippet.Thumbnails).Url,
		SearchableTranscript: searchable.String(),
		TranscriptType:       string(store.WhisperBase),
	}); err != nil {
		return fmt.Errorf("creating video: %w", err)
	}

	log.Println("[INFO]: saving to the database")
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

type Result struct {
	Video   store.Video
	Results []store.Transcript
}

func search(ctx context.Context, ch store.Channel, query string) (res []Result, err error) {
	start := time.Now()
	defer func() {
		log.Printf("[INFO]: searching took %s", time.Since(start))
	}()

	log.Printf("[INFO]: searching for %q in %q", query, ch.Title)

	videos, err := queries.VideosOfChannel(ctx, ch.ID)
	if err != nil {
		return nil, fmt.Errorf("retrieving channel videos: %w", err)
	}

	var group errgroup.Group
	group.SetLimit(20)
	var mu sync.Mutex
	for _, vid := range videos {
		vid := vid
		group.Go(func() error {
			results, err := searchIn(&vid, query)
			if err != nil {
				return fmt.Errorf("searching: %w", err)
			}

			tsc := make([]store.Transcript, 0, len(results))
			for _, r := range results {
				ts, err := queries.Transcript(ctx, r)
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

	return res, nil
}

func searchIn(vid *store.Video, query string) (res []int64, err error) {
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

func normalizeCaption(caption string) string {
	return html.UnescapeString(caption)
}
