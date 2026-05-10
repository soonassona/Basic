# VisionLoop — Phase Tracker

The full product specification lives in `docs/spec/visionloop-spec.md`.
`CLAUDE.md` at the repo root summarizes agent context and points back to
this file and the spec. This document tracks **what is built**, **what is
next**, and the open issues we are deliberately deferring.

---

## Phase 1 — Foundation ✅ done · verified end-to-end

Exit criteria from the spec: *"end-to-end signup and upload flow, all CI green."*

| Item                                                     | Status |
| -------------------------------------------------------- | ------ |
| Bun-workspace monorepo (`apps/{web,api,ai-service}`)     | ✅ |
| Postgres 17 + pgvector 0.8 schema, 4 migrations          | ✅ |
| sqlc 1.27 generation, output committed                   | ✅ |
| Better Auth 1.1 wired to existing `users` table          | ✅ |
| Org + owner-membership provisioned on signup             | ✅ |
| Go API verifies HMAC-signed Better Auth cookie           | ✅ |
| R2/MinIO presigned PUT + finalize cycle                  | ✅ |
| Tenant isolation enforced at SQL layer (org_id WHERE)    | ✅ |
| RBAC (owner/admin/annotator/viewer) on image writes      | ✅ |
| File validation: MIME allowlist + 50 MB cap → 413/415    | ✅ |
| Structured JSON logging (timestamp, level, request_id)   | ✅ |
| Security headers (HSTS, X-Frame-Options, nosniff, CSP)   | ✅ |
| GitHub Actions CI (lint/typecheck/test/build/Trivy)      | ✅ |
| docker-compose.yml + override for non-default ports      | ✅ |
| 5 ADRs documenting load-bearing decisions                | ✅ |
| Playwright E2E spec (`tests/e2e/signup-and-upload`)      | ✅ |

### Bugs caught by booting the real stack (would have shipped to Phase 2)

1. **Better Auth defaults to `user` (singular) + camelCase columns.** Our
   schema is `users` snake_case. Fixed by `modelName` + `fields` overrides
   in `apps/web/src/lib/auth.ts`.
2. **`generateId` lives at `advanced.generateId`, not `advanced.database.generateId`.**
   The default 32-char base32 string was leaking into our UUID column.
   The TS types accept both shapes, so typecheck did not catch it.
3. **Better Auth signs cookies as `urlEncode(token + "." + base64(HMAC))`**
   using *standard* base64 (not URL-safe). The Go API's session lookup was
   reading the raw cookie value as the token. Fixed in
   `apps/api/internal/infrastructure/auth/sessions.go` with a 5-test
   verifier (`sessions_test.go`).

### Bugs caught by booting the real stack (Phase 2)

1. **`ApplyJobCallback` failed at runtime with `inconsistent types
   deduced for parameter $2` (SQLSTATE 42P08).** The `state` parameter
   was used as the `job_state` enum in the SET clause and as text in the
   `CASE WHEN ... = 'running'` branches, so Postgres could not unify the
   parameter's type during prepare. Fixed by casting every reference to
   `::job_state` (named via `sqlc.arg(state)` so the generated Go field
   stays `State`). Caught only when the testcontainers e2e ran the real
   prepared statement against Postgres — the handler's mocked-repo unit
   test never exercised the SQL. The Makefile's `test-integration` glob
   was previously `./internal/infrastructure/...`, which excluded
   `./internal/integration/`; now both are run.

---

## Phase 2 — Core API · in progress

Exit criteria: *"job submitted, processed, status streamed via SSE."*

