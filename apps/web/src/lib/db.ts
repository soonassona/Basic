// Single Postgres pool reused by Better Auth and the post-signup org/membership
// transaction. Lives outside of the Better Auth adapter so the same connection
// pool can run the membership insert in the same transaction (ADR-0003).
import { Pool } from "pg";
import { env } from "./env";

const globalForPg = globalThis as unknown as { __vlPool?: Pool };

export const pgPool: Pool =
  globalForPg.__vlPool ??
  new Pool({
    connectionString: env.DATABASE_URL,
    max: 5,
    idleTimeoutMillis: 30_000,
  });

if (process.env.NODE_ENV !== "production") {
  globalForPg.__vlPool = pgPool;
}
