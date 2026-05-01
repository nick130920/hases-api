-- +goose Up
CREATE TABLE IF NOT EXISTS induction_org_media (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    module_id UUID NOT NULL REFERENCES induction_org_modules(id) ON DELETE CASCADE,
    file_id UUID NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('video', 'image', 'pdf', 'audio')),
    title TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    duration_seconds INT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_induction_media_module ON induction_org_media (module_id, sort_order);

ALTER TABLE induction_org_progress
    ADD COLUMN IF NOT EXISTS viewed_seconds INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_viewed_at TIMESTAMPTZ NULL;

-- +goose Down
ALTER TABLE induction_org_progress
    DROP COLUMN IF EXISTS last_viewed_at,
    DROP COLUMN IF EXISTS viewed_seconds;
DROP TABLE IF EXISTS induction_org_media;
