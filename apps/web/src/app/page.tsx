import Link from "next/link";
import { useTranslations } from "next-intl";

export default function MarketingHome() {
  const t = useTranslations("marketing");
  return (
    <main className="mx-auto flex min-h-[100dvh] max-w-5xl flex-col justify-center px-6 py-24">
      <span className="font-mono text-xs uppercase tracking-widest text-[var(--color-muted)]">
        {t("title")}
      </span>
      <h1 className="mt-4 text-4xl font-semibold leading-tight text-[var(--color-text)] md:text-5xl">
        {t("tagline")}
      </h1>
      <p className="mt-6 max-w-2xl text-lg text-[var(--color-muted)]">{t("subtitle")}</p>
      <div className="mt-10 flex gap-3">
        <Link href="/register" className="btn btn-primary">
          {t("cta_register")}
        </Link>
        <Link href="/login" className="btn">
          {t("cta_login")}
        </Link>
      </div>

      <section className="mt-20 grid gap-4 md:grid-cols-3">
        <Card title="Two production models" body="SAM 2.1 and YOLOv11 collaborate per job — boxes feed segmentation, segments feed retraining." />
        <Card title="Active learning, not active waiting" body="Uncertainty scores rank the queue so reviewers spend time where the model needs help most." />
        <Card title="Closes the loop" body="Continuous training kicks off only when acceptance and metrics improve. Quality gates keep regressions out of production." />
      </section>
    </main>
  );
}

function Card({ title, body }: { title: string; body: string }) {
  return (
    <div className="surface rounded-md p-5">
      <h2 className="font-mono text-xs uppercase tracking-widest text-[var(--color-muted)]">
        {title}
      </h2>
      <p className="mt-2 text-[var(--color-text)]">{body}</p>
    </div>
  );
}
