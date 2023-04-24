package search_test

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"testing"

	"github.com/laytan/youtupedia/internal/index"
	"github.com/laytan/youtupedia/internal/pathutils"
	"github.com/laytan/youtupedia/internal/search"
	"github.com/laytan/youtupedia/internal/store"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/profile"
)

const (
	Channel = "UCFKDEp9si4RmHFWJW1vYsMA"
	Query   = "Thanks for watching"
)

func BenchmarkSearch(b *testing.B) {
	ctx := context.Background()
	d, err := sql.Open("sqlite3", pathutils.Root()+"/db.sqlite")
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

    defer profile.Start(profile.MemProfile, profile.ProfilePath(pathutils.Root())).Stop()

	for i := 0; i < b.N; i++ {
		_, err := search.Channel(ctx, channel, Query)
		if err != nil {
			panic(err)
		}
	}
}
