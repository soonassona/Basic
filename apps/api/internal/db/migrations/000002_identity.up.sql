-- Identity domain: orgs, users, memberships, api keys, audit log.
-- Better Auth owns sessions and accounts (000003_better_auth.up.sql).

CREATE TABLE organizations (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug          TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    plan          TEXT NOT NULL DEFAULT 'free' CHECK (plan IN ('free', 'pro', 'enterprise')),
    cors_allowlist TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    settings      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);

CREATE INDEX idx_organizations_active ON organizations (id) WHERE deleted_at IS NULL;

CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL UNIQUE,
    email_verified  BOOLEAN NOT NULL DEFAULT FALSE,
    display_name    TEXT,
    avatar_url      TEXT,
    locale          TEXT NOT NULL DEFAULT 'en',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX idx_users_email_lower ON users (LOWER(email)) WHERE deleted_at IS NULL;

CREATE TYPE membership_role AS ENUM ('owner', 'admin', 'annotator', 'viewer');

CREATE TABLE memberships (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role        membership_role NOT NULL,
    invited_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, user_id)
);

CREATE INDEX idx_memberships_user ON memberships (user_id);
CREATE INDEX idx_memberships_org_role ON memberships (org_id, role);

CREATE TABLE api_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    created_by  UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    name        TEXT NOT NULL,
    -- bcrypt of the raw key; the raw key is shown to the user once at creation.
    key_hash    TEXT NOT NULL,
    -- last 4 chars of the raw key for display; never enough to authenticate.
    key_suffix  CHAR(4) NOT NULL,
    scopes      TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    last_used_at TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_api_keys_org ON api_keys (org_id) WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX idx_api_keys_hash ON api_keys (key_hash);

-- Append-only audit trail. Triggers in later migrations populate this for
-- every privileged mutation; for now the table is writable from the
-- application layer with `INSERT ONLY` enforced by a row-level revoke.
CREATE TABLE audit_log (
    id          BIGSERIAL PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    org_id      UUID,
    actor_id    UUID,
    actor_kind  TEXT NOT NULL CHECK (actor_kind IN ('user', 'api_key', 'system')),
    action      TEXT NOT NULL,
    resource    TEXT NOT NULL,
    resource_id UUID,
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
    request_id  TEXT,
    trace_id    TEXT
);

CREATE INDEX idx_audit_org_time ON audit_log (org_id, occurred_at DESC);
CREATE INDEX idx_audit_resource ON audit_log (resource, resource_id);

-- Audit log immutability: revoke UPDATE/DELETE for the application role.
-- The migration role keeps full access for retention housekeeping (Phase 8).
REVOKE UPDATE, DELETE ON audit_log FROM PUBLIC;

-- Helper trigger: keep updated_at fresh.
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_organizations_updated BEFORE UPDATE ON organizations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER trg_users_updated BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
