ROLE
You are a senior full-stack and ML platform engineer building VisionLoop, 
a production-grade SaaS platform for computer vision dataset creation. 
This specification follows industry-standard practices for security 
(OWASP ASVS L2), accessibility (WCAG 2.1 AA), data protection (GDPR), 
and observability (OpenTelemetry).

═══════════════════════════════════════════════════════════════════════
1. PRODUCT VISION
═══════════════════════════════════════════════════════════════════════

Mission
  Create high-quality datasets for computer vision models with 
  user-friendly annotation tools. Streamline AI development workflows.

Value Proposition
  An annotation platform that learns from every correction. Upload, 
  segment with AI, review, and the model retrains automatically — 
  closing the loop between data and model performance.

Target Users
  ML engineers, data scientists, annotation teams, and CV researchers 
  who need labeled image data for training segmentation and detection 
  models.

Competitive Positioning
  Differentiated from CVAT, Roboflow, and Label Studio through 
  automated continuous learning, model uncertainty-driven active 
  learning, and zero-config AI inference.

═══════════════════════════════════════════════════════════════════════
2. SCOPE
═══════════════════════════════════════════════════════════════════════

In Scope (v1.0)
  • Two AI models — SAM 2.1 (segmentation) and YOLOv11x (detection)
  • Annotation workflows — bounding box, point prompt, auto-segment, 
    polygon, manual
  • Active learning queue based on model uncertainty
  • Continuous training pipeline with quality gates
  • Dataset export — COCO JSON, YOLO, Pascal VOC, TFRecord
  • Multi-tenant organization model with RBAC
  • Internationalization — English (default), Thai

Out of Scope (deferred to v2.0)
  • Natural language segmentation (Grounding DINO)
  • Open-vocabulary detection (YOLO-World)
  • Semantic image search via CLIP embeddings
  • Video annotation and tracking
  • 3D point cloud annotation

═══════════════════════════════════════════════════════════════════════
3. TECHNOLOGY STACK
═══════════════════════════════════════════════════════════════════════

Frontend (apps/web)
  Next.js 15.3 (App Router with React Server Components)
  React 19.1, TypeScript 5.8 (strict mode)
  TailwindCSS 4.1, shadcn/ui component library
  Konva.js 9.3 for canvas rendering
  Better Auth 1.x for authentication
  TanStack Query v5.70 for server state
  Zustand 5.0 for client state
  React Hook Form 7 with Zod 3 schema validation
  next-intl for internationalization
  Bun 1.2 as package manager
  Vitest 2 for unit tests, Playwright 1.51 for E2E, MSW 2 for mocks

API Gateway (apps/api)
  Go 1.24 with Gin 1.10 web framework
  pgx 5 PostgreSQL driver
  sqlc 1.28 for type-safe SQL (no raw query interpolation permitted)
  golang-migrate 4.18 for schema migrations
  go-redis/v9 client
  amqp091-go for RabbitMQ
  OpenTelemetry Go SDK 1.34
  testcontainers-go for integration testing

ML Service (apps/ai-service)
  Python 3.13 with FastAPI 0.115 and Uvicorn 0.32
  Celery 5.5 for task queue
  PyTorch 2.6 for model inference
  ONNX Runtime 1.20 for CPU inference
  Albumentations 2.x for data augmentation
  pytest 8 with pytest-asyncio 0.24

Infrastructure
  PostgreSQL 17 with pgvector 0.8 extension
  Redis 8.0
  RabbitMQ 4.0
  Cloudflare R2 (S3-compatible object storage)
  OpenTelemetry Collector 0.120
  Prometheus and Grafana for metrics
  Jaeger (development) or Honeycomb (production) for traces
  Docker, Helm charts, Terraform for infrastructure as code

═══════════════════════════════════════════════════════════════════════
4. SYSTEM ARCHITECTURE
═══════════════════════════════════════════════════════════════════════

Architectural Style
  Clean Architecture in Go API (domain, application, infrastructure, 
  interfaces). Hexagonal/ports-and-adapters pattern.

Service Topology
  Web client (Next.js) → API Gateway (Go) → 
    ├── PostgreSQL (transactional state)
    ├── Redis (sessions, cache, rate limits)
    ├── RabbitMQ (async job queue)
    └── ML Service (Python) → Cloudflare R2 (object storage)

Communication Patterns
  Synchronous — REST/HTTPS between client and API
  Asynchronous — RabbitMQ between API and ML workers
  Real-time — Server-Sent Events for job status updates
  Internal — HTTP callbacks from ML workers to API

