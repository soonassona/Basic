# ADR-0001 — Clean Architecture in the Go API

**Status:** Accepted (Phase 1)
**Date:** 2026-05-03

## Context

The API gateway must integrate Postgres, Redis, RabbitMQ, R2, and a Python
ML service, while the domain model (organisations, images, annotations,
jobs, dataset versions) is the long-lived asset. Section 18 of the spec
calls out anti-patterns like "schema present but no handler enforcement"
and "endpoints declared but never integrated"; both are easier to ship
when handlers reach directly into infra. We need a layout that prevents
that drift without imposing ceremony on small handlers.

## Decision

Adopt Clean Architecture with strict inward dependency direction:

```
apps/api/internal/
├── domain/         # entities, value objects, domain errors. Zero imports.
├── application/    # use-cases (commands/queries). Depends on domain only,
│                   # talks to infra through interfaces declared here.
├── infrastructure/ # postgres (sqlc), redis, rabbitmq, s3, betterauth.
│                   # Implements application interfaces.
└── interfaces/
    └── http/       # gin handlers, request/response DTOs, middleware.
                    # Wires infra into application use-cases.
```

Compile-time rules:

- `domain` imports nothing from this repo.
- `application` imports `domain` only.
- `infrastructure` and `interfaces` may import `domain` and `application`.
- `cmd/api` is the only place where everything is wired together.

## Consequences

**Positive.** Use-cases are unit-testable without a database. Swapping R2
for another object store is an infrastructure-only change. The "schema
without handler" anti-pattern is harder because adding a domain operation
requires a use-case, which forces a handler to call it.

**Negative.** Three files instead of one for each new endpoint. Mitigated
by keeping use-cases small (one struct, one `Execute`) and avoiding
premature interface explosion — interfaces live where they are *consumed*
(application layer), not where they are *implemented*.
