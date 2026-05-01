-- +goose Up
ALTER TABLE vacancies
    ADD COLUMN IF NOT EXISTS role_manual_body TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS role_manual_file_id UUID REFERENCES files(id);

-- +goose Down
ALTER TABLE vacancies
    DROP COLUMN IF EXISTS role_manual_file_id,
    DROP COLUMN IF EXISTS role_manual_body;
