-- +goose Up
ALTER TABLE notifications ADD COLUMN deliver_after timestamptz;

-- +goose Down
ALTER TABLE notifications DROP COLUMN deliver_after;
