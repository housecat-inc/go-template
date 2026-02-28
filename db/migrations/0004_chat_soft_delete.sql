-- +goose Up
ALTER TABLE chats ADD COLUMN deleted_at TIMESTAMP;

-- +goose Down
ALTER TABLE chats DROP COLUMN deleted_at;
