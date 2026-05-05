import { defineConfig } from "vitest/config";
import { fileURLToPath } from "node:url";

// Pact consumer tests live outside the default vitest run because they
// boot a Pact mock server (Rust FFI) per interaction — heavier than the
// regular unit suite. Run via `bun run test:pact:consumer`.
//
// Output JSON pacts land under `apps/web/pacts/`. The Go provider then
// verifies them via `make test-pact-provider` (build tag `pact`).
export default defineConfig({
  test: {
    environment: "node",
    globals: true,
    include: ["tests/pact/**/*.test.ts"],
    exclude: ["node_modules/**", "tests/e2e/**"],
    testTimeout: 30_000,
  },
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
});
