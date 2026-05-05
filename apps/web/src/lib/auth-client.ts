// Better Auth client used by the React app. Same secret/cookie space as
// the server-side instance.
"use client";

import { createAuthClient } from "better-auth/react";
import { env } from "./env";

export const authClient = createAuthClient({
  baseURL: env.APP_URL,
});

export const { useSession, signIn, signUp, signOut } = authClient;
