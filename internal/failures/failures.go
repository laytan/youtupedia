package failures

import (
	"bytes"
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
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/laytan/youtupedia/internal/stem"
	"github.com/laytan/youtupedia/internal/store"
	"github.com/laytan/youtupedia/internal/tube"
)

var (
	Db      *sql.DB
	Queries *store.Queries
	Yt      *tube.Client

	WhisperModelPath  = "../whisper.cpp/models/ggml-base.en.bin"
	WhisperThreads    = "1"
	WhisperProcessors = strconv.Itoa(
		runtime.NumCPU() - 1,
	) // Keep 1 processor for non-whisper stuff.
	BinWhisperCpp = "../whisper.cpp/main"

	BinFfmpeg = "ffmpeg"
	BinYtDlp  = "yt-dlp"
)

func init() {
    if wp, ok := os.LookupEnv("WHISPER_BIN"); ok {
        BinWhisperCpp = wp
    }

    if mp, ok := os.LookupEnv("WHISPER_MODEL"); ok {
        WhisperModelPath = mp
    }
}

func WhisperNoCaptionFailures(ctx context.Context) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	errs := make(chan error, 5)
	fc := Failures(ctx, errs, store.FailureTypeNoCaptions)
	dc := DownloadFailures(ctx, errs, fc)
	wc := WhisperDownloads(ctx, errs, dc)
	IndexWhispers(ctx, errs, wc)

	reportTicker := time.NewTicker(time.Minute)
	defer reportTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			cancel()
			// Sleep so cleanup can finish.
			// Don't @ me!
			time.Sleep(time.Second)
			return
		case <-signals:
			signal.Stop(signals)
			cancel()
		case nerr := <-errs:
			err = errors.Join(err, nerr)
			cancel()
		case <-reportTicker.C:
			count, err := Queries.CountFailures(
				ctx,
				store.CountFailuresParams{Type: string(store.FailureTypeNoCaptions)},
			)
			if err != nil {
				errs <- fmt.Errorf("counting failures: %w", err)
			}
			log.Printf("[INFO]: %d failures in the queue", count)
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
	FailureId int64
	VideoId   string
	Path      string
	Video     *tube.ResVideo
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
						log.Println("[WARN]: video already in database, removing failure")

						if err := Queries.DeleteFailure(ctx, failure.ID); err != nil {
							errs <- fmt.Errorf("deleting indexed failure: %w", err)
							return false
						}

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
                        "-f",
                        "bestaudio",
						"--ignore-config",
						"--no-progress",
						"--output",
						videoId+".wav",
						"--extract-audio",
						"--audio-format",
						"wav",
                        "https://youtube.com/watch?v=" + videoId,
					)
					dlStdout := &bytes.Buffer{}
					cmd.Stdout = dlStdout // Need to capture stdout for error messages, for some reasons errors are shown on stdout.
					if err := cmd.Run(); err != nil {
						handleExecErr("yt-dlp", err, errs, dlStdout.String())
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
						"--",
						videoId+".16k.wav",
					)
					dlStdout.Reset()
					cmd.Stdout = dlStdout // Need to capture stdout for error messages, for some reasons errors are shown on stdout.
					if err := cmd.Run(); err != nil {
						handleExecErr("ffmpeg", err, errs, dlStdout.String())
						return false
					}

					log.Println("[INFO]: sending downloaded video...")

					select {
					case <-ctx.Done():
						return false
					case c <- &Download{
						FailureId: failure.ID,
						Path:      videoId + ".16k.wav", // TODO: we sent the message, but it might not be received, so there might not be a cleanup for this file.
						VideoId:   videoId,
						Video:     video,
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
	FailureId int64
	VideoId   string
	Path      string
	Video     *tube.ResVideo
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
						"-t",
						WhisperThreads,
						"-p",
						WhisperProcessors,
					)
					dlStdout := bytes.Buffer{}
					cmd.Stdout = &dlStdout // Need to capture stdout for error messages, for some reasons errors are shown on stdout.
					if err := cmd.Run(); err != nil {
						handleExecErr("whisper.cpp", err, errs, dlStdout.String())
						return false
					}

					log.Println("[INFO]: sending whisper csv...")
					select {
					case <-ctx.Done():
						return false
					case c <- &Whisper{
						FailureId: download.FailureId,
						VideoId:   videoId,
						Path:      download.Path + ".csv",
						Video:     download.Video,
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
				log.Println("[INFO]: waiting for next whisper to index...")
				select {
				case <-ctx.Done():
					return false
				case whisper, ok := <-whispers:
					if !ok {
						return false
					}
					log.Println("[INFO]: retrieved whisper to index...")

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
						SearchableTranscript: "",
						TranscriptType:       string(store.WhisperBase),
					}); err != nil {
						errs <- fmt.Errorf("creating video: %w", err)
						return false
					}

					searchable := strings.Builder{}
					for {
						row, err := r.Read()
						if err != nil {
							if errors.Is(err, io.EOF) {
								break
							}

							log.Printf(
								"[WARN]: reading csv failed, writing failed csv to failed-%s.csv and skipping this failure: %v",
								whisper.VideoId,
								err,
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

							return true
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

						txt := strings.TrimSpace(row[2])

						id, err := qtx.CreateTranscript(ctx, store.CreateTranscriptParams{
							VideoID: whisper.VideoId,
							Start:   int32((time.Duration(startMs) * time.Millisecond) / time.Second),
							Text:    txt,
						})
						if err != nil {
							errs <- fmt.Errorf("creating transcript entry for row %v: %w", row, err)
							return false
						}

						searchable.WriteString(fmt.Sprintf("~%d~", id))
						searchable.WriteString(stem.StemLine(txt))
					}

                    if err := qtx.SetSearchableTranscript(ctx, store.SetSearchableTranscriptParams{
                    	ID:                   whisper.VideoId,
                    	SearchableTranscript: searchable.String(),
                    }); err != nil {
                            errs <- fmt.Errorf("updating transcript: %w", err)
                    }

					if err := qtx.DeleteFailure(ctx, whisper.FailureId); err != nil {
						errs <- fmt.Errorf("deleting indexed failure: %w", err)
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

func handleExecErr(id string, err error, errs chan<- error, extra ...string) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() == -1 { // context cancelled.
			log.Printf("[WARN]: %s: context cancelled", id)
			return
		}

		errs <- fmt.Errorf(
			id+": exit code %d and stderr %q and extra: %q: %w",
			exitErr.ExitCode(),
			string(exitErr.Stderr),
			strings.Join(extra, ", "),
			err,
		)
		return
	}

	errs <- fmt.Errorf(id+": unexpected err: %w", err)
}