Multi-Tenancy
  Row-level isolation via org_id foreign key on all tenant-scoped 
  tables. Storage keys namespaced by org_id. CORS allowlist 
  configured per organization.

═══════════════════════════════════════════════════════════════════════
5. AI MODELS
═══════════════════════════════════════════════════════════════════════

SAM 2.1 (sam2.1_hiera_large)
  Role: Primary segmentation engine
  Inputs: bounding box, point prompts, automatic mask generation
  Loading: Singleton with thread-safe inference lock
  Deployment: ONNX exported for CPU inference fallback

YOLOv11x (Ultralytics)
  Role: Object detection and pre-labeling
  Outputs: Bounding boxes feeding into SAM 2.1 for auto-segmentation
  Training: Fine-tuned per organization during continuous learning
  Deployment: ONNX exported

Model Routing Logic
  job_type = "auto"     → YOLOv11 → SAM 2.1
  job_type = "box"      → SAM 2.1
  job_type = "points"   → SAM 2.1
  job_type = "polygon"  → manual (no AI)
  default               → SAM 2.1

═══════════════════════════════════════════════════════════════════════
6. DESIGN SYSTEM
═══════════════════════════════════════════════════════════════════════

Theme
  Dark, data-dense, professional. Inspired by Linear, Roboflow, 
  and modern developer tools.

Color Tokens
  Background:  #0c0d0f      Surface:     #13151a
  Border:      #252830      Border-2:    #2e3240
  Primary:     #4a8ff5      Success:     #3ecf8e
  Warning:     #f59e0b      Danger:      #ef4444
  Text:        #e4e6ed      Muted:       #6b7485

Typography
  Inter for UI text
  JetBrains Mono for code, scores, and identifiers
  Font sizes follow modular scale: 11, 12, 13, 14, 16, 18, 22, 28, 42

Layout Principles
  Density over whitespace for power-user workflows
  Keyboard-first interaction model
  Status communication through color and shape
  Flat surfaces, no decorative gradients
  Maximum 70% viewport for canvas in studio

Accessibility (WCAG 2.1 AA)
  Color contrast ratio ≥ 4.5:1 for all text
  Focus indicators visible on all interactive elements
  Keyboard navigation for all workflows
  Screen reader labels via aria-label
  prefers-reduced-motion respected

═══════════════════════════════════════════════════════════════════════
7. PAGE INVENTORY
═══════════════════════════════════════════════════════════════════════

Public
  /                Marketing landing page
  /pricing         Subscription tiers (Free, Pro, Enterprise)
  /docs            Documentation portal
  /login           Authentication with email and OAuth
  /register        Account creation with organization setup

Application (authenticated, with sidebar navigation)
  /dashboard       Workspace overview with KPIs and recent activity
  /images          Image library with filters and upload
  /images/:id      Image detail with annotation history
  /studio/:id      Full-screen annotation interface
  /queue           Active learning queue
  /jobs            Async job tracker with live status
  /dataset         Dataset versioning and export
  /models          Model registry with performance metrics
  /analytics       Aggregate analytics and team performance

Settings (sub-navigation tabs)
  /settings/general    Organization profile
  /settings/members    Team and role management
  /settings/api-keys   API credential management
  /settings/webhooks   Event subscription endpoints
  /settings/billing    Subscription and invoicing

═══════════════════════════════════════════════════════════════════════
8. DATABASE SCHEMA
═══════════════════════════════════════════════════════════════════════

Conventions
  All identifiers: UUID v4 generated by gen_random_uuid()
  Timestamps: TIMESTAMPTZ DEFAULT now()
  Soft deletes: deleted_at TIMESTAMPTZ NULL
  Foreign keys: ON DELETE CASCADE for owned data, RESTRICT for shared
  Indexes: btree by default, GIN for full-text, IVFFlat for vectors

Core Tables
  organizations, users, memberships, api_keys, images, labels, 
  annotation_sets (with version column for optimistic concurrency 
  control), annotations (with ai_score, quality_score, model_used, 
  human_accepted), jobs (with dedup_key UNIQUE constraint), 
  audit_log (append-only, immutable), dataset_snapshots (semantic 
  versioning), model_versions (with onnx_key, mean_iou, map50, 
  is_active flag)

Future-Proofing
  pgvector extension installed in v1.0 for v2.0 readiness
  Schema reserves space for clip_embedding VECTOR(768) column

═══════════════════════════════════════════════════════════════════════
9. SECURITY
═══════════════════════════════════════════════════════════════════════

