DROP TRIGGER IF EXISTS trg_users_updated ON users;
DROP TRIGGER IF EXISTS trg_organizations_updated ON organizations;
DROP FUNCTION IF EXISTS set_updated_at();
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS memberships;
DROP TYPE IF EXISTS membership_role;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS organizations;
