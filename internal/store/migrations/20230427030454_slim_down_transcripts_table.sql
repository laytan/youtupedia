-- +goose Up

-- Other space todo's:
-- TODO: 1. Use integers as ids for videos.
-- TODO: 2. Group transcripts together on specific interval (10secs?) (only for auto yt subs).

-- Drop unused columns to save space in the db, this adds up when you got millions of rows.
ALTER TABLE transcripts DROP COLUMN updated_at;
ALTER TABLE transcripts DROP COLUMN created_at;
ALTER TABLE transcripts DROP COLUMN duration;

-- Flooring to the second is fine for the start time, also saves space.
ALTER TABLE transcripts ALTER COLUMN start type INTEGER;

-- +goose Down
ALTER TABLE transcripts ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL;
ALTER TABLE transcripts ADD COLUMN updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL;
ALTER TABLE transcripts ADD COLUMN duration REAL NOT NULL;

ALTER TABLE transcripts ALTER COLUMN start type DOUBLE PRECISION;
