-- +goose Up

ALTER TABLE application_documents
    ADD COLUMN IF NOT EXISTS issued_at TIMESTAMPTZ NULL;

ALTER TABLE document_types
    ADD COLUMN IF NOT EXISTS max_age_days INT NULL,
    ADD COLUMN IF NOT EXISTS requires_template BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS requires_issued_at BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE document_types SET max_age_days = 30, requires_issued_at = TRUE WHERE item_key = 'banco';
UPDATE document_types SET requires_template = TRUE WHERE item_key = 'hv';

-- Foto separada del CV (formato estricto solicitado por RRHH)
INSERT INTO document_types (item_key, label, requires_vehicle, typical_required, requires_template)
VALUES ('foto', 'Fotografia formato estricto', FALSE, TRUE, TRUE)
ON CONFLICT (item_key) DO NOTHING;

CREATE TABLE IF NOT EXISTS document_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    item_key TEXT NOT NULL UNIQUE,
    label TEXT NOT NULL,
    file_id UUID NOT NULL REFERENCES files(id),
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Sembrar el item `foto` y `hv` en plantillas existentes para que las nuevas vacantes lo incluyan.
DO $$
DECLARE
    t RECORD;
BEGIN
    FOR t IN SELECT id FROM checklist_templates LOOP
        INSERT INTO checklist_items (template_id, sort_order, item_key, label, required, requires_vehicle)
        VALUES (t.id, 13, 'foto', 'Fotografia formato estricto', TRUE, FALSE)
        ON CONFLICT (template_id, item_key) DO NOTHING;
    END LOOP;
END $$;

-- +goose Down
DROP TABLE IF EXISTS document_templates;
DELETE FROM document_types WHERE item_key = 'foto';
ALTER TABLE document_types
    DROP COLUMN IF EXISTS requires_issued_at,
    DROP COLUMN IF EXISTS requires_template,
    DROP COLUMN IF EXISTS max_age_days;
ALTER TABLE application_documents DROP COLUMN IF EXISTS issued_at;
