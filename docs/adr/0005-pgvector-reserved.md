# ADR-0005 — pgvector enabled in v1, embedding column added in v2

**Status:** Accepted (Phase 1)
**Date:** 2026-05-03

## Context

CLIP-based semantic image search is on the v2 roadmap. Section 18 of the
spec calls out a previous embedding-dimension mismatch. Enabling
`pgvector` later requires a maintenance window and a backfill; enabling
it on day one is free.

## Decision

Phase 1 migrations install `CREATE EXTENSION IF NOT EXISTS vector;`. The
`images` table reserves no `clip_embedding` column yet — we add it in v2
when the embedding model is selected, so the dimension matches the
chosen model exactly. The model dimension is validated at AI service
startup against a `model_versions.embedding_dim` column when populated;
mismatch is a fail-fast error.

## Consequences

**Positive.** No schema migration friction in v2. The fail-fast check
prevents the prior iteration's silent dimension mismatch.

**Negative.** Slightly larger Postgres image (pgvector ~1 MB). Acceptable.
