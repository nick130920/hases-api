-- +goose Up

-- Catálogo editable de motivos de rechazo (ya existía en 001 con SERIAL/UNIQUE label)
-- Tipos de documento reutilizables
CREATE TABLE IF NOT EXISTS document_types (
    id SERIAL PRIMARY KEY,
    item_key TEXT NOT NULL UNIQUE,
    label TEXT NOT NULL,
    requires_vehicle BOOLEAN NOT NULL DEFAULT FALSE,
    typical_required BOOLEAN NOT NULL DEFAULT TRUE
);

INSERT INTO document_types (item_key, label, requires_vehicle, typical_required) VALUES
    ('hv',         'Hoja de vida con foto (formato)',     FALSE, TRUE),
    ('cedula',     'Fotocopia cedula 150%',               FALSE, TRUE),
    ('proc',       'Antecedentes Procuraduria',           FALSE, TRUE),
    ('cura',       'Antecedentes Curaduria',              FALSE, TRUE),
    ('pol',        'Antecedentes Policia Nacional',       FALSE, TRUE),
    ('lic_cond',   'Licencia de conduccion',              TRUE,  FALSE),
    ('lic_trans',  'Licencia de transito',                TRUE,  FALSE),
    ('banco',      'Certificado bancario (max 30 dias)',  FALSE, TRUE),
    ('eps',        'Certificado afiliacion EPS',          FALSE, TRUE),
    ('pension',    'Certificado fondo pensional',         FALSE, TRUE),
    ('estudio',    'Certificado de estudios (opcional)',  FALSE, FALSE),
    ('laboral',    'Certificados laborales',              FALSE, TRUE)
ON CONFLICT DO NOTHING;

-- Vacancies: campo para datos de cargo y fecha de cierre opcional
ALTER TABLE vacancies
    ADD COLUMN IF NOT EXISTS closed_at TIMESTAMPTZ;

-- Notas en applications (texto largo opcional, libre)
ALTER TABLE applications
    ADD COLUMN IF NOT EXISTS notes TEXT NOT NULL DEFAULT '';

-- EPP: marcar firma del formato (file_id existente en signature_file_id)
-- Functional evidence: ya tiene file_ids[]; nada que hacer aquí (lectura/escritura desde API).

-- Sembrar motivos de rechazo comunes
INSERT INTO rejection_reasons (label) VALUES
    ('Documentación incompleta'),
    ('No cumple perfil'),
    ('Resultado IPS no apto'),
    ('Decisión empleador')
ON CONFLICT (label) DO NOTHING;

-- +goose Down
ALTER TABLE applications DROP COLUMN IF EXISTS notes;
ALTER TABLE vacancies DROP COLUMN IF EXISTS closed_at;
DROP TABLE IF EXISTS document_types;
