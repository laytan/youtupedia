package search_test

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"os"
	"testing"

	"github.com/laytan/youtupedia/internal/index"
	"github.com/laytan/youtupedia/internal/search"
	"github.com/laytan/youtupedia/internal/store"
	_ "github.com/lib/pq"
)

const (
	Channel = "UCFKDEp9si4RmHFWJW1vYsMA"
	Query   = "Thanks for watching"
)

func BenchmarkSearch(b *testing.B) {
	ctx := context.Background()
	pgDsn := os.Getenv("POSTGRES_DSN")
	if pgDsn == "" {
		panic("POSTGRES_DSN environment variable not set")
	}

	d, err := sql.Open("postgres", pgDsn)
	if err != nil {
		log.Fatalf("[ERROR]: opening database: %v", err)
	}

	buf := bytes.Buffer{}
	log.SetOutput(&buf)

	queries := store.New(d)
	index.Queries = queries
	search.Queries = queries

	channel, err := index.Channel(ctx, Channel)
	if err != nil {
		panic(err)
	}

	for i := 0; i < b.N; i++ {
		_, err := search.Channel(ctx, channel, Query)
		if err != nil {
			panic(err)
		}
	}
}
