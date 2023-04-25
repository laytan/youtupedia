package main

import (
	"context"
	"database/sql"
	_ "embed"
	"log"
	"os"

	"github.com/laytan/youtupedia/internal/failures"
	"github.com/laytan/youtupedia/internal/index"
	"github.com/laytan/youtupedia/internal/search"
	"github.com/laytan/youtupedia/internal/store"
	"github.com/laytan/youtupedia/internal/tube"
	"github.com/laytan/youtupedia/internal/youtupedia"
	_ "github.com/lib/pq"
)

var (
	queries *store.Queries
	db      *sql.DB
	yt      *tube.Client
	ytKey   = os.Getenv("YT_KEY")
	pgDsn   = os.Getenv("POSTGRES_DSN")
)

func main() {
	if ytKey == "" {
		panic("YT_KEY environment variable must be set")
	}

	if pgDsn == "" {
		panic("POSTGRES_DSN environment variable must be set")
	}

	ctx := context.Background()
	d, err := sql.Open("postgres", pgDsn)
	if err != nil {
		log.Fatalf("[ERROR]: opening database: %v", err)
	}

	db = d
	queries = store.New(db)
	yt = &tube.Client{Key: ytKey}

	failures.Queries = queries
	failures.Db = db
	failures.Yt = yt

	index.Queries = queries
	index.Db = db
	index.Yt = yt

	search.Queries = queries

	if len(os.Args) > 2 && os.Args[1] == "index" {
		id := os.Args[2]
		channel, err := index.Channel(ctx, id)
		if err != nil {
			log.Panicf("[ERROR]: Getting channel %q: %v", id, err)
		}

		log.Printf("[INFO]: Index for channel %q", channel.Title)
		if err := index.IndexChannel(ctx, channel); err != nil {
			log.Panicf("[ERROR]: Indexing channel %q: %v", channel.ID, err)
		}

		log.Printf("[INFO]: Finished indexing %q", id)
	} else if len(os.Args) > 1 && os.Args[1] == "failures" { // TODO: allow passing in channel.
		if err := failures.WhisperNoCaptionFailures(ctx); err != nil {
			log.Panicf("[ERROR]: Processing no caption failures: %v", err)
		}

		log.Println("[INFO]: Finished failures processing")
	} else {
		youtupedia.Queries = queries
		youtupedia.Start(ctx)
	}
}
