-- name: Channel :one
SELECT * FROM channels
WHERE id = ? LIMIT 1;

-- name: CreateChannel :one
INSERT INTO channels (
    id, title, videos_list_id, thumbnail_url
) VALUES (
    ?,  ?,     ?,              ?
)
RETURNING *;

-- name: LastVideo :one
SELECT * FROM videos
WHERE channel_id = ?
ORDER BY published_at
DESC LIMIT 1;

-- name: CreateTranscript :one
INSERT INTO transcripts (
    video_id, start, duration, text
) VALUES (
    ?,        ?,     ?,        ?
)
RETURNING id;

-- name: CreateVideo :exec
INSERT INTO videos (
    id, channel_id, published_at, title, description, thumbnail_url, searchable_transcript, transcript_type
) VALUES (
    ?,  ?,          ?,            ?,     ?,           ?,             ?,                     ?
);

-- name: VideosOfChannel :many
SELECT * FROM videos
WHERE channel_id = ?;

-- name: Transcript :one
SELECT * FROM transcripts
WHERE id = ?;

-- name: CreateFailure :exec
INSERT INTO failures (
    channel_id, data, type
) VALUES (
    ?,          ?,    ?
);

-- name: NoCaptionFailures :many
SELECT * FROM failures
WHERE channel_id = ?
AND type = "no_captions";

-- name: DeleteFailure :exec
DELETE FROM failures
WHERE id = ?;

-- name: Video :one
SELECT * FROM videos
WHERE id = ?;

