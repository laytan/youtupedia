package store

import (
	"context"
	"log"
	"strconv"
	"time"
)

func (t *Transcript) StartDuration() time.Duration {
	return time.Duration(t.Start) * time.Second
}

// VideosOfChannelWithWords is an optimized query to retrieve videos that
// might be a match of a query, words must be stemmed.
func (q *Queries) VideosOfChannelWithWords(
	ctx context.Context,
	channelID string,
	words []string,
) ([]Video, error) {
	if len(words) == 0 {
		return nil, nil
	}

	start := time.Now()
	defer func() {
		log.Printf("[INFO]: videos query took %s", time.Since(start))
	}()

	ifs := make([]interface{}, len(words)+1)
	ifs[0] = channelID

	query := "SELECT * FROM videos WHERE channel_id = $1 AND searchable_transcript LIKE '%' "
	for i, word := range words {
		query += "|| $" + strconv.Itoa(i+2) + " || '%' "
		ifs[i+1] = word
	}
	query += ";"

	rows, err := q.db.QueryContext(ctx, query, ifs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Video
	for rows.Next() {
		var i Video
		if err := rows.Scan(
			&i.ID,
			&i.ChannelID,
			&i.PublishedAt,
			&i.Title,
			&i.Description,
			&i.ThumbnailUrl,
			&i.SearchableTranscript,
			&i.CreatedAt,
			&i.UpdatedAt,
			&i.TranscriptType,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
