-- name: Channel :one
SELECT * FROM channels
WHERE id = $1
LIMIT 1;

-- name: Channels :many
SELECT * FROM channels;

-- name: CreateChannel :one
INSERT INTO channels (
    id, title, videos_list_id, thumbnail_url
) VALUES (
    $1, $2,    $3,             $4
)
RETURNING *;

-- name: LastVideo :one
SELECT * FROM videos
WHERE channel_id = $1
ORDER BY published_at
DESC LIMIT 1;

-- name: CreateTranscript :one
INSERT INTO transcripts (
    video_id, start, duration, text
) VALUES (
    $1,        $2,     $3,        $4
)
RETURNING id;

-- name: CreateVideo :exec
INSERT INTO videos (
    id, channel_id, published_at, title, description, thumbnail_url, searchable_transcript, transcript_type
) VALUES (
    $1,  $2,          $3,            $4,     $5,           $6,             $7,                     $8
);

-- name: VideosOfChannel :many
SELECT * FROM videos
WHERE channel_id = $1;

-- name: Transcript :one
SELECT * FROM transcripts
WHERE id = $1;

-- name: CreateFailure :exec
INSERT INTO failures (
    channel_id, data, type
) VALUES (
    $1,          $2,    $3
);

-- name: NoCaptionFailures :many
SELECT * FROM failures
WHERE channel_id = $1
AND type = "no_captions";

-- name: DeleteFailure :exec
DELETE FROM failures
WHERE id = $1;

-- name: NextFailure :one
SELECT * FROM failures
WHERE id > $1
AND type = $2
ORDER BY id ASC
LIMIT 1;

-- name: CountFailures :one
SELECT COUNT(*) FROM failures
WHERE type = $1
AND id > $2; -- Need second arg here because type is a reserved word in go.

-- name: Video :one
SELECT * FROM videos
WHERE id = $1;

-- name: TranscriptsByIds :many
SELECT * FROM transcripts
WHERE id = ANY(@ids::bigint[]);

-- name: SetSearchableTranscript :exec
UPDATE videos
SET searchable_transcript = $2
WHERE id = $1;
