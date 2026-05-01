-- +goose Up
CREATE TABLE IF NOT EXISTS sla_definitions (
    state TEXT PRIMARY KEY,
    max_days INT NOT NULL CHECK (max_days >= 0)
);

INSERT INTO sla_definitions (state, max_days) VALUES
    ('docs_review',           3),
    ('interview_pending',     5),
    ('occ_sent',             10),
    ('hiring_pending',        3),
    ('induction_org',         5),
    ('induction_theory',      7),
    ('induction_epp_pending', 3),
    ('induction_practice',   15)
ON CONFLICT (state) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS sla_definitions;
