-- +goose Up

-- Ampliar el rol con `worker` (postulante / nuevo trabajador con cuenta propia).
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (
    role IN ('admin', 'hr', 'evaluator', 'hiring_manager', 'worker')
);

-- Vincula una postulación con el usuario que la opera desde el portal.
CREATE TABLE IF NOT EXISTS application_user_links (
    application_id UUID PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    invited_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    invitation_token TEXT NULL,
    invitation_expires_at TIMESTAMPTZ NULL,
    accepted_at TIMESTAMPTZ NULL
);
CREATE INDEX IF NOT EXISTS idx_app_user_links_token
    ON application_user_links (invitation_token)
    WHERE invitation_token IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS application_user_links;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (
    role IN ('admin', 'hr', 'evaluator', 'hiring_manager')
);