Authentication
  Better Auth 1.x — fully integrated from initial commit
  Session-based with JWT tokens (15-minute access, 7-day refresh)
  OAuth providers: Google, GitHub, Facebook
  Password requirements: 12+ characters, complexity validated client 
  and server side, breach checked against HaveIBeenPwned

Authorization
  Role-Based Access Control with four roles:
    Owner — full administrative access
    Admin — manage members, API keys, webhooks
    Annotator — create and edit annotations
    Viewer — read-only access
  Resource-level scoping by organization

Middleware Stack (executed in order)
  1. Request ID injection (X-Request-ID header)
  2. CORS validation against per-org allowlist
  3. Rate limiting (token bucket: 100/min user, 1000/min API key)
  4. JWT or session validation
  5. RBAC policy enforcement
  6. Input sanitization
  7. File validation (magic bytes, MIME allowlist, 50MB cap)

Database Security
  All queries via sqlc — zero raw string interpolation permitted
  Parameterized queries enforced at compile time

Transport Security
  TLS 1.3 only, HSTS with includeSubDomains and preload
  Strict Content Security Policy
  X-Frame-Options DENY, X-Content-Type-Options nosniff

Data Protection
  AES-256 encryption at rest (cloud KMS managed keys)
  Audit log retained 2 years, append-only, immutable
  GDPR data export and right-to-erasure endpoints
  PII isolated in users table, referenced by UUID elsewhere

Vulnerability Management
  govulncheck for Go, pip-audit for Python, bun audit for Node
  Trivy container scanning on every build
  Dependabot security updates enabled
  All dependencies pinned to specific versions

═══════════════════════════════════════════════════════════════════════
10. ANNOTATION STUDIO REQUIREMENTS
═══════════════════════════════════════════════════════════════════════

Canvas
  Konva.js 9.3 occupying 70% or more of viewport
  Mask overlay rendered as PNG composite per label color
  Hardware-accelerated rendering with offscreen canvas

Tools
  Select, bounding box, point prompt, polygon, auto-segment

Concurrency Control
  Optimistic locking via version column on annotation_sets
  PATCH endpoints require If-Match header with current version
  Server returns HTTP 409 with current_version on conflict
  Client presents merge UI on conflict (not just notification)

Persistence
  Autosave debounced at 2000ms
  Saves to API endpoint via POST or PATCH
  ETag header used for conflict detection
  No localStorage for primary state (UI preferences only)

History
  Command pattern undo/redo stack, maximum depth 50

Keyboard Shortcuts (guarded against input focus)
  A — accept selected annotation
  R — reject selected annotation
  L — focus label picker
  D — delete selected annotation
  Z — undo
  Shift+Z — redo
  Esc — deselect
  ←/→ — previous or next image

Internationalization
  All UI strings in i18n message catalogs
  Date and number formatting via Intl API
  RTL layout support reserved for future locales

═══════════════════════════════════════════════════════════════════════
11. ASYNCHRONOUS JOB PROCESSING
═══════════════════════════════════════════════════════════════════════

Submission Flow
  Client POST /jobs → API responds 202 Accepted with job_id
  Client subscribes to GET /jobs/:id/events (Server-Sent Events)
  API publishes to RabbitMQ exchange jobs.pending
  ML worker consumes, runs inference, calls back to API
  API updates state and pushes SSE event to subscribed clients

Idempotency
  Deduplication key: SHA-256 of (org_id, image_id, type, payload)
  Window: 10 minutes
  Duplicate submissions return existing job_id

Reliability
  Retry policy: 3 attempts with exponential backoff (1s, 4s, 16s)
  Dead-letter queue after final retry exhaustion
  Persistent message delivery mode

═══════════════════════════════════════════════════════════════════════
12. CONTINUOUS LEARNING PIPELINE
═══════════════════════════════════════════════════════════════════════

Trigger Conditions
  Accepted annotations exceed 500 (configurable per organization)
  AND acceptance rate exceeds 95% over last 1000 reviews

Pipeline Stages
  1. Snapshot — versioned dataset export (semantic versioning)
  2. Augmentation — Albumentations pipeline (8-16x expansion)
  3. Fine-tuning — YOLOv11 and SAM 2.1 on augmented dataset
  4. Evaluation — held-out 10% split, compute mIoU and mAP@0.5
  5. Promotion gate — mIoU improves by ≥0.01 AND mAP50 by ≥0.005
  6. ONNX export — both models exported for CPU deployment
  7. Hot reload — ML service updates active model
  8. Notification — webhook fired, audit logged

