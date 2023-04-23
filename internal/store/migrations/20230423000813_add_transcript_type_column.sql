-- +goose Up
ALTER TABLE videos
ADD transcript_type VARCHAR(25) NOT NULL DEFAULT 'tube_auto';

-- +goose Down
ALTER TABLE videos
DROP column transcript_type;
