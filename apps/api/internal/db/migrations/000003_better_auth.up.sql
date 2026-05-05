-- Better Auth tables. The Postgres adapter on the web app writes here;
-- the Go API reads `better_auth_session` to authenticate requests
-- (ADR-0003).
--
-- Column names match Better Auth 1.x's default snake_case table option.

CREATE TABLE better_auth_account (
    id                       TEXT PRIMARY KEY,
    user_id                  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id               TEXT NOT NULL,
    provider_id              TEXT NOT NULL,
    access_token             TEXT,
    refresh_token            TEXT,
    id_token                 TEXT,
    access_token_expires_at  TIMESTAMPTZ,
    refresh_token_expires_at TIMESTAMPTZ,
    scope                    TEXT,
    password                 TEXT,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider_id, account_id)
);

CREATE INDEX idx_account_user ON better_auth_account (user_id);

CREATE TABLE better_auth_session (
    id          TEXT PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token       TEXT NOT NULL UNIQUE,
    ip_address  TEXT,
    user_agent  TEXT,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_session_user ON better_auth_session (user_id);
CREATE INDEX idx_session_expiry ON better_auth_session (expires_at);

CREATE TABLE better_auth_verification (
    id          TEXT PRIMARY KEY,
    identifier  TEXT NOT NULL,
    value       TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_verification_identifier ON better_auth_verification (identifier);
