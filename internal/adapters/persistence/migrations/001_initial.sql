-- +goose Up

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    full_name TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('admin', 'hr', 'evaluator', 'hiring_manager')),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE files (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    storage_key TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    byte_size BIGINT NOT NULL,
    original_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE company_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    legal_name TEXT NOT NULL DEFAULT '',
    tax_id TEXT DEFAULT '',
    logo_file_id UUID REFERENCES files(id),
    default_sender_email TEXT DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO company_settings (id, legal_name) VALUES (1, 'Mi empresa') ON CONFLICT DO NOTHING;

CREATE TABLE audit_logs (
    id BIGSERIAL PRIMARY KEY,
    actor_user_id UUID REFERENCES users(id),
    entity_type TEXT NOT NULL,
    entity_id UUID,
    action TEXT NOT NULL,
    payload JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE rejection_reasons (
    id SERIAL PRIMARY KEY,
    label TEXT NOT NULL UNIQUE
);

CREATE TABLE checklist_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL
);

CREATE TABLE checklist_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id UUID NOT NULL REFERENCES checklist_templates(id) ON DELETE CASCADE,
    sort_order INT NOT NULL DEFAULT 0,
    item_key TEXT NOT NULL,
    label TEXT NOT NULL,
    required BOOLEAN NOT NULL DEFAULT TRUE,
    requires_vehicle BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE(template_id, item_key)
);

CREATE TABLE vacancies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    description TEXT DEFAULT '',
    requirements TEXT DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('draft', 'published', 'closed')) DEFAULT 'draft',
    public_slug TEXT NOT NULL UNIQUE,
    published_at TIMESTAMPTZ,
    checklist_template_id UUID REFERENCES checklist_templates(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE applications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vacancy_id UUID NOT NULL REFERENCES vacancies(id),
    status TEXT NOT NULL,
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    email TEXT NOT NULL,
    phone TEXT DEFAULT '',
    channel TEXT DEFAULT '',
    cv_reference TEXT DEFAULT '',
    requires_vehicle BOOLEAN NOT NULL DEFAULT FALSE,
    discarded_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_applications_vacancy ON applications(vacancy_id);
CREATE INDEX idx_applications_status ON applications(status);

CREATE TABLE application_documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    checklist_item_id UUID NOT NULL REFERENCES checklist_items(id),
    file_id UUID REFERENCES files(id),
    review_status TEXT NOT NULL CHECK (review_status IN ('pending', 'approved', 'rejected')) DEFAULT 'pending',
    reviewer_notes TEXT DEFAULT '',
    reviewed_by UUID REFERENCES users(id),
    reviewed_at TIMESTAMPTZ,
    UNIQUE(application_id, checklist_item_id)
);

CREATE TABLE interview_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vacancy_id UUID REFERENCES vacancies(id) ON DELETE SET NULL,
    title TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE interview_questions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id UUID NOT NULL REFERENCES interview_templates(id) ON DELETE CASCADE,
    section_name TEXT DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    question_text TEXT NOT NULL,
    answer_type TEXT NOT NULL CHECK (answer_type IN ('boolean', 'scale', 'text')) DEFAULT 'text'
);

CREATE TABLE interview_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    template_id UUID NOT NULL REFERENCES interview_templates(id),
    scheduled_at TIMESTAMPTZ,
    location TEXT DEFAULT '',
    modality TEXT DEFAULT '',
    interviewer_notes TEXT DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE interview_responses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES interview_sessions(id) ON DELETE CASCADE,
    question_id UUID NOT NULL REFERENCES interview_questions(id) ON DELETE CASCADE,
    answer_json JSONB NOT NULL DEFAULT '{}',
    UNIQUE(session_id, question_id)
);

CREATE TABLE occupational_orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    sent_at TIMESTAMPTZ,
    email_to TEXT DEFAULT '',
    pdf_file_id UUID REFERENCES files(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE ips_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    outcome TEXT NOT NULL CHECK (outcome IN ('fit', 'unfit', 'fit_restrictions')),
    recommendations TEXT DEFAULT '',
    attachment_file_id UUID REFERENCES files(id),
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE induction_org_modules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0
);

CREATE TABLE induction_org_progress (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    module_id UUID NOT NULL REFERENCES induction_org_modules(id) ON DELETE CASCADE,
    completed_at TIMESTAMPTZ,
    validated_by UUID REFERENCES users(id),
    UNIQUE(application_id, module_id)
);

CREATE TABLE induction_signatures (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('regulation', 'policies', 'contract')),
    signature_file_id UUID REFERENCES files(id),
    signed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    metadata JSONB DEFAULT '{}'
);

CREATE TABLE functional_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id UUID NOT NULL UNIQUE REFERENCES applications(id) ON DELETE CASCADE,
    manual_summary TEXT DEFAULT '',
    theory_completed_at TIMESTAMPTZ,
    practice_started_at TIMESTAMPTZ,
    practice_completed_at TIMESTAMPTZ,
    onboarding_completed_at TIMESTAMPTZ
);

CREATE TABLE epp_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id UUID NOT NULL UNIQUE REFERENCES applications(id) ON DELETE CASCADE,
    items_json JSONB NOT NULL DEFAULT '[]',
    delivered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    signature_file_id UUID REFERENCES files(id)
);

CREATE TABLE functional_evidence (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    phase TEXT NOT NULL CHECK (phase IN ('theory', 'practice')),
    notes TEXT DEFAULT '',
    actor TEXT DEFAULT 'worker',
    file_ids UUID[] DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS functional_evidence;
DROP TABLE IF EXISTS epp_deliveries;
DROP TABLE IF EXISTS functional_plans;
DROP TABLE IF EXISTS induction_signatures;
DROP TABLE IF EXISTS induction_org_progress;
DROP TABLE IF EXISTS induction_org_modules;
DROP TABLE IF EXISTS ips_results;
DROP TABLE IF EXISTS occupational_orders;
DROP TABLE IF EXISTS interview_responses;
DROP TABLE IF EXISTS interview_sessions;
DROP TABLE IF EXISTS interview_questions;
DROP TABLE IF EXISTS interview_templates;
DROP TABLE IF EXISTS application_documents;
DROP TABLE IF EXISTS applications;
DROP TABLE IF EXISTS vacancies;
DROP TABLE IF EXISTS checklist_items;
DROP TABLE IF EXISTS checklist_templates;
DROP TABLE IF EXISTS rejection_reasons;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS company_settings;
DROP TABLE IF EXISTS files;
DROP TABLE IF EXISTS users;
