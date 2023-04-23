-- +goose Up

CREATE TABLE IF NOT EXISTS channels (
    id             VARCHAR(255) NOT NULL PRIMARY KEY,
    title          VARCHAR(255) NOT NULL,
    videos_list_id VARCHAR(255) NOT NULL,
    thumbnail_url  VARCHAR(255) NOT NULL,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS videos (
    id                    VARCHAR(255) NOT NULL PRIMARY KEY,
    channel_id            VARCHAR(255) NOT NULL,
    published_at          DATETIME NOT NULL,
    title                 VARCHAR(255) NOT NULL,
    description           VARCHAR(255) NOT NULL,
    thumbnail_url         VARCHAR(255) NOT NULL,
    searchable_transcript TEXT NOT NULL,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL,

    FOREIGN KEY (channel_id) REFERENCES channels (channel_id) ON UPDATE CASCADE ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS channel_id ON videos(channel_id);

CREATE TABLE IF NOT EXISTS transcripts (
    id       INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
    video_id VARCHAR(255) NOT NULL,
    start    DECIMAL(10, 2) NOT NULL,
    duration DECIMAL(10, 2) NOT NULL,
    text     TEXT NOT NULL,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL,

    FOREIGN KEY (video_id) REFERENCES videos (video_id) ON UPDATE CASCADE ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS failures (
    id         INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
    channel_id VARCHAR(255) NOT NULL,
    data       TEXT NOT NULL, -- Data dependant on type.
    type       VARCHAR(25) NOT NULL,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL,

    FOREIGN KEY (channel_id) REFERENCES channels (channel_id) ON UPDATE CASCADE ON DELETE CASCADE
)

-- +goose Down

DROP INDEX channel_id;

DROP TABLE failures;

DROP TABLE transcripts;

DROP TABLE videos;

DROP TABLE channels;
