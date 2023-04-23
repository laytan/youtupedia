package failures

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"

	"github.com/laytan/youtupedia/internal/stem"
	"github.com/laytan/youtupedia/internal/store"
	"github.com/laytan/youtupedia/internal/tube"
)

var (
	Db      *sql.DB
	Queries *store.Queries
	Yt      *tube.Client

	BinWhisperCpp    = "../../../whisper.cpp/main"
	WhisperModelPath = "../../../whisper.cpp/models/ggml-base.en.bin"
	BinFfmpeg        = "ffmpeg"
	BinYtDlp         = "yt-dlp"
)

func WhisperNoCaptionFailures(ctx context.Context) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	errs := make(chan error, 5)
	fc := Failures(ctx, errs, store.FailureTypeNoCaptions)
	dc := DownloadFailures(ctx, errs, fc)
	wc := WhisperDownloads(ctx, errs, dc)
	IndexWhispers(ctx, errs, wc)

	for {
		select {
		case <-ctx.Done():
			cancel()
			return
		case <-signals:
			cancel()
		case nerr := <-errs:
			err = errors.Join(err, nerr)
			cancel()
		}
	}
}

func Failures(ctx context.Context, errs chan<- error, typ store.FailureType) <-chan *store.Failure {
	c := make(chan *store.Failure)
	var last int64
	go func() {
		defer close(c)
		for {
			log.Println("[INFO]: querying next failure to process...")
			failure, err := Queries.NextFailure(ctx, store.NextFailureParams{
				ID:   last,
				Type: string(typ),
			})
			if err != nil {
				errs <- fmt.Errorf("getting next failure: %w", err)
				return
			}
			last = failure.ID

			log.Println("[INFO]: sending next failure...")
			select {
			case <-ctx.Done():
				return
			case c <- &failure:
			}
		}
	}()

	return c
}

type Download struct {
	VideoId string
	Path    string
	Video   *tube.ResVideo
}

