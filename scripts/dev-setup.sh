#!/usr/bin/env bash
# Bootstrap a fresh clone for local development.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ ! -f apps/web/.env.local ]]; then
  cp .env.example apps/web/.env.local
  echo "Created apps/web/.env.local"
fi
if [[ ! -f apps/api/.env ]]; then
  cp .env.example apps/api/.env
  echo "Created apps/api/.env"
fi
if [[ ! -f apps/ai-service/.env ]]; then
  cp .env.example apps/ai-service/.env
  echo "Created apps/ai-service/.env"
fi

echo "Installing JS deps via Bun..."
bun install

echo "Downloading Go modules..."
( cd apps/api && go mod download )

echo "Bringing up infra (postgres, redis, rabbitmq, minio)..."
docker compose up -d postgres redis rabbitmq minio minio-init

echo "Waiting for Postgres..."
until docker compose exec -T postgres pg_isready -U visionloop -d visionloop >/dev/null 2>&1; do
  sleep 1
done

echo "Running migrations..."
( cd apps/api && make migrate-up )

echo "Generating sqlc..."
( cd apps/api && make sqlc )

echo "Done. Start the apps with:"
echo "  ( cd apps/api && go run ./cmd/api )"
echo "  ( cd apps/web && bun run dev )"
