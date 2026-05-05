// Better Auth server instance. ADR-0003 designates this as the single
// source of truth for sessions; the Go API reads `better_auth_session`
// directly to authenticate requests.
//
// Model and field overrides match migrations 000002_identity and
// 000003_better_auth so Better Auth lands rows on the existing schema
// without a translation layer.

import { betterAuth } from "better-auth";
import { randomUUID } from "node:crypto";
import { pgPool } from "./db";
import { env } from "./env";

const oauthProviders: Record<string, { clientId: string; clientSecret: string }> = {};
if (env.GOOGLE_CLIENT_ID && env.GOOGLE_CLIENT_SECRET) {
  oauthProviders.google = {
    clientId: env.GOOGLE_CLIENT_ID,
    clientSecret: env.GOOGLE_CLIENT_SECRET,
  };
}
if (env.GITHUB_CLIENT_ID && env.GITHUB_CLIENT_SECRET) {
  oauthProviders.github = {
    clientId: env.GITHUB_CLIENT_ID,
    clientSecret: env.GITHUB_CLIENT_SECRET,
  };
}

export const auth = betterAuth({
  database: pgPool,
  secret: env.BETTER_AUTH_SECRET,
  baseURL: env.BETTER_AUTH_URL,
  trustedOrigins: [env.APP_URL],
  emailAndPassword: {
    enabled: true,
    minPasswordLength: 12,
    autoSignIn: true,
  },
  socialProviders: oauthProviders,

  // Generate string UUIDs so Postgres' UUID column on `users.id` accepts
  // the value Better Auth inserts. Better Auth reads `advanced.generateId`
  // (top-level), not `advanced.database.generateId` — the latter is silently
  // ignored. crypto.randomUUID is RFC 4122 v4.
  advanced: {
    cookiePrefix: "better-auth",
    generateId: () => randomUUID(),
  },

  // ── User table ─────────────────────────────────────────────────────
  user: {
    modelName: "users",
    fields: {
      name: "display_name",
      emailVerified: "email_verified",
      image: "avatar_url",
      createdAt: "created_at",
      updatedAt: "updated_at",
    },
    additionalFields: {
      locale: { type: "string", required: false, defaultValue: "en" },
    },
  },

  // ── Session table ──────────────────────────────────────────────────
  session: {
    modelName: "better_auth_session",
    fields: {
      expiresAt: "expires_at",
      ipAddress: "ip_address",
      userAgent: "user_agent",
      userId: "user_id",
      createdAt: "created_at",
      updatedAt: "updated_at",
    },
  },

  // ── Account table (OAuth + email/password credentials) ─────────────
  account: {
    modelName: "better_auth_account",
    fields: {
      accountId: "account_id",
      providerId: "provider_id",
      userId: "user_id",
      accessToken: "access_token",
      refreshToken: "refresh_token",
      idToken: "id_token",
      accessTokenExpiresAt: "access_token_expires_at",
      refreshTokenExpiresAt: "refresh_token_expires_at",
      createdAt: "created_at",
      updatedAt: "updated_at",
    },
  },

  // ── Verification table (password reset / email verify) ─────────────
  verification: {
    modelName: "better_auth_verification",
    fields: {
      expiresAt: "expires_at",
      createdAt: "created_at",
      updatedAt: "updated_at",
    },
  },
});

export type Auth = typeof auth;