| Task                                                              | Status |
| ----------------------------------------------------------------- | ------ |
| RabbitMQ topology: `jobs` exchange, `jobs.pending.*` keys, DLQ    | ✅ |
| `POST /v1/jobs` with SHA-256 dedup window (10 min)                | ✅ |
| `GET /v1/jobs/:id` (single read)                                  | ✅ |
| `GET /v1/jobs/:id/events` (SSE with `Last-Event-ID`)              | ✅ |
| `POST /internal/jobs/callback` (HMAC-authenticated worker hook)   | ✅ |
| AI service: Celery worker consumes RabbitMQ, posts callback      | ✅ |
| Retry policy: 3 attempts, exponential backoff (1s/4s/16s)         | ✅ |
| `PATCH /v1/annotations/:id` with `If-Match` → 409 on stale        | ✅ |
| `GET /v1/annotation-sets/:image_id` returning ETag                | ✅ |
| Integration test: real RabbitMQ + Postgres via testcontainers     | ✅ |
| OpenAPI 3.1 spec (`docs/api/openapi.yaml`) covering Phase 1 + 2  | ✅ |
| Pact consumer-driven contracts (web ↔ api)                        | ✅ |

---

## Phase 3 — AI Integration

Exit criteria: *"all job types return masks for test images."*

- [x] SAM 2.1 (`sam2.1_hiera_large`) loaded as thread-safe singleton
- [x] YOLOv11x loaded, used for `auto` and `detect` jobs
- [x] Model router: `auto` → YOLO → SAM, `box`/`points` → SAM, `polygon` → manual
- [x] ONNX export pipeline (CPU inference fallback)
- [x] Worker callback writes mask key + `ai_score` + `model_used` on annotation
- [x] Lazy model load for tests; small fixture ONNX in `apps/ai-service/tests/fixtures/`

## Phase 4 — Annotation Studio

Exit criteria: *"annotator completes review-correct-save workflow."*

- [x] Konva.js canvas at ≥70% viewport
- [ ] Tools: select / bbox / point prompt / polygon / auto-segment *(select + bbox done; point / polygon / auto-segment pending Slice B4)*
- [ ] Mask overlay rendered as PNG composite per label color *(deferred to B4 — requires AI worker to emit mask URLs)*
- [x] Command-pattern undo/redo (depth 50)
- [x] Keyboard shortcuts: A / R / L / D / Z / Shift+Z / Esc / ←→ (all or nothing)
- [x] Autosave debounced 2000ms, ETag/If-Match conflict resolution UI
- [ ] Label picker, no localStorage for primary state *(no localStorage rule honored; dropdown UI pending B4)*

### Stage A — scaffold (commit c1ca83a)

(studio) route group, Konva canvas, tool picker, sidebar, studio-store,
API client (getAnnotationSet / patchAnnotation), STORAGE_PUBLIC_URL env,
next.config webpack alias for Konva. 5/5 vitest. Prod build clean.

### Slice B1 — bbox drawing + history (commit ae4f065)

studio-store extended with command stack (depth 50: create/update/delete),
local annotation buffer keyed by id, undo/redo. Canvas wires draw-bbox +
drag-to-move; sidebar gets undo/redo/delete buttons. 13 store tests.

### Slice B2 — keyboard shortcuts (commit 8be2e38)

Full 8-shortcut bundle (A/R/L/D/Z/Shift+Z/Esc/←→) shipped all-at-once per
CLAUDE.md rule. Guarded against input focus. Pure shortcutFor() mapping
exported for testing. 28 shortcut tests.

### Slice B3 — autosave + 409 conflict (commit 3d3befd)

`original` snapshot enables dirty-id derivation. useAutosave hook PATCHes
each dirty row sequentially with If-Match (debounce 2000ms). 409 → modal
("server has version vN; discard your edits and reload?") that invalidates
the react-query cache. Sidebar surfaces unsaved-draft warning. 6 autosave
tests. **56/56 vitest, exit criterion met for the typical review-correct-
save flow.**

### Slice B4 — TODO (the polish that flips remaining items to ✅)

1. POST /v1/annotations + DELETE /v1/annotations/:id — needed so locally
   drawn bboxes and locally deleted rows can autosave (currently shown as
   "drafts" in the sidebar warning banner)
2. Add GET /v1/images/:id with presigned download URL (B3 still uses
   list+filter and the dev MinIO public bucket)
