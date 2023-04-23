package migrations

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/laytan/youtupedia/internal/stem"
	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigration(Up, Down)
}

func Up(tx *sql.Tx) error {
	rows, err := tx.Query("SELECT id, searchable_transcript FROM videos")
	if err != nil {
		return fmt.Errorf("retrieving videos: %w", err)
	}
	defer rows.Close()

	var id, searchableTranscript string
    updated := strings.Builder{}
	for rows.Next() {
        updated.Reset()
		if err := rows.Scan(&id, &searchableTranscript); err != nil {
			return fmt.Errorf("scanning video row: %w", err)
		}

		inMeta := false
		var nonMetaStart int
		for i, ch := range searchableTranscript {
			switch ch {
			case '~':
				if inMeta {
					nonMetaStart = i + 1
				} else if nonMetaStart > 0 {
					// stem from nonMetaStart to here, write to updated.
					toStem := searchableTranscript[nonMetaStart:i]

					// Sanity check, that the ranges are correct.
					if strings.Contains(toStem, "~") {
						panic(toStem)
					}

					stemmed := stem.StemLine(toStem)
					updated.WriteString(stemmed)
				}

				inMeta = !inMeta
				updated.WriteRune(ch)
			default:
				if inMeta {
					updated.WriteRune(ch)
				}
			}
		}

		if _, err := tx.Exec("UPDATE videos SET searchable_transcript = ? WHERE id = ?", updated.String(), id); err != nil {
			return fmt.Errorf("updating video: %w", err)
		}
	}

	return nil
}

func Down(tx *sql.Tx) error {
	// Can't really roll this back.
	return nil
}
