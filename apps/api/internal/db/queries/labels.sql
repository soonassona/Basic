-- Label catalog (spec §10 — picker source). Per-org, soft-archive via
-- the `archived` boolean. The studio reads this list to populate its
-- label dropdown; bootstrap data is currently inserted via SQL until a
-- CRUD admin surface lands.

-- name: ListLabelsByOrg :many
-- Returns all non-archived labels for an org, alphabetised by name so
-- the picker has a stable order. ID, name, color are everything the
-- studio renders today; description ships for future tooltips.
SELECT id, org_id, name, color, description, archived, created_at
FROM labels
WHERE org_id = $1 AND archived = FALSE
ORDER BY name ASC;
