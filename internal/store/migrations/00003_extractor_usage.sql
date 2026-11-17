-- +goose Up
CREATE TABLE extractor_usage (
    usage_day date PRIMARY KEY,
    tokens integer NOT NULL,
    CONSTRAINT extractor_usage_tokens_check CHECK (tokens >= 0)
);

-- +goose Down
DROP TABLE extractor_usage;
