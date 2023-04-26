-- +goose Up
ALTER TABLE channels
ALTER COLUMN custom_url
SET NOT NULL;

-- +goose Down
ALTER TABLE channels
ALTER COLUMN custom_url
DROP NOT NULL;
