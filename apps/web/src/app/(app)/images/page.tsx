"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import Link from "next/link";
import { useRef, useState } from "react";
import { api, type ImageRecord, ApiClientError } from "@/lib/api";

const ACCEPTED = ["image/jpeg", "image/png", "image/webp"];
const MAX_BYTES = 50 * 1024 * 1024;

export default function ImagesPage() {
  const t = useTranslations("images");
  const inputRef = useRef<HTMLInputElement>(null);
  const qc = useQueryClient();
  const [error, setError] = useState<string | null>(null);

  const list = useQuery({ queryKey: ["images"], queryFn: () => api.listImages(50) });

  const uploadMutation = useMutation({
    mutationFn: async (files: File[]) => {
      for (const file of files) {
        if (!ACCEPTED.includes(file.type)) {
          throw new Error(`Unsupported type: ${file.type}`);
        }
        if (file.size > MAX_BYTES) {
          throw new Error(`Too large: ${file.name}`);
        }
        const presign = await api.presignUpload({
          content_type: file.type,
          byte_size: file.size,
        });

        const put = await fetch(presign.upload.url, {
          method: presign.upload.method,
          headers: presign.upload.headers,
          body: file,
        });
        if (!put.ok) throw new Error(`Upload failed: ${put.status}`);

        const dims = await loadImageDimensions(file);
        await api.confirmUpload(presign.image.id, dims);
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["images"] });
      setError(null);
    },
    onError: (err) => {
      setError(err instanceof ApiClientError ? `${err.code}: ${err.message}` : err.message);
    },
  });

  function onPick(files: FileList | null) {
    if (!files || files.length === 0) return;
    setError(null);
    uploadMutation.mutate(Array.from(files));
  }

  return (
    <div className="space-y-6">
      <header className="flex items-center justify-between">
        <h1 className="text-3xl font-semibold">{t("title")}</h1>
        <button
          type="button"
          className="btn btn-primary"
          onClick={() => inputRef.current?.click()}
          disabled={uploadMutation.isPending}
        >
          {t("upload")}
        </button>
        <input
          ref={inputRef}
          type="file"
          accept={ACCEPTED.join(",")}
          multiple
          className="hidden"
          aria-label={t("upload")}
          data-testid="image-upload-input"
          onChange={(e) => onPick(e.target.files)}
        />
      </header>

      <DropZone onFiles={onPick} disabled={uploadMutation.isPending} label={t("drop_zone")} />

      {uploadMutation.isPending && (
        <p className="text-sm text-[var(--color-muted)]">
          {t("uploading", { count: 1 })}
        </p>
      )}
      {error && (
        <p role="alert" className="text-sm text-[var(--color-danger)]">
          {error}
        </p>
      )}

      <ImagesGrid items={list.data?.items ?? []} emptyLabel={t("empty")} />
    </div>
  );
}

function DropZone({
  onFiles,
  disabled,
  label,
}: {
  onFiles: (files: FileList | null) => void;
  disabled: boolean;
  label: string;
}) {
  const [hover, setHover] = useState(false);
  return (
    <div
      role="region"
      aria-label={label}
      onDragOver={(e) => {
        e.preventDefault();
        setHover(true);
      }}
      onDragLeave={() => setHover(false)}
      onDrop={(e) => {
        e.preventDefault();
        setHover(false);
        if (!disabled) onFiles(e.dataTransfer.files);
      }}
      className={`surface flex h-40 items-center justify-center rounded-md border-2 border-dashed transition-colors ${
        hover ? "border-[var(--color-primary)]" : "border-[var(--color-border-2)]"
      }`}
    >
      <span className="text-sm text-[var(--color-muted)]">{label}</span>
    </div>
  );
}

function ImagesGrid({ items, emptyLabel }: { items: ImageRecord[]; emptyLabel: string }) {
  if (items.length === 0) {
    return <p className="text-[var(--color-muted)]">{emptyLabel}</p>;
  }
  return (
    <ul className="grid gap-3 md:grid-cols-3 lg:grid-cols-4" data-testid="images-grid">
      {items.map((img) => (
        <li key={img.id} className="surface rounded-md p-3" data-testid="image-card">
          <Link
            href={`/studio/${img.id}`}
            className="block"
            aria-label={`Open ${img.storage_key} in studio`}
          >
            <div className="font-mono text-xs text-[var(--color-muted)]">{img.id.slice(0, 8)}</div>
            <div className="mt-1 truncate text-sm">{img.storage_key}</div>
            <div className="mt-2 flex items-center justify-between text-xs text-[var(--color-muted)]">
              <span>
                {img.width && img.height ? `${img.width}×${img.height}` : "—"}
              </span>
              <StatusPill status={img.status} />
            </div>
          </Link>
        </li>
      ))}
    </ul>
  );
}

function StatusPill({ status }: { status: ImageRecord["status"] }) {
  const color =
    status === "ready"
      ? "var(--color-success)"
      : status === "errored"
      ? "var(--color-danger)"
      : "var(--color-muted)";
  return (
    <span style={{ color }} className="font-mono uppercase">
      {status}
    </span>
  );
}

async function loadImageDimensions(file: File): Promise<{ width: number; height: number }> {
  const url = URL.createObjectURL(file);
  try {
    const img = await new Promise<HTMLImageElement>((resolve, reject) => {
      const i = new Image();
      i.onload = () => resolve(i);
      i.onerror = () => reject(new Error("decode failed"));
      i.src = url;
    });
    return { width: img.naturalWidth, height: img.naturalHeight };
  } finally {
    URL.revokeObjectURL(url);
  }
}
