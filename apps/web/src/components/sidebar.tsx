"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useTranslations } from "next-intl";
import { signOut } from "@/lib/auth-client";
import { useRouter } from "next/navigation";

type Item = { href: string; key: string };
const ITEMS: Item[] = [
  { href: "/dashboard", key: "dashboard" },
  { href: "/images", key: "images" },
  { href: "/queue", key: "queue" },
  { href: "/jobs", key: "jobs" },
  { href: "/dataset", key: "dataset" },
  { href: "/models", key: "models" },
  { href: "/analytics", key: "analytics" },
  { href: "/settings/general", key: "settings" },
];

export function Sidebar() {
  const t = useTranslations("nav");
  const pathname = usePathname();
  const router = useRouter();

  const isActive = (href: string) => pathname === href || pathname.startsWith(`${href}/`);

  return (
    <nav className="surface flex flex-col gap-1 border-r p-4" aria-label="Primary">
      <div className="mb-6 px-2 font-mono text-xs uppercase tracking-widest text-[var(--color-muted)]">
        VisionLoop
      </div>
      {ITEMS.map((item) => (
        <Link
          key={item.key}
          href={item.href}
          aria-current={isActive(item.href) ? "page" : undefined}
          className={`rounded-md px-3 py-2 text-sm transition-colors ${
            isActive(item.href)
              ? "bg-[var(--color-border)] text-[var(--color-text)]"
              : "text-[var(--color-muted)] hover:bg-[var(--color-border)] hover:text-[var(--color-text)]"
          }`}
        >
          {t(item.key)}
        </Link>
      ))}
      <div className="mt-auto">
        <button
          type="button"
          className="btn w-full text-sm"
          onClick={async () => {
            await signOut();
            router.push("/login");
            router.refresh();
          }}
        >
          {t("logout")}
        </button>
      </div>
    </nav>
  );
}
