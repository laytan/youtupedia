package migrations

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/laytan/youtupedia/internal/tube"
	"github.com/pressly/goose/v3"
)

type Channel struct {
	ID           string
	Title        string
	VideosListID string
	ThumbnailUrl string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CustomUrl    sql.NullString
}

func init() {
	goose.AddMigration(up, down)
}

func up(tx *sql.Tx) error {
	if _, ok := os.LookupEnv("YT_KEY"); !ok {
		return errors.New("YT_KEY not found")
	}
	yt := tube.Client{Key: os.Getenv("YT_KEY")}

	row, err := tx.Query("SELECT * FROM channels WHERE custom_url IS NULL;")
	if err != nil {
		return fmt.Errorf("querying channels with custom url that is null: %w", err)
	}

	var channels []Channel
	for row.Next() {
		var ch Channel
		if err := row.Scan(&ch.ID, &ch.Title, &ch.VideosListID, &ch.ThumbnailUrl, &ch.CreatedAt, &ch.UpdatedAt, &ch.CustomUrl); err != nil {
			return fmt.Errorf("scanning channel row: %w", err)
		}
		channels = append(channels, ch)
	}

	for _, ch := range channels {
		chinfo, err := yt.ChannelInfo(ch.ID)
		if err != nil {
			return fmt.Errorf("retrieving channel info: %w", err)
		}

		log.Printf(
			"[INFO]: setting custom_url %q on channel %q",
			chinfo.Snippet.CustomUrl,
			ch.Title,
		)
		if _, err := tx.Exec("UPDATE channels SET custom_url = $1 WHERE id = $2;", chinfo.Snippet.CustomUrl, ch.ID); err != nil {
			return fmt.Errorf(
				"setting custom_url %q on channel %q: %w",
				chinfo.Snippet.CustomUrl,
				ch.Title,
				err,
			)
		}
	}

	return nil
}

func down(tx *sql.Tx) error {
	return nil
}
