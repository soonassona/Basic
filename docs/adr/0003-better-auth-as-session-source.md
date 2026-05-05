# ADR-0003 — Better Auth as the single source of session truth

**Status:** Accepted (Phase 1)
**Date:** 2026-05-03

## Context

The previous iteration declared an auth library in `package.json` but
never integrated it (section 18). To prevent recurrence, Phase 1's exit
criterion explicitly requires an end-to-end signup test on day one.

The architectural question: where does session validation happen for the
Go API? Options were (a) duplicate JWT validation in Go, (b) call back
to the Next.js auth endpoint per request, (c) share the database-backed
session table.

## Decision

**Better Auth 1.x** owns the session table in Postgres. The Next.js app
uses the Better Auth Postgres adapter directly. The Go API validates
sessions by looking up the session cookie value against the same table
(`better_auth_sessions`) — no extra round-trip, no duplicated JWT logic.
Cookie name and signing secret are shared via `BETTER_AUTH_SECRET`.

Signup provisions an organisation and an `owner` membership atomically
relative to the user's first authenticated request:

1. Better Auth commits the user row.
2. A post-signup hook attempts to insert `organizations` + `memberships`
   in a single Postgres transaction.
3. If that hook fails (transient DB error, cold start, etc.), the
   middleware on the first authenticated page hit calls
   `ensureOrganization`, which is idempotent — it returns the existing
   org if a membership is already present.

The combination guarantees that **no authenticated request can observe
a user without a membership**: the middleware blocks the dashboard until
provisioning succeeds. Better Auth's user-insert transaction is owned
by the library, so we cannot literally extend it; the retry layer is the
honest substitute.

## Consequences

**Positive.** One source of truth for sessions. The Go API can revoke a
session by deleting a row. OAuth providers (Google, GitHub) drop in via
Better Auth without touching API code.

**Negative.** Both services need `DATABASE_URL` access. Acceptable since
they already share the same Postgres for tenant data.
