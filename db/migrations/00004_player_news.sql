-- +goose Up
-- News is global per player; owner scoping happens through rosters on read.
CREATE TABLE player_news (
    source text NOT NULL CHECK (source <> ''),
    source_item_id text NOT NULL CHECK (source_item_id <> ''),
    player_id uuid NOT NULL REFERENCES players(id),
    headline text NOT NULL CHECK (btrim(headline) <> ''),
    summary text NOT NULL DEFAULT '',
    url text NOT NULL CHECK (url <> ''),
    source_label text NOT NULL CHECK (source_label <> ''),
    published_at timestamptz NOT NULL,
    fetched_at timestamptz NOT NULL,
    PRIMARY KEY (source, source_item_id, player_id)
);
CREATE INDEX player_news_player_idx ON player_news (player_id, published_at DESC);

-- +goose Down
DROP TABLE player_news;
