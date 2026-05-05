"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { signUp } from "@/lib/auth-client";

const schema = z.object({
  name: z.string().min(2).max(80),
  email: z.string().email(),
  password: z
    .string()
    .min(12, "At least 12 characters")
    .regex(/[A-Z]/, "Add an uppercase letter")
    .regex(/[a-z]/, "Add a lowercase letter")
    .regex(/[0-9]/, "Add a digit"),
});

type RegisterValues = z.infer<typeof schema>;

export default function RegisterPage() {
  const t = useTranslations("auth");
  const router = useRouter();
  const [serverError, setServerError] = useState<string | null>(null);

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<RegisterValues>({ resolver: zodResolver(schema), mode: "onBlur" });

  async function onSubmit(values: RegisterValues) {
    setServerError(null);
    const res = await signUp.email({
      email: values.email,
      password: values.password,
      name: values.name,
    });
    if (res.error) {
      setServerError(res.error.message ?? "Registration failed");
      return;
    }
    router.push("/dashboard");
    router.refresh();
  }

  return (
    <main className="mx-auto flex min-h-[100dvh] max-w-md flex-col justify-center px-6 py-24">
      <h1 className="text-3xl font-semibold">{t("register_title")}</h1>
      <p className="mt-2 text-[var(--color-muted)]">{t("register_subtitle")}</p>

      <form onSubmit={handleSubmit(onSubmit)} className="mt-8 space-y-4" noValidate>
        <Field label={t("name")} error={errors.name?.message}>
          <input type="text" autoComplete="name" {...register("name")} className="input" />
        </Field>
        <Field label={t("email")} error={errors.email?.message}>
          <input type="email" autoComplete="email" {...register("email")} className="input" />
        </Field>
        <Field label={t("password")} error={errors.password?.message} hint={t("password_hint")}>
          <input
            type="password"
            autoComplete="new-password"
            {...register("password")}
            className="input"
          />
        </Field>

        {serverError && (
          <p role="alert" className="text-sm text-[var(--color-danger)]">
            {serverError}
          </p>
        )}

        <button type="submit" className="btn btn-primary w-full" disabled={isSubmitting}>
          {isSubmitting ? "…" : t("submit_register")}
        </button>
      </form>

      <a href="/login" className="mt-6 text-sm text-[var(--color-muted)] hover:text-[var(--color-text)]">
        {t("switch_to_login")}
      </a>
    </main>
  );
}

function Field({
  label,
  error,
  hint,
  children,
}: {
  label: string;
  error?: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-sm text-[var(--color-muted)]">{label}</span>
      {children}
      {hint && !error && <span className="mt-1 block text-xs text-[var(--color-muted)]">{hint}</span>}
      {error && <span role="alert" className="mt-1 block text-xs text-[var(--color-danger)]">{error}</span>}
    </label>
  );
}
