package youtupedia

import (
	"context"
	"embed"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/laytan/youtupedia/internal/index"
	"github.com/laytan/youtupedia/internal/search"
	"github.com/laytan/youtupedia/internal/store"
)

const (
	ServeChannel = "UCFKDEp9si4RmHFWJW1vYsMA"
	Port         = ":8080"
	CheckTime    = time.Hour
)

var (
	Queries *store.Queries

	//go:embed static
	_staticFS embed.FS
	staticFS  fs.FS
	//go:embed templates
	_templatesFS embed.FS

	templResults *template.Template
)

func init() {
	newStaticFS, err := fs.Sub(_staticFS, "static")
	if err != nil {
		panic(err)
	}
	staticFS = newStaticFS

	subTemplatesFS, err := fs.Sub(_templatesFS, "templates")
	if err != nil {
		panic(err)
	}

	templResults = template.Must(template.ParseFS(subTemplatesFS, "results.html"))
}

func Start(ctx context.Context) {
	go periodicallyCheckNewUploads(ctx)

	http.Handle("/", http.FileServer(http.FS(staticFS)))

	http.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			log.Printf("[INFO]: Search took %s", time.Since(start))
		}()

		query := r.URL.Query().Get("query")
		if len(query) < 3 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte("Please type at least 3 characters"))
			return
		}

		channel, err := index.Channel(ctx, ServeChannel)
		if err != nil {
			log.Printf("[ERROR]: retrieving channel %q: %v", ServeChannel, err)
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte("Could not find/retrieve channel to search for"))
			return
		}

		log.Printf("[INFO]: searching for %q in %q", query, channel.Title)
		res, err := search.Channel(ctx, channel, query)
		if err != nil {
			log.Printf("[ERROR]: searching through channel: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Searching failed"))
			return
		}

		if err := templResults.Execute(w, res); err != nil {
			log.Printf("[ERROR]: executing results template: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Populating results template failed"))
			return
		}
	})

	log.Printf("[INFO]: Listening on port %s", Port)
	log.Println(http.ListenAndServe(Port, nil))
}

// NOTE: maybe could do webhooks, like a checkNewUploads, followed by subscribing to the webhooks for the channels.
func periodicallyCheckNewUploads(ctx context.Context) {
	if err := checkNewUploads(ctx); err != nil {
		log.Printf("[ERROR]: checking new uploads: %v", err)
	}

	ticker := time.NewTicker(CheckTime)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := checkNewUploads(ctx); err != nil {
				log.Printf("[ERROR]: checking new uploads: %v", err)
			}
		}
	}
}

func checkNewUploads(ctx context.Context) error {
	channels, err := Queries.Channels(ctx)
	if err != nil {
		return fmt.Errorf("retrieving channels: %w", err)
	}

	for _, channel := range channels {
		log.Printf("[INFO]: checking new uploads for %q - %q", channel.ID, channel.Title)
		err := index.IndexChannel(ctx, &channel)
		if err != nil && !errors.Is(err, index.ErrAlreadyIndexed) {
			return fmt.Errorf("indexing channel %q: %w", channel.Title, err)
		}
	}

	return nil
}
