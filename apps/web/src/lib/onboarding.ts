// Provisions an organisation and an owner membership for a freshly created
// user. ADR-0003 requires this to run in the same transaction as the user
// row so a "user without org" state never exists.

import type { PoolClient } from "pg";
import { pgPool } from "./db";

const SLUG_RE = /^[a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])?$/;

export class OnboardingError extends Error {
  constructor(message: string, public code: "slug_taken" | "invalid_slug" | "internal") {
    super(message);
  }
}

function normalizeSlug(input: string): string {
  const v = input.trim().toLowerCase().replace(/\s+/g, "-").replace(/[^a-z0-9-]/g, "");
  if (!SLUG_RE.test(v)) {
    throw new OnboardingError(`invalid slug: ${input}`, "invalid_slug");
  }
  return v;
}

export async function ensureOrganization(args: {
  userId: string;
  email: string;
  preferredName?: string;
}): Promise<{ orgId: string; slug: string }> {
  const baseSlug = normalizeSlug(
    args.preferredName?.trim() || args.email.split("@")[0] || "workspace",
  );

  const client = await pgPool.connect();
  try {
    return await runOnboarding(client, args.userId, baseSlug, args.preferredName ?? baseSlug);
  } finally {
    client.release();
  }
}

async function runOnboarding(
  client: PoolClient,
  userId: string,
  baseSlug: string,
  displayName: string,
): Promise<{ orgId: string; slug: string }> {
  await client.query("BEGIN");
  try {
    // Skip if the user already has a membership (idempotent on retry).
    const existing = await client.query<{ org_id: string; slug: string }>(
      `SELECT m.org_id, o.slug
         FROM memberships m
         JOIN organizations o ON o.id = m.org_id
        WHERE m.user_id = $1
        ORDER BY m.created_at ASC
        LIMIT 1`,
      [userId],
    );
    if (existing.rows.length > 0) {
      await client.query("COMMIT");
      return { orgId: existing.rows[0].org_id, slug: existing.rows[0].slug };
    }

    let slug = baseSlug;
    for (let attempt = 0; attempt < 5; attempt++) {
      const taken = await client.query<{ exists: boolean }>(
        "SELECT EXISTS (SELECT 1 FROM organizations WHERE slug = $1) AS exists",
        [slug],
      );
      if (!taken.rows[0].exists) break;
      slug = `${baseSlug}-${Math.random().toString(36).slice(2, 6)}`;
    }

    const orgRow = await client.query<{ id: string }>(
      `INSERT INTO organizations (slug, name) VALUES ($1, $2) RETURNING id`,
      [slug, displayName.slice(0, 80)],
    );
    const orgId = orgRow.rows[0].id;

    await client.query(
      `INSERT INTO memberships (org_id, user_id, role) VALUES ($1, $2, 'owner')`,
      [orgId, userId],
    );

    await client.query(
      `INSERT INTO audit_log (org_id, actor_id, actor_kind, action, resource, resource_id, metadata)
       VALUES ($1, $2, 'user', 'organization.created', 'organization', $1, '{}'::jsonb)`,
      [orgId, userId],
    );

    await client.query("COMMIT");
    return { orgId, slug };
  } catch (err) {
    await client.query("ROLLBACK");
    if (err instanceof OnboardingError) throw err;
    throw new OnboardingError(`failed to provision organization: ${(err as Error).message}`, "internal");
  }
}
