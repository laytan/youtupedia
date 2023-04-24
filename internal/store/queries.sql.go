// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.17.2
// source: queries.sql

package store

import (
	"context"
	"time"
)

const channel = `-- name: Channel :one
SELECT id, title, videos_list_id, thumbnail_url, created_at, updated_at FROM channels
WHERE id = ? LIMIT 1
`

func (q *Queries) Channel(ctx context.Context, id string) (Channel, error) {
	row := q.db.QueryRowContext(ctx, channel, id)
	var i Channel
	err := row.Scan(
		&i.ID,
		&i.Title,
		&i.VideosListID,
		&i.ThumbnailUrl,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const channels = `-- name: Channels :many
SELECT id, title, videos_list_id, thumbnail_url, created_at, updated_at FROM channels
`

func (q *Queries) Channels(ctx context.Context) ([]Channel, error) {
	rows, err := q.db.QueryContext(ctx, channels)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Channel
	for rows.Next() {
		var i Channel
		if err := rows.Scan(
			&i.ID,
			&i.Title,
			&i.VideosListID,
			&i.ThumbnailUrl,
			&i.CreatedAt,
			&i.UpdatedAt,
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

const countFailures = `-- name: CountFailures :one
SELECT COUNT(*) FROM failures
WHERE type = ?
AND id > ?
`

type CountFailuresParams struct {
	Type string
	ID   int64
}

func (q *Queries) CountFailures(ctx context.Context, arg CountFailuresParams) (int64, error) {
	row := q.db.QueryRowContext(ctx, countFailures, arg.Type, arg.ID)
	var count int64
	err := row.Scan(&count)
	return count, err
}

const createChannel = `-- name: CreateChannel :one
INSERT INTO channels (
    id, title, videos_list_id, thumbnail_url
) VALUES (
    ?,  ?,     ?,              ?
)
RETURNING id, title, videos_list_id, thumbnail_url, created_at, updated_at
`

type CreateChannelParams struct {
	ID           string
	Title        string
	VideosListID string
	ThumbnailUrl string
}

func (q *Queries) CreateChannel(ctx context.Context, arg CreateChannelParams) (Channel, error) {
	row := q.db.QueryRowContext(ctx, createChannel,
		arg.ID,
		arg.Title,
		arg.VideosListID,
		arg.ThumbnailUrl,
	)
	var i Channel
	err := row.Scan(
		&i.ID,
		&i.Title,
		&i.VideosListID,
		&i.ThumbnailUrl,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const createFailure = `-- name: CreateFailure :exec
INSERT INTO failures (
    channel_id, data, type
) VALUES (
    ?,          ?,    ?
)
`

type CreateFailureParams struct {
	ChannelID string
	Data      string
	Type      string
}

func (q *Queries) CreateFailure(ctx context.Context, arg CreateFailureParams) error {
	_, err := q.db.ExecContext(ctx, createFailure, arg.ChannelID, arg.Data, arg.Type)
	return err
}

const createTranscript = `-- name: CreateTranscript :one
INSERT INTO transcripts (
    video_id, start, duration, text
) VALUES (
    ?,        ?,     ?,        ?
)
RETURNING id
`

type CreateTranscriptParams struct {
	VideoID  string
	Start    float64
	Duration float64
	Text     string
}

func (q *Queries) CreateTranscript(ctx context.Context, arg CreateTranscriptParams) (int64, error) {
	row := q.db.QueryRowContext(ctx, createTranscript,
		arg.VideoID,
		arg.Start,
		arg.Duration,
		arg.Text,
	)
	var id int64
	err := row.Scan(&id)
	return id, err
}

const createVideo = `-- name: CreateVideo :exec
INSERT INTO videos (
    id, channel_id, published_at, title, description, thumbnail_url, searchable_transcript, transcript_type
) VALUES (
    ?,  ?,          ?,            ?,     ?,           ?,             ?,                     ?
)
`

type CreateVideoParams struct {
	ID                   string
	ChannelID            string
	PublishedAt          time.Time
	Title                string
	Description          string
	ThumbnailUrl         string
	SearchableTranscript string
	TranscriptType       string
}

func (q *Queries) CreateVideo(ctx context.Context, arg CreateVideoParams) error {
	_, err := q.db.ExecContext(ctx, createVideo,
		arg.ID,
		arg.ChannelID,
		arg.PublishedAt,
		arg.Title,
		arg.Description,
		arg.ThumbnailUrl,
		arg.SearchableTranscript,
		arg.TranscriptType,
	)
	return err
}

const deleteFailure = `-- name: DeleteFailure :exec
DELETE FROM failures
WHERE id = ?
`

func (q *Queries) DeleteFailure(ctx context.Context, id int64) error {
	_, err := q.db.ExecContext(ctx, deleteFailure, id)
	return err
}

const lastVideo = `-- name: LastVideo :one
SELECT id, channel_id, published_at, title, description, thumbnail_url, searchable_transcript, created_at, updated_at, transcript_type FROM videos
WHERE channel_id = ?
ORDER BY published_at
DESC LIMIT 1
`

func (q *Queries) LastVideo(ctx context.Context, channelID string) (Video, error) {
	row := q.db.QueryRowContext(ctx, lastVideo, channelID)
	var i Video
	err := row.Scan(
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
	)
	return i, err
}

const nextFailure = `-- name: NextFailure :one
SELECT id, channel_id, data, type, created_at, updated_at FROM failures
WHERE id > ?
AND type = ?
ORDER BY id ASC
LIMIT 1
`

type NextFailureParams struct {
	ID   int64
	Type string
}

func (q *Queries) NextFailure(ctx context.Context, arg NextFailureParams) (Failure, error) {
	row := q.db.QueryRowContext(ctx, nextFailure, arg.ID, arg.Type)
	var i Failure
	err := row.Scan(
		&i.ID,
		&i.ChannelID,
		&i.Data,
		&i.Type,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const noCaptionFailures = `-- name: NoCaptionFailures :many
SELECT id, channel_id, data, type, created_at, updated_at FROM failures
WHERE channel_id = ?
AND type = "no_captions"
`

func (q *Queries) NoCaptionFailures(ctx context.Context, channelID string) ([]Failure, error) {
	rows, err := q.db.QueryContext(ctx, noCaptionFailures, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Failure
	for rows.Next() {
		var i Failure
		if err := rows.Scan(
			&i.ID,
			&i.ChannelID,
			&i.Data,
			&i.Type,
			&i.CreatedAt,
			&i.UpdatedAt,
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

const transcript = `-- name: Transcript :one
SELECT id, video_id, start, duration, text, created_at, updated_at FROM transcripts
WHERE id = ?
`

func (q *Queries) Transcript(ctx context.Context, id int64) (Transcript, error) {
	row := q.db.QueryRowContext(ctx, transcript, id)
	var i Transcript
	err := row.Scan(
		&i.ID,
		&i.VideoID,
		&i.Start,
		&i.Duration,
		&i.Text,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const video = `-- name: Video :one

SELECT id, channel_id, published_at, title, description, thumbnail_url, searchable_transcript, created_at, updated_at, transcript_type FROM videos
WHERE id = ?
`

// Need second arg here because type is a reserved word in go.
func (q *Queries) Video(ctx context.Context, id string) (Video, error) {
	row := q.db.QueryRowContext(ctx, video, id)
	var i Video
	err := row.Scan(
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
	)
	return i, err
}

const videosOfChannel = `-- name: VideosOfChannel :many
SELECT id, channel_id, published_at, title, description, thumbnail_url, searchable_transcript, created_at, updated_at, transcript_type FROM videos
WHERE channel_id = ?
`

func (q *Queries) VideosOfChannel(ctx context.Context, channelID string) ([]Video, error) {
	rows, err := q.db.QueryContext(ctx, videosOfChannel, channelID)
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
