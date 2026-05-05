"use client";

import { useQuery } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { api } from "@/lib/api";

export default function DashboardPage() {
  const t = useTranslations("dashboard");
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me() });
  const images = useQuery({ queryKey: ["images"], queryFn: () => api.listImages(1) });

  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-3xl font-semibold">{t("title")}</h1>
        <p className="mt-1 text-[var(--color-muted)]">
          {me.data ? t("greeting", { name: me.data.user.display_name || me.data.user.email }) : "…"}
        </p>
      </header>

      <section className="grid gap-4 md:grid-cols-4">
        <Kpi label={t("kpi_images")} value={images.data?.total ?? "—"} />
        <Kpi label={t("kpi_annotations")} value="—" muted />
        <Kpi label={t("kpi_acceptance")} value="—" muted />
        <Kpi label={t("kpi_jobs")} value="—" muted />
      </section>

      <section className="surface rounded-md p-8 text-center">
        <p className="text-[var(--color-muted)]">{t("empty_state")}</p>
        <a href="/images" className="btn btn-primary mt-4 inline-flex">
          {t("kpi_images")} →
        </a>
      </section>
    </div>
  );
}

function Kpi({ label, value, muted = false }: { label: string; value: string | number; muted?: boolean }) {
  return (
    <div className="surface rounded-md p-4">
      <div className="font-mono text-xs uppercase tracking-widest text-[var(--color-muted)]">
        {label}
      </div>
      <div className={`mt-2 font-mono text-3xl ${muted ? "text-[var(--color-muted)]" : "text-[var(--color-text)]"}`}>
        {value}
      </div>
    </div>
  );
}
