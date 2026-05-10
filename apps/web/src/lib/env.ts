// Centralised env access. Throws at module load if a required value is
// missing — section 18: "validate at service startup, fail fast on
// mismatch."

const required = (key: string): string => {
  const value = process.env[key];
  if (!value || value.trim() === "") {
    throw new Error(`Missing required environment variable: ${key}`);
  }
  return value;
};

const optional = (key: string, fallback = ""): string => process.env[key] ?? fallback;

export const env = {
  NODE_ENV: optional("NODE_ENV", "development"),
  APP_URL: optional("NEXT_PUBLIC_APP_URL", "http://localhost:3000"),
  API_URL: optional("NEXT_PUBLIC_API_URL", "http://localhost:8080"),
  BETTER_AUTH_SECRET: optional("BETTER_AUTH_SECRET", ""),
  BETTER_AUTH_URL: optional("BETTER_AUTH_URL", "http://localhost:3000"),
  DATABASE_URL: optional("DATABASE_URL", ""),
  GOOGLE_CLIENT_ID: optional("GOOGLE_CLIENT_ID", ""),
  GOOGLE_CLIENT_SECRET: optional("GOOGLE_CLIENT_SECRET", ""),
  GITHUB_CLIENT_ID: optional("GITHUB_CLIENT_ID", ""),
  GITHUB_CLIENT_SECRET: optional("GITHUB_CLIENT_SECRET", ""),
} as const;

// Server-only check. The auth module imports this and validates that
// required keys are set at boot. Keeping the throw out of module load on
// the client side avoids "missing env" errors during static analysis.
export function assertServerEnv(): void {
  required("BETTER_AUTH_SECRET");
  required("DATABASE_URL");
}
