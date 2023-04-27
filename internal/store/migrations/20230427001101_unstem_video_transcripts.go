package migrations

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigration(upUnstemVideoTranscripts, downUnstemVideoTranscripts)
}

type Row struct {
	VideoId string
	Start   float64
	Text    string
}

func upUnstemVideoTranscripts(tx *sql.Tx) error {
	// for each video, select all transcripts ordered by start, combine them all into a new searchable_transcript.
	rows, err := tx.Query(
		"SELECT video_id, start, text FROM transcripts ORDER BY video_id, start;",
	)
	if err != nil {
		return fmt.Errorf("querying videos: %w", err)
	}

    // Need this intermedia map, can't execute query within while scanning rows.
	newTranscripts := map[string]string{}

	currId := ""
	builder := strings.Builder{}
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.VideoId, &r.Start, &r.Text); err != nil {
			return fmt.Errorf("scanning row: %w", err)
		}

		if currId != "" && currId != r.VideoId {
			newTranscripts[currId] = builder.String()
			builder.Reset()
			currId = r.VideoId
		} else if currId == "" {
			currId = r.VideoId
        }

		start := strconv.Itoa(int(math.Round(r.Start)))
		builder.WriteString(" ~")
        builder.WriteString(start)
        builder.WriteString("~ ")
		builder.WriteString(r.Text)
	}

    // Save the last one.
    newTranscripts[currId] = builder.String()

    log.Printf("%d videos", len(newTranscripts))

    for video_id, newTranscript := range newTranscripts {
        if _, err := tx.Exec("UPDATE videos SET searchable_transcript = $1 WHERE id = $2;", newTranscript, video_id); err != nil {
            return fmt.Errorf("saving new transcript: %w", err)
        }
    }

	return nil
}

func downUnstemVideoTranscripts(tx *sql.Tx) error {
	return nil
}