3. Label picker dropdown — fetch labels from a new GET /v1/labels endpoint;
   L shortcut already focuses the placeholder button
4. Mask overlay PNG composite per label color (only meaningful once the
   AI worker writes mask images — currently writes mask_storage_key but no
   image is produced by the stub backends)
5. Point + polygon tool drawing handlers (bbox is the proven pattern)
6. Playwright E2E: load → edit bbox → autosave → trigger 409 → resolve

## Phase 5 — Active Learning

Exit criteria: *"queue ranks images by uncertainty score."*

- [ ] Uncertainty scoring (entropy of mask probabilities)
- [ ] `GET /v1/queue` ordered by uncertainty descending
- [ ] Quality score aggregation surfaced on dashboard

## Phase 6 — Observability

Exit criteria: *"distributed trace visible across web → api → ai."*

- [ ] OpenTelemetry SDK in all three services, OTLP exporter
- [ ] W3C `traceparent` propagated via fetch + RabbitMQ headers
- [ ] Prometheus `/metrics` on api + ai (job_queue_depth,
  inference_latency_seconds, api_request_duration_seconds, …)
- [ ] Grafana dashboards committed under `infra/grafana/`
- [ ] Alert rules: DLQ depth > 10 → P2, P99 inference > 30s → Slack, …

## Phase 7 — Continuous Learning

Exit criteria: *"retrain runs end-to-end, model promoted only on metric improvement."*

- [ ] Trigger: ≥500 accepted annotations AND ≥95% acceptance over last 1000
- [ ] Snapshot exporter — COCO / YOLO / Pascal VOC / TFRecord
- [ ] Albumentations augmentation pipeline (8–16× expansion)
- [ ] Fine-tune YOLOv11 + SAM 2.1, eval on held-out 10%
- [ ] Promotion gate: ΔmIoU ≥ 0.01 AND ΔmAP50 ≥ 0.005
- [ ] ONNX export + hot reload of active model
- [ ] Webhook on promotion + audit log entry

## Phase 8 — Hardening

Exit criteria: *"all security controls verified, load test passes."*

- [ ] Per-org rate limiting (token bucket: 100/min user, 1000/min API key)
- [ ] Audit log triggers on every privileged mutation
- [ ] Pact contracts for every service pair
- [ ] k6 load tests: P95 < 2 s for non-inference at 200 concurrent users
- [ ] Trivy / govulncheck / pip-audit blocking in CI
- [ ] Helm charts under `infra/helm/`
- [ ] Image signing with cosign before ghcr.io push

---

## Open issues / known gaps

- `TODO.md` itself was originally pasted spec content; rewritten this turn.
- `audit_log.request_id` is currently NULL because the AuditRecorder doesn't
  read the request id from context. Cleanup task during Phase 6 observability
  work — the access log already has it; audit should too.
- Better Auth's UserAgent / IP fields aren't being set on session rows. Add
  before Phase 8.
- `pnpm`/`npm` users will hit Bun-specific `bun audit` in CI; lockfile is
  Bun-only by design.
- Phase 1's `apps/api/internal/infrastructure/repo/images.go` still uses raw
  SQL strings, predating the sqlc-only rule. New code (jobs repo) uses
  sqlc. Refactor images.go onto sqlc as a Phase 8 hardening cleanup —
  bundle with the audit `request_id` work.
- Orphan-failed-job sweeper: rows left in `state='failed' AND
  error='publish_failed'` after a publish-after-commit failure are
  recoverable on retry (dedup window) but not auto-cleaned. Counter
  `job_publish_failures_total` already alerts. Sweeper is a Phase 6
  reliability task — see ADR-0004 appendix for the explicit deferral.

## Deferred to v2.0 (per spec section 2)

- CLIP embeddings (column reservation already in place)
- Grounding DINO natural-language segmentation
- YOLO-World open-vocabulary detection
- Video annotation + tracking
- 3D point clouds