func DownloadFailures(
	ctx context.Context,
	errs chan<- error,
	failures <-chan *store.Failure,
) <-chan *Download {
	c := make(chan *Download)
	go func() {
		defer close(c)
		for {
			// Using an inner function so each loop iteration runs the defer/cleanup.
			cont := func() bool {
				select {
				case <-ctx.Done():
					return false
				case failure, ok := <-failures:
					if !ok {
						return false
					}

					log.Println("[INFO]: retrieved failure, downloading...")

					videoId := failure.Data

					log.Println("[INFO]: checking if video does does not already exist")
					if _, err := Queries.Video(ctx, videoId); err == nil {
						log.Println("[WARN]: video already in database")
						return true
					}

					log.Printf("[INFO]: getting video %q info from API", videoId)
					video, err := Yt.Video(videoId)
					if err != nil {
						errs <- fmt.Errorf("getting youtube video info: %w", err)
						return false
					}

					if video.IsBroadcast() {
						log.Println("[WARN]: video is a broadcast, can't index, skipping")
						return true
					}

					defer cleanGlob(videoId+".*", videoId+".16k.wav")

					log.Printf(
						"[INFO]: downloading audio from video titled %q",
						video.Snippet.Title,
					)
					cmd := exec.CommandContext(
						ctx,
						BinYtDlp,
						"--ignore-config",
						"--output",
						videoId+".wav",
						"--extract-audio",
						"--audio-format",
						"wav",
						videoId,
					)
					if err := cmd.Run(); err != nil {
						handleExecErr("yt-dlp", err, errs)
						return false
					}

					log.Println("[INFO]: converting audio to 16 KHz")
					cmd = exec.CommandContext(
						ctx,
						BinFfmpeg,
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
					if err := cmd.Run(); err != nil {
						handleExecErr("ffmpeg", err, errs)
						return false
					}

					log.Println("[INFO]: sending downloaded video...")

					select {
					case <-ctx.Done():
						return false
					case c <- &Download{
						Path:    videoId + ".16k.wav",
						VideoId: videoId,
						Video:   video,
					}:
						return true
					}
				}
			}()
			if !cont {
				return
			}
		}
	}()

	return c
}

type Whisper struct {
	VideoId string
	Path    string
	Video   *tube.ResVideo
}

func WhisperDownloads(
	ctx context.Context,
	errs chan<- error,
	downloads <-chan *Download,
) <-chan *Whisper {
	c := make(chan *Whisper)
	go func() {
		defer close(c)
		for {
			log.Println("[INFO]: waiting for next download...")
			cont := func() bool { // Inner function so each iteration runs the defer.
				select {
				case <-ctx.Done():
					return false
				case download, ok := <-downloads:
					if !ok {
						return false
					}

					log.Println("[INFO]: retrieved download, running whisper...")
					videoId := download.VideoId

					defer func() {
						log.Printf("[INFO]: deleting file %s (cleanup)", download.Path)
						if err := os.Remove(download.Path); err != nil {
							log.Printf("cleaning download after whisper: %v", err)
						}
					}()

					log.Println("[INFO]: running whisper on the audio")
					cmd := exec.CommandContext(
						ctx,
						BinWhisperCpp,
						"-m",
						WhisperModelPath,
						"-f",
						download.Path,
						"-ocsv",
					)
					if err := cmd.Run(); err != nil {
						handleExecErr("whisper.cpp", err, errs)
						return false
					}

					log.Println("[INFO]: sending whisper csv...")
					select {
					case <-ctx.Done():
						return false
					case c <- &Whisper{
						VideoId: videoId,
						Path:    download.Path + ".csv",
						Video:   download.Video,
					}:
						return true
					}
				}
			}()
			if !cont {
				return
			}
		}
	}()

	return c
}

func IndexWhispers(ctx context.Context, errs chan<- error, whispers <-chan *Whisper) {
	go func() {
		for {
			cont := func() bool {
				log.Println("waiting for next whisper to index...")
				select {
				case <-ctx.Done():
					return false
				case whisper, ok := <-whispers:
					if !ok {
						return false
					}
					log.Println("retrieved whisper to index...")

					defer func() {
						log.Printf("[INFO]: deleting file %s (cleanup)", whisper.Path)
						if err := os.Remove(whisper.Path); err != nil {
							errs <- fmt.Errorf("cleaning %s: %w", whisper.Path, err)
						}
					}()

					log.Println("[INFO]: parsing output captions csv")
					fh, err := os.Open(whisper.Path)
					if err != nil {
						errs <- fmt.Errorf("could not open %s: %w", whisper.Path, err)
						return false
					}
					defer fh.Close()
					r := csv.NewReader(fh)
					r.ReuseRecord = true
					r.FieldsPerRecord = 3
					r.LazyQuotes = true

					// Read and discard header row.
					if _, err := r.Read(); err != nil {
						errs <- fmt.Errorf("reading header row of csv: %w", err)
						return false
					}

					tx, err := Db.Begin()
					if err != nil {
						errs <- fmt.Errorf("starting transaction: %w", err)
						return false
					}
					defer tx.Rollback() // Rollback, ignore error which is returned if tx is committed.
					qtx := Queries.WithTx(tx)

					searchable := strings.Builder{}
					for {
						row, err := r.Read()
						if err != nil {
							if errors.Is(err, io.EOF) {
								break
							}
							errs <- err

							log.Printf(
								"[WARN]: reading csv failed, writing failed csv to failed-%s.csv",
								whisper.VideoId,
							)
							fh, err := os.Open(whisper.Path)
							if err != nil {
								errs <- fmt.Errorf("could not open %s: %w", whisper.Path, err)
								return false
							}
							defer fh.Close()
							csv, err := io.ReadAll(fh)
							if err != nil {
								errs <- fmt.Errorf("could not read full csv into memory: %w", err)
								return false
							}
							err = os.WriteFile(
								fmt.Sprintf("failed-%s.csv", whisper.VideoId),
								csv,
								0666,
							)
							if err != nil {
								errs <- fmt.Errorf("writing failed csv: %w", err)
								return false
							}
						}

						startMs, err := strconv.Atoi(row[0])
						if err != nil {
							errs <- fmt.Errorf(
								"reading start ms from string %q in row %v: %w",
								row[0],
								row,
								err,
							)
							return false
						}

						endMs, err := strconv.Atoi(row[1])
						if err != nil {
							errs <- fmt.Errorf(
								"reading end ms from string %q in row %v: %w",
								row[1],
								row,
								err,
							)
							return false
						}

						durMs := endMs - startMs
						txt := strings.TrimSpace(row[2])

						id, err := qtx.CreateTranscript(ctx, store.CreateTranscriptParams{
							VideoID:  whisper.VideoId,
							Start:    float64(startMs) / 1000,
							Duration: float64(durMs) / 1000,
							Text:     txt,
						})
						if err != nil {
							errs <- fmt.Errorf("creating transcript entry for row %v: %w", row, err)
							return false
						}

						searchable.WriteString(fmt.Sprintf("~%d~", id))
						searchable.WriteString(stem.StemLine(txt))
					}

					published, err := tube.ParsePublishedTime(whisper.Video.Snippet.PublishedAt)
					if err != nil {
						errs <- fmt.Errorf("parsing video published: %w", err)
						return false
					}

					if err = qtx.CreateVideo(ctx, store.CreateVideoParams{
						ID:                   whisper.VideoId,
						ChannelID:            whisper.Video.Snippet.ChannelId,
						PublishedAt:          published,
						Title:                whisper.Video.Snippet.Title,
						Description:          whisper.Video.Snippet.Description,
						ThumbnailUrl:         tube.HighestResThumbnail(whisper.Video.Snippet.Thumbnails).Url,
						SearchableTranscript: searchable.String(),
						TranscriptType:       string(store.WhisperBase),
					}); err != nil {
						errs <- fmt.Errorf("creating video: %w", err)
						return false
					}

					log.Println("[INFO]: saving to the database")
					if err := tx.Commit(); err != nil {
						errs <- fmt.Errorf("committing transaction: %w", err)
						return false
					}

					log.Println("[INFO]: finished index...")
					return true
				}
			}()
			if !cont {
				return
			}
		}
	}()
}

func cleanGlob(glob string, exceptions ...string) {
	matches, err := fs.Glob(os.DirFS("."), glob)
	if err != nil {
		log.Printf("[ERROR]: glob for files to delete failed: %v", err)
	}

Outer:
	for _, match := range matches {
		for _, exception := range exceptions {
			if match == exception {
				continue Outer
			}
		}

		log.Printf("[INFO]: deleting file %q (cleanup)", match)
		if err := os.Remove(match); err != nil {
			log.Printf("[ERROR]: could not delete %q: %v", match, err)
		}
	}
}

func handleExecErr(id string, err error, errs chan<- error) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() == -1 { // context cancelled.
			log.Printf("[WARN]: %s: context cancelled", id)
			return
		}

		errs <- fmt.Errorf(
			id+": exit code %d and stderr %q: %w",
			exitErr.ExitCode(),
			string(exitErr.Stderr),
			err,
		)
		return
	}

	errs <- fmt.Errorf(id+": unexpected err: %w", err)
}
