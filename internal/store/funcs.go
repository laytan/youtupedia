package store

import (
	"context"
	"log"
	"time"
)

func (t *Transcript) StartDuration() time.Duration {
	return time.Duration(t.Start) * time.Second
}

// TranscriptsByIds is an optimized implementation to retrieve a lot of transcripts by their ID's.
func (q *Queries) TranscriptsByIds(
	ctx context.Context,
	ids []int64,
) (map[int64]*Transcript, error) {
	if len(ids) == 0 {
		return nil, nil
	}

    start := time.Now()
    defer func() {
        log.Printf("[INFO]: transcripts query took %s", time.Since(start))
    }()

	query := "SELECT * FROM transcripts WHERE id IN ("
	for i := range ids {
		query += "?"

		if i == len(ids)-1 {
			query += ");"
		} else {
			query += ","
		}
	}

	ifs := make([]interface{}, len(ids))
	for i := range ids {
		ifs[i] = ids[i]
	}

	rows, err := q.db.QueryContext(ctx, query, ifs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make(map[int64]*Transcript, len(ids))
	for rows.Next() {
		var i Transcript
		if err := rows.Scan(
			&i.ID,
			&i.VideoID,
			&i.Start,
			&i.Duration,
			&i.Text,
			&i.CreatedAt,
			&i.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items[i.ID] = &i
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
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

	query := "SELECT * FROM videos WHERE channel_id = ?"
	for _, word := range words {
		// NOTE: this would be dangerous for sql injection, but stemming removes all the special characters that
        // are able to do that already, so this should be save.
		query += " AND searchable_transcript LIKE \"%" + word + "%\""
	}
	query += ";"

	rows, err := q.db.QueryContext(ctx, query, channelID)
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
