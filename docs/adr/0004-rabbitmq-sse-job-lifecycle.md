# ADR-0004 — RabbitMQ + SSE for the async job lifecycle

**Status:** Accepted (deferred to Phase 2 implementation)
**Date:** 2026-05-03

## Context

ML inference jobs (auto-segment, detect-then-segment) take seconds to
tens of seconds. The client must see live status without polling. Options
were Redis Streams + WebSocket, RabbitMQ + SSE, and Postgres LISTEN /
NOTIFY + WebSocket.

## Decision

- **Queue:** RabbitMQ 4.0. Topic exchange `jobs`, routing keys
  `jobs.pending.{type}`. Persistent delivery, manual ack. DLQ
  `jobs.dead` after three retries with exponential backoff (1s, 4s, 16s).
- **Client → API:** REST submission, idempotent via `dedup_key` =
  SHA-256 of `(org_id, image_id, type, payload)` with a 10-minute window.
- **API → Client:** Server-Sent Events at `GET /v1/jobs/:id/events`.
  SSE chosen over WebSocket because the channel is one-way (server →
  client), survives proxies trivially, and reconnects via `Last-Event-ID`.
- **Worker → API:** HTTP callback to `POST /internal/jobs/callback`
  authenticated by a shared bearer token plus HMAC of the job ID.

Phase 1 reserves the `jobs` table schema and the SSE handler stub but
does not yet publish messages.

## Consequences

**Positive.** SSE removes WebSocket auth complexity. RabbitMQ DLQ is
operationally well-understood. The dedup key prevents replay storms.

**Negative.** SSE means each subscriber holds an open HTTP/2 stream;
behind some load balancers this requires explicit timeouts. We pin the
SSE timeout to 10 minutes and let clients reconnect.

## Implementation note (2026-05-03)

Topology lives in `apps/api/internal/infrastructure/queue/`:

- `topology.go` — declares the two exchanges and three queues.
- `connection.go` — long-lived connection + publish channel with bounded
  dial timeout.
- `publisher.go` — `Publisher` port (`AMQPPublisher` impl + `NoopPublisher`
  for unit tests).
- `topology_test.go` — testcontainers-go integration test, build tag
  `integration`. Proves: idempotent declare, persistent round-trip, DLX on
  reject, retry-queue TTL routing back into `jobs.pending` (per-message
  TTL pattern, single-queue variant).

Retry policy: a single `jobs.retry` queue with `x-dead-letter-exchange =
jobs` and `x-dead-letter-routing-key = jobs.pending.retry`. The retry
publisher (separate TODO row) addresses the queue via the default
exchange and sets per-message `Expiration` (1000 / 4000 / 16000 ms per
attempt). On TTL expiry the broker dead-letters the message back into
`jobs.pending` via the topic binding.

Toolchain bump: pulling in `testcontainers-go` forces `go 1.25.0` in
`apps/api/go.mod` (transitive deps require it). CI updated accordingly.
Spec §3 nominally pins Go 1.24 — keep that as the *minimum* runtime
target while go.mod tracks the actual build floor.

## Implementation note (2026-05-03, second update)

`POST /v1/jobs` now publishes through the topology declared above.

**Dedup canonicalisation.** The dedup key is

    SHA-256( org_id || 0x1f || image_id || 0x1f || type || 0x1f || canonical(payload) )

with `canonical(payload)` being RFC 8785 JCS. Go uses
`github.com/gowebpki/jcs`; Python uses stdlib `json.dumps(sort_keys=True,
separators=(',', ':'))` with floats explicitly rejected (ECMA-262
ToString divergence is the only RFC 8785 surface stdlib doesn't cover —
our payload schema doesn't carry floats anyway). A pinned cross-service
hash test in `apps/ai-service/tests/test_dedup.py` fails loudly if
either side changes the algorithm.

**Publish-after-commit failure.** Inserting the row before publishing
opens a tiny window where the row exists but no message reaches the
broker. The use-case marks the row `state=failed` + `error='publish_failed'`,
increments `job_publish_failures_total{org_id, reason}` (counter is
already registered against `prometheus.DefaultRegisterer`; Phase 6 adds
the `/metrics` endpoint that scrapes it), and the handler returns 503 +
`Retry-After: 2` + a typed JSON error envelope (`code:
JOB_PUBLISH_FAILED`, `job_id`, `retry_after_ms`) so the client can retry
safely (replays land on the dedup window and return the same `job_id`).

**Deferred to Phase 6 (not a missed requirement).** A reconciler that
sweeps `state='failed' AND error='publish_failed' AND created_at <
now() - $threshold` rows and either republishes or finalises them is a
reliability concern, not a correctness one. Surface area is in the
counter + a TODO row under Phase 6 ("Open issues / known gaps").

**Idempotency response shape.** `Idempotent-Replayed: true` HTTP header
set on 202 replays alongside the `replayed: true` JSON field. Mirrors
Stripe / GitHub conventions; lets observability tooling tag dedup hits
without parsing the body.
