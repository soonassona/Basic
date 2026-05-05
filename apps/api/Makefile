SHELL := /usr/bin/env bash

POSTGRES_URL ?= postgres://visionloop:visionloop@localhost:5432/visionloop?sslmode=disable
MIGRATIONS_DIR := internal/db/migrations
PACT_LIB_DIR  ?= /tmp/pact-libs

.PHONY: build run test test-integration test-pact-provider pact-libs lint vet vuln sqlc migrate-up migrate-down migrate-create docker

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/api ./cmd/api

run:
	go run ./cmd/api

test:
	go test -race -count=1 -timeout=120s ./...

# Integration tests (testcontainers-go) — needs Docker and pulls
# rabbitmq:4.0-alpine and pgvector/pgvector:pg17 on first run. Gated by
# build tag to keep `make test` fast. Covers both per-component contracts
# under internal/infrastructure/{queue,repo,...} and the end-to-end
# submit→broker→callback round-trip in internal/integration.
test-integration:
	go test -tags=integration -race -count=1 -timeout=600s \
		./internal/infrastructure/... ./internal/integration/...

# Pact provider verification. Reads apps/web/pacts/<consumer>-<provider>.json
# and replays each interaction against the production-shaped router with
# Postgres + RabbitMQ booted via testcontainers (same as test-integration).
# Requires libpact_ffi.so on the loader path — `make pact-libs` downloads
# it; CI calls both targets in sequence.
test-pact-provider: pact-libs
	CGO_LDFLAGS="-L$(PACT_LIB_DIR)" \
	LD_LIBRARY_PATH="$(PACT_LIB_DIR):$$LD_LIBRARY_PATH" \
	go test -tags=pact -count=1 -timeout=600s ./internal/integration/...

pact-libs:
	@if [ ! -f "$(PACT_LIB_DIR)/libpact_ffi.so" ]; then \
		echo "downloading libpact_ffi to $(PACT_LIB_DIR)"; \
		go install github.com/pact-foundation/pact-go/v2@v2.4.3; \
		"$$(go env GOPATH)/bin/pact-go" install --libDir "$(PACT_LIB_DIR)"; \
	else \
		echo "libpact_ffi already present at $(PACT_LIB_DIR)"; \
	fi

lint:
	go vet ./...

vuln:
	govulncheck ./... || (echo "Install with: go install golang.org/x/vuln/cmd/govulncheck@latest" && exit 1)

sqlc:
	sqlc generate

migrate-up:
	migrate -path $(MIGRATIONS_DIR) -database "$(POSTGRES_URL)" up

migrate-down:
	migrate -path $(MIGRATIONS_DIR) -database "$(POSTGRES_URL)" down 1

migrate-create:
	@if [ -z "$(name)" ]; then echo "usage: make migrate-create name=add_widgets"; exit 1; fi
	migrate create -ext sql -dir $(MIGRATIONS_DIR) -seq $(name)

docker:
	docker build -t visionloop/api:dev .
