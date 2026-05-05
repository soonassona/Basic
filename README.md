# VisionLoop

Production-grade SaaS platform for computer vision dataset creation. Two AI
models — **SAM 2.1** (segmentation) and **YOLOv11x** (detection) — close the
loop between annotation and model performance through continuous learning.

> **Status:** Phase 1 — Foundation. **Verified end-to-end against the live
> stack:** signup → cookie HMAC verification → presigned upload → confirm →
> row reaches `ready`. Phase 2 (jobs + SSE + optimistic locking) is next.
> See [`TODO.md`](TODO.md) for phase-by-phase tracking.

---

## Repository layout

```
visionloop/
├── apps/
│   ├── web/         # Next.js 15 + React 19 + Better Auth (TypeScript strict)
│   ├── api/         # Go 1.24 + Gin + sqlc (Clean Architecture)
│   └── ai-service/  # Python 3.13 + FastAPI + Celery (Phase 1 stub)
├── packages/        # Shared TS contracts (Phase 2+)
├── docs/
│   ├── adr/         # Architecture Decision Records
│   └── ops/         # Branch protection, runbooks, deploy guides
├── infra/
│   ├── docker/      # prometheus.yml, compose helpers
│   └── helm/        # Helm charts (Phase 8)
├── scripts/         # Local dev helpers (dev-setup.sh)
├── .github/workflows/
├── docker-compose.yml
├── docker-compose.override.yml   # host port shifts for shared dev machines
├── TODO.md          # phase tracker — start here
└── README.md
```

## Prerequisites

| Tool     | Version | Notes                                                 |
| -------- | ------- | ----------------------------------------------------- |
| Bun      | ≥ 1.2   | Web app + workspace manager                           |
| Go       | ≥ 1.22  | 1.24 in CI/Docker; 1.22 fine for local builds         |
| Python   | ≥ 3.12  | 3.13 in CI                                            |
| Docker   | ≥ 24    | Local stack via `docker compose`                      |
| sqlc     | 1.27    | `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.27.0` |
| migrate  | 4.18    | `go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1` |

## First-time setup

```bash
git clone <repo> visionloop && cd visionloop

cp .env.example apps/web/.env.local
cp .env.example apps/api/.env
cp .env.example apps/ai-service/.env
# Generate one shared secret and write it to BOTH apps/web/.env.local
# and apps/api/.env. Mismatch = every API call rejected as unauthorized.
SECRET=$(openssl rand -hex 32)
sed -i "s|replace-with-a-32-byte-random-string|$SECRET|g" \
  apps/web/.env.local apps/api/.env

bun install                         # workspace deps
( cd apps/api && go mod download )
( cd apps/ai-service && python -m venv .venv && \
  . .venv/bin/activate && pip install -e ".[dev]" )

docker compose up -d postgres redis rabbitmq minio minio-init

# Migrations run automatically on API boot in dev. To run them by hand:
( cd apps/api && make migrate-up && make sqlc )
```

### Port conflicts on shared dev machines

`docker-compose.override.yml` shifts host ports off the defaults so the
stack coexists with a host-installed Postgres / Redis:

| Service    | Container port | Host port (override) |
| ---------- | -------------- | -------------------- |
| Postgres   | 5432           | **55432**            |
| Redis      | 6379           | **56379**            |
| RabbitMQ   | 5672 / 15672   | **55672 / 55673**    |
| MinIO      | 9000 / 9001    | **59000 / 59001**    |

If you do not have host services running, either delete the override file
or use `docker compose -f docker-compose.yml up …` to ignore it. Update
`POSTGRES_URL`, `REDIS_URL`, etc. in `apps/api/.env` to match.

## Running the stack

```bash
# All services in containers
docker compose up --build

# Or hot-reload locally:
( cd apps/api && go run ./cmd/api )       # :8080
( cd apps/web && bun run dev )            # :3000
( cd apps/ai-service && uvicorn app.main:app --reload --port 8000 )
```

URLs:

