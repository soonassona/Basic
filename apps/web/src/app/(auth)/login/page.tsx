"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslations } from "next-intl";
import { useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { signIn } from "@/lib/auth-client";

const schema = z.object({
  email: z.string().email(),
  password: z.string().min(12),
});

type LoginValues = z.infer<typeof schema>;

export default function LoginPage() {
  const t = useTranslations("auth");
  const router = useRouter();
  const params = useSearchParams();
  const next = params.get("next") || "/dashboard";
  const [serverError, setServerError] = useState<string | null>(null);

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<LoginValues>({ resolver: zodResolver(schema), mode: "onBlur" });

  async function onSubmit(values: LoginValues) {
    setServerError(null);
    const res = await signIn.email({ email: values.email, password: values.password });
    if (res.error) {
      setServerError(res.error.message ?? "Login failed");
      return;
    }
    router.push(next);
    router.refresh();
  }

  return (
    <main className="mx-auto flex min-h-[100dvh] max-w-md flex-col justify-center px-6 py-24">
      <h1 className="text-3xl font-semibold">{t("login_title")}</h1>
      <p className="mt-2 text-[var(--color-muted)]">{t("login_subtitle")}</p>

      <form onSubmit={handleSubmit(onSubmit)} className="mt-8 space-y-4" noValidate>
        <Field label={t("email")} error={errors.email?.message}>
          <input type="email" autoComplete="email" {...register("email")} className="input" />
        </Field>
        <Field label={t("password")} error={errors.password?.message}>
          <input type="password" autoComplete="current-password" {...register("password")} className="input" />
        </Field>

        {serverError && (
          <p role="alert" className="text-sm text-[var(--color-danger)]">
            {serverError}
          </p>
        )}

        <button type="submit" className="btn btn-primary w-full" disabled={isSubmitting}>
          {isSubmitting ? "…" : t("submit_login")}
        </button>
      </form>

      <a href="/register" className="mt-6 text-sm text-[var(--color-muted)] hover:text-[var(--color-text)]">
        {t("switch_to_register")}
      </a>
    </main>
  );
}

function Field({ label, error, children }: { label: string; error?: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1 block text-sm text-[var(--color-muted)]">{label}</span>
      {children}
      {error && <span role="alert" className="mt-1 block text-xs text-[var(--color-danger)]">{error}</span>}
    </label>
  );
}