Versioning Semantics
  PATCH — annotations updated on existing images
  MINOR — new images added to dataset
  MAJOR — label taxonomy changed (breaking)

═══════════════════════════════════════════════════════════════════════
13. OBSERVABILITY
═══════════════════════════════════════════════════════════════════════

Distributed Tracing
  OpenTelemetry SDK in all three services
  W3C Trace Context propagation via traceparent header
  Spans for HTTP requests, database queries, queue operations, 
  and model inference
  Export to Jaeger (development) or Honeycomb (production)

Metrics (Prometheus)
  job_queue_depth (gauge, labeled by priority)
  inference_latency_seconds (histogram, labeled by model)
  model_router_decisions_total (counter, labeled by model)
  annotation_acceptance_rate (gauge, per org per day)
  annotation_quality_score_avg (gauge, per org)
  api_request_duration_seconds (histogram, labeled by route)
  storage_bytes_used (gauge, per org)

Structured Logging (JSON)
  Required fields: timestamp, level, service, trace_id, request_id, 
  user_id, org_id, method, path, status, duration_ms, message

Alerting
  Dead-letter queue depth > 10 — PagerDuty P2
  Inference P99 > 30 seconds — Slack notification
  API error rate > 5% over 5 minutes — PagerDuty P1
  Acceptance rate < 0.80 for any organization — Slack warning
  Model promotion event — Slack #ml-ops channel

═══════════════════════════════════════════════════════════════════════
14. CONTINUOUS INTEGRATION AND DEPLOYMENT
═══════════════════════════════════════════════════════════════════════

CI Pipeline (GitHub Actions)
  Triggers: push to any branch, pull request to main
  Three parallel jobs (one per service):
    Lint and format check
    Type checking (tsc --noEmit, mypy)
    Unit and integration tests with coverage
    Security audit (govulncheck, pip-audit, bun audit)
    Docker image build
    Container vulnerability scan (Trivy)
  Image push to ghcr.io only on main branch after all jobs pass

Quality Gates
  Test coverage minimum 80% on domain layer
  Zero high or critical vulnerabilities permitted
  Pact contract tests pass for all service pairs
  k6 load test maintains P95 latency under 2 seconds for 
  non-inference endpoints at 200 concurrent users

Branch Protection
  Required status checks: API, AI Service, Web
  Require branches to be up to date before merging
  Require pull request review approval

═══════════════════════════════════════════════════════════════════════
15. TESTING STRATEGY
═══════════════════════════════════════════════════════════════════════

Test Pyramid Distribution
  Unit tests (70%) — fast, isolated, mock external dependencies
  Integration tests (20%) — real database via testcontainers
  End-to-end tests (10%) — Playwright for critical user journeys

Go API
  Table-driven unit tests on domain and application layers
  Integration tests via testcontainers-go (real Postgres + Redis)
  HTTP handler tests via httptest
  Coverage target: 80% on domain and application layers

Python ML Service
  pytest 8 with pytest-asyncio
  FastAPI TestClient for endpoint testing
  unittest.mock for inference mocking
  Integration test with small ONNX checkpoint on CPU

Web Frontend
  Vitest 2 for components and hooks
  Mock Service Worker 2 for API mocking
  Playwright 1.51 for E2E flows: upload → annotate → export

Contract Testing
  Pact.io consumer-driven contracts between all service pairs
  CI fails on breaking schema changes

Load Testing
  k6 v0.56 scripts for upload, job submission, annotation save
  Service Level Objective: P95 < 2s for non-inference endpoints

═══════════════════════════════════════════════════════════════════════
16. DEPLOYMENT
═══════════════════════════════════════════════════════════════════════

Production Topology
  Web — Vercel (Next.js edge runtime, global CDN)
  API — Fly.io (auto-scale, three regions)
  ML Service — RunPod (GPU primary), Railway (CPU fallback)
  Database — Neon serverless Postgres with read replica pool
  Cache — Upstash Redis (serverless)
  Queue — CloudAMQP managed RabbitMQ (high availability)
  Storage — Cloudflare R2 (zero egress cost)
  Observability — Honeycomb (traces), Grafana Cloud (metrics)

Environment Strategy
  Development — Docker Compose locally
  Staging — full deployment, k6 load tests gate promotion
  Production — blue/green deployment with automated rollback

═══════════════════════════════════════════════════════════════════════
17. IMPLEMENTATION PHASES
═══════════════════════════════════════════════════════════════════════

