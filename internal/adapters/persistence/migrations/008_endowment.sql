-- +goose Up

-- endowment_deliveries unifica EPP y Dotación, distinguidos por `kind`.
CREATE TABLE IF NOT EXISTS endowment_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('epp', 'dotacion')),
    items_json JSONB NOT NULL DEFAULT '[]',
    delivered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    signature_file_id UUID REFERENCES files(id),
    UNIQUE (application_id, kind)
);

INSERT INTO endowment_deliveries (id, application_id, kind, items_json, delivered_at, signature_file_id)
SELECT id, application_id, 'epp', items_json, delivered_at, signature_file_id
FROM epp_deliveries
ON CONFLICT (application_id, kind) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS endowment_deliveries;