| Surface             | URL                                                |
| ------------------- | -------------------------------------------------- |
| Web app             | http://localhost:3000                              |
| API health          | http://localhost:8080/healthz                      |
| AI health           | http://localhost:8000/health                       |
| MinIO console       | http://localhost:59001 (minioadmin / minioadmin)   |
| RabbitMQ console    | http://localhost:55673 (visionloop / visionloop)   |

## Phase 1 acceptance flow

1. `docker compose up -d` brings the stack up.
2. Open http://localhost:3000 → **Register** → Better Auth creates the user
   row, then a post-hook writes `organizations` + `memberships(role='owner')`
   in a single transaction. The middleware on every authenticated route
   re-runs `ensureOrganization` idempotently so the user is **never** seen
   without a membership ([ADR-0003](docs/adr/0003-better-auth-as-session-source.md)).
3. Navigate to `/images` → drop an image → web requests a presigned URL via
   `POST /v1/images:presign` → uploads directly to MinIO/R2 → web confirms
   via `POST /v1/images/:id/confirm` → the row flips from `uploading` to
   `ready`.
4. Run the E2E test: `cd apps/web && bun run test:e2e`.

### Verified contract envelope (live integration, not just unit tests)

| Check                                              | Status |
| -------------------------------------------------- | ------ |
| Anonymous request → `401`                          | ✅ |
| Viewer presigning → `403`                          | ✅ |
| 50 MB+1 byte upload → `413`                        | ✅ |
| `image/gif` → `415`                                | ✅ |
| Expired session → `401`                            | ✅ |
| Cross-tenant image read → `404`                    | ✅ |
| Better Auth HMAC cookie verified by Go API         | ✅ |
| `X-Request-ID` round-trips                         | ✅ |
| HSTS / X-Frame-Options / nosniff / Referrer-Policy | ✅ |

## Testing

```bash
# Web
cd apps/web && bun run typecheck && bun run lint && bun run test
( cd apps/web && bun run test:e2e )       # Playwright; needs docker compose up

# API (race-clean)
cd apps/api && GOTOOLCHAIN=local go test -race -count=1 ./...

# AI service
cd apps/ai-service && .venv/bin/pytest -q
```

## CI

GitHub Actions (`.github/workflows/ci.yml`) runs three parallel workflows
(`web`, `api`, `ai-service`), each performing lint → typecheck → test →
security audit (govulncheck / pip-audit / bun audit) → docker build →
Trivy scan. Images push to `ghcr.io` **only** on `main` after every
required check passes.

`e2e.yml` runs the full `docker compose up` and the Playwright signup-and-
upload spec on every PR. See
[docs/ops/branch-protection.md](docs/ops/branch-protection.md) for the
required checks list.

## Architecture decisions

| ADR | Topic |
| --- | ----- |
| [0001](docs/adr/0001-clean-architecture-go-api.md) | Clean Architecture in the Go API; strict inward dependency direction |
| [0002](docs/adr/0002-sqlc-over-orm.md)             | sqlc over an ORM; zero raw-string interpolation |
| [0003](docs/adr/0003-better-auth-as-session-source.md) | Better Auth as the single source of session truth |
| [0004](docs/adr/0004-rabbitmq-sse-job-lifecycle.md)| RabbitMQ + SSE for async job lifecycle (Phase 2 implementation) |
| [0005](docs/adr/0005-pgvector-reserved.md)         | pgvector enabled in v1; embedding column added in v2 |

## What's NOT in this commit (deliberately)

| Capability                              | Lands in |
| --------------------------------------- | -------- |
| Job submission, RabbitMQ, SSE           | Phase 2  |
| SAM 2.1 / YOLOv11 inference             | Phase 3  |
| Konva annotation studio                 | Phase 4  |
| Active learning queue                   | Phase 5  |
| OpenTelemetry exporters                 | Phase 6  |
| Continuous training pipeline            | Phase 7  |
| Helm charts, Pact, k6, cosign signing   | Phase 8  |

## License

UNLICENSED — internal project.