Phase 1: Foundation
  Authentication fully integrated, database schema with migrations, 
  sqlc setup, R2 upload via presigned URLs, GitHub Actions CI.
  Exit criteria: end-to-end signup and upload flow, all CI green.

Phase 2: Core API
  Job submission, RabbitMQ topology, SSE endpoint, deduplication, 
  retry and dead-letter queue, PATCH /annotations/:id with 
  optimistic locking returning 409 on conflict.
  Exit criteria: job submitted, processed, status streamed via SSE.

Phase 3: AI Integration
  SAM 2.1 and YOLOv11 loaded as thread-safe singletons, model 
  router, ONNX export pipeline, HTTP callback to API.
  Exit criteria: all job types return masks for test images.

Phase 4: Annotation Studio
  Konva canvas, all annotation tools, keyboard shortcuts, autosave 
  to API, optimistic locking conflict UI, label picker.
  Exit criteria: annotator completes review-correct-save workflow.

Phase 5: Active Learning
  Uncertainty scoring, annotation queue API, quality score 
  computation surfaced in UI.
  Exit criteria: queue ranks images by uncertainty score.

Phase 6: Observability
  OpenTelemetry tracing, Prometheus metrics, Grafana dashboards, 
  structured JSON logs, alert rules.
  Exit criteria: distributed trace visible across web → api → ai.

Phase 7: Continuous Learning
  Snapshot export with all four formats, Albumentations pipeline, 
  retrain and eval scripts with quality gates, ONNX export, 
  hot-reload, webhook notifications.
  Exit criteria: retrain runs end-to-end, model promoted only on 
  metric improvement.

Phase 8: Hardening
  Complete RBAC, audit logging, rate limiting, Trivy in CI, Pact 
  contracts, k6 load tests passing SLOs.
  Exit criteria: all security controls verified, load test passes.

Each phase exit gate requires:
  Tests written and passing
  CI pipeline green
  Specification compliance verified through review
  End-to-end flow demonstrable

═══════════════════════════════════════════════════════════════════════
18. LESSONS LEARNED FROM PREVIOUS ITERATION
═══════════════════════════════════════════════════════════════════════

The following anti-patterns must be avoided:

  Authentication library declared in package.json but not integrated
  → Wire authentication completely from the first commit

  Embedding model dimension mismatch with database column
  → Validate dimensions at service startup, fail fast on mismatch

  Background job declared but never triggered
  → Integration test must verify the trigger fires

  Optimistic locking schema present but no handler enforcement
  → Build schema and handler in the same pull request

  Critical endpoint missing while UI calls it
  → Frontend cannot ship without backend endpoint review

  Keyboard shortcuts partially implemented
  → All shortcuts in specification or none

  ONNX export pipeline absent
  → CPU inference path requires this from Phase 3

  Zero test coverage across all services
  → Tests are mandatory exit criteria for every phase

  No CI pipeline
  → CI must be operational before any feature work begins

  State persisted only to localStorage
  → Authoritative state lives in the API

═══════════════════════════════════════════════════════════════════════
19. DELIVERABLES
═══════════════════════════════════════════════════════════════════════

Generate the complete project including:

  Folder structure for monorepo with three applications
  Working code for all three services
  Dockerfiles for each service
  docker-compose.yml for local development
  Kubernetes Helm charts for production
  Database migrations and sqlc query files
  OpenAPI 3.1 specification
  GitHub Actions workflow definitions
  Environment variable templates (no secrets committed)
  README with setup, development, and deployment instructions
  Branch protection configuration guide
  Architecture Decision Records (ADRs) for key choices

═══════════════════════════════════════════════════════════════════════
20. ENGINEERING PRINCIPLES
═══════════════════════════════════════════════════════════════════════

Apply these principles throughout implementation:

  Clean Architecture in Go (domain, application, infrastructure, 
  interfaces) with strict dependency direction inward

  Type-safe SQL via sqlc — raw query strings prohibited

  Authentication integration verified by end-to-end test on day one

  All async flows publish status via Server-Sent Events

  Every page implements the dark design system tokens consistently

  All user-facing text wrapped in i18n message function

  All endpoints accepting input validate via schema (Zod or struct tags)

  All endpoints returning resource state include ETag header

  All mutating endpoints with concurrency risk enforce If-Match

  All metrics labeled with org_id for per-tenant observability

  All log lines include trace_id for cross-service correlation

  All container images scanned and signed before production push

Build the data flywheel correctly. Two models. Production-grade.
