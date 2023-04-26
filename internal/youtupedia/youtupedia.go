package youtupedia

import (
	"context"
	"embed"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/html"
	"github.com/laytan/youtupedia/internal/index"
	"github.com/laytan/youtupedia/internal/search"
	"github.com/laytan/youtupedia/internal/store"
)

const (
	ServeChannel = "UCd3dNckv1Za2coSaHGHl5aA"
	Port         = ":8080"
	CheckTime    = time.Hour
)

var (
	Queries *store.Queries

	//go:embed templates
	_templatesFS embed.FS
	templatesFS  fs.FS
)

type IndexData struct {
	Channels []store.Channel
}

type ChannelData struct {
	Channel store.Channel
	Results []search.Result
	IsQuery bool
	Query   string
}

func init() {
	subTemplatesFS, err := fs.Sub(_templatesFS, "templates")
	if err != nil {
		panic(err)
	}
	templatesFS = subTemplatesFS
}

func Start(ctx context.Context) {
	engine := html.NewFileSystem(http.FS(templatesFS), "")
    engine.Debug(true)
    engine.Reload(true)

	app := fiber.New(fiber.Config{
		Views: engine,
        ViewsLayout: "layout",
	})

	// go periodicallyCheckNewUploads(ctx)

	// TODO: can this be static?
	app.Static("/", "internal/youtupedia/static")

	app.Get("/", func(c *fiber.Ctx) error {
		channels, err := Queries.Channels(ctx)
		if err != nil {
			log.Println(err)
			c.Status(http.StatusInternalServerError)
			return nil
		}

		return c.Render("index", IndexData{Channels: channels})
	})

	app.Get("/@:url", func(c *fiber.Ctx) error {
		var data ChannelData
		channel, err := Queries.ChannelByUrl(ctx, "@"+c.Params("url"))
		if err != nil {
			return fmt.Errorf("retrieving channel: %w", err)
		}
		data.Channel = channel

		_, isHtmx := c.GetReqHeaders()["Hx-Request"]

		query := c.Query("q")
		if query == "" {
			if isHtmx {
				return c.Render("results", data.Results)
			}

			return c.Render("channel", data)
		}

		if len(query) < 3 {
			return fiber.NewError(
				http.StatusUnprocessableEntity,
				"Please type at least 3 characters",
			)
		}
		data.Query = strings.Clone(query)

		log.Printf("[INFO]: searching for %q in %q", query, channel.Title)
		res, err := search.Channel(ctx, &channel, query)
		if err != nil {
			log.Printf("[ERROR]: %v", err)
			return fiber.NewError(http.StatusInternalServerError, "search failed")
		}

		data.Results = res
		data.IsQuery = true

		if isHtmx {
			return c.Render("results", data.Results)
		}
		return c.Render("channel", data)
	})

	log.Fatal(app.Listen(Port))
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
