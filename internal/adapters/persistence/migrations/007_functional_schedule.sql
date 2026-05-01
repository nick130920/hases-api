-- +goose Up

-- Plantilla de actividades del cronograma funcional por vacante.
CREATE TABLE IF NOT EXISTS functional_activity_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vacancy_id UUID NOT NULL REFERENCES vacancies(id) ON DELETE CASCADE,
    phase TEXT NOT NULL CHECK (phase IN ('theory', 'practice')),
    sort_order INT NOT NULL DEFAULT 0,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    evidence_required BOOLEAN NOT NULL DEFAULT FALSE,
    audiovisual_file_id UUID REFERENCES files(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_functional_act_templates_vac
    ON functional_activity_templates (vacancy_id, phase, sort_order);

-- Snapshot por aplicación: lo concreto que el trabajador completa.
CREATE TABLE IF NOT EXISTS functional_activities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    template_id UUID REFERENCES functional_activity_templates(id) ON DELETE SET NULL,
    phase TEXT NOT NULL CHECK (phase IN ('theory', 'practice')),
    sort_order INT NOT NULL DEFAULT 0,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    evidence_required BOOLEAN NOT NULL DEFAULT FALSE,
    audiovisual_file_id UUID REFERENCES files(id),
    completed_at TIMESTAMPTZ NULL,
    completed_by UUID REFERENCES users(id),
    evidence_notes TEXT NOT NULL DEFAULT '',
    evidence_file_ids UUID[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_functional_activities_app
    ON functional_activities (application_id, phase, sort_order);

-- +goose Down
DROP TABLE IF EXISTS functional_activities;
DROP TABLE IF EXISTS functional_activity_templates;
