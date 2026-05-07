---
name: sqlc-query-pattern
description: Use this skill when adding or modifying SQL queries, repository methods, or database schema in the VisionLoop Go API. The project rule is no raw SQL string interpolation — all queries go through sqlc-generated code. Covers two cases: (1) query/repo work only — write SQL in the queries directory and run sqlc generate; (2) schema changes — write a migration first, then write the query. Triggers when the task involves database queries, repository methods, new tables, new columns, or schema migrations.
---

# sqlc Query Pattern

VisionLoop uses sqlc for all database access. Raw SQL string
interpolation is prohibited by CLAUDE.md project rule.

## Case A — Query or repo change only (no schema change)

Use this when adding or modifying a query against an existing table.

### Steps

1. **Add or modify the SQL file** in
   `apps/api/internal/db/queries/{table}.sql`

   ```sql
   -- name: GetImageByID :one
   SELECT * FROM images
   WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

   -- name: ListImagesByOrg :many
   SELECT * FROM images
   WHERE org_id = $1 AND deleted_at IS NULL
   ORDER BY created_at DESC
   LIMIT $2 OFFSET $3;
   ```

2. **Run sqlc generate** from `apps/api/`:
   ```bash
   sqlc generate
   ```

3. **Use the generated Go functions** in repository
   implementations under
   `apps/api/internal/infrastructure/repo/`.

---

## Case B — Schema change (new table or new column)

Use this when the task requires adding a table or column that does not exist yet.

### Steps

1. **Write the migration** in
   `apps/api/internal/db/migrations/`:
   ```
   NNN_describe_change.up.sql
   NNN_describe_change.down.sql
   ```
   Migration number must be next in sequence. The down migration
   must fully reverse the up migration.

2. **Run the migration locally** to verify it applies cleanly:
   ```bash
   go run ./cmd/migrate up
   ```

3. **Add or update the SQL query file** in
   `apps/api/internal/db/queries/{table}.sql`
   (same as Case A step 1).

4. **Run sqlc generate**:
   ```bash
   sqlc generate
   ```

5. **Use the generated Go functions** in the repository.

---

## Rules

- **Always include org_id in WHERE clause** for every tenant-scoped table.
- **Always include deleted_at IS NULL** in queries that list active resources,
  unless explicitly listing deleted ones.
- **Use named parameters** (`sqlc.arg(name)`) when the same parameter appears
  more than once in a query. This prevents the Postgres type-inference
  ambiguity error that caused a Phase 2 failure.
- **Cast enum types explicitly** when used in CASE/WHEN branches:
  `state::job_state`. Uncast enums produced SQLSTATE 42P08 in Phase 2.

## sqlc return type annotations

| Annotation | Returns |
|------------|---------|
| `:one` | exactly one row; error if zero or multiple |
| `:many` | slice of rows |
| `:exec` | nothing |
| `:execrows` | affected row count |
| `:execresult` | sql.Result |

## Forbidden patterns

```go
// ❌ NEVER — raw string query
db.Query("SELECT * FROM images WHERE id = " + imageID)

// ❌ NEVER — even with Sprintf
db.Query(fmt.Sprintf("SELECT * FROM %s WHERE id = $1", tableName), id)

// ✅ ALWAYS — sqlc generated function
queries.GetImageByID(ctx, db.GetImageByIDParams{
    ID:    imageID,
    OrgID: orgID,
})
```

## Known technical debt

`apps/api/internal/infrastructure/repo/images.go` contains raw SQL that
predates the sqlc rule (Phase 1). This is **known debt scheduled for
Phase 8 cleanup**.

Rules for this file until it is refactored:
- Do **not** add new raw SQL queries to it.
- Do **not** use it as a pattern for new repository files.
- Any new query needed from the images table must go through sqlc
  in the standard location.

## Related

- `CLAUDE.md` — project rules including the no-raw-SQL mandate
- `docs/adr/0002-sqlc-no-raw-sql.md` — rationale for this choice
- `docs/spec/visionloop-spec.md` §3 — architecture overview
