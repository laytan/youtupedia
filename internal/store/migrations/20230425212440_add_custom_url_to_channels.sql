-- +goose Up
ALTER TABLE channels
ADD COLUMN custom_url VARCHAR(255);

-- +goose Down
ALTER TABLE channels DROP COLUMN custom_url;
