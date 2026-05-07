"use client";

// Client wrapper that resolves the image record and hands off to StudioShell.
// Stage A uses the existing list endpoint + filter by id — adding a dedicated
// GET /v1/images/:id is tracked for Stage B together with presigned download
// URLs (the public bucket URL is dev-only).
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import { api, type ImageRecord } from "@/lib/api";
import { env } from "@/lib/env";
import { StudioShell } from "@/components/studio/studio-shell";

export function StudioPage({ imageId }: { imageId: string }) {
  const list = useQuery({
    queryKey: ["images"],
    queryFn: () => api.listImages(200),
    staleTime: 30_000,
  });

  const image: ImageRecord | undefined = useMemo(
    () => list.data?.items.find((i) => i.id === imageId),
    [list.data, imageId],
  );

  if (list.isLoading) {
    return (
      <div className="grid h-[100dvh] place-items-center text-sm text-[var(--color-muted)]">
        Loading image…
      </div>
    );
  }
  if (list.isError) {
    return (
      <div role="alert" className="grid h-[100dvh] place-items-center text-sm text-[var(--color-danger)]">
        Failed to load images.
      </div>
    );
  }
  if (!image) {
    return (
      <div role="alert" className="grid h-[100dvh] place-items-center text-sm text-[var(--color-muted)]">
        Image not found.
      </div>
    );
  }

  const imageUrl = `${env.STORAGE_PUBLIC_URL}/${image.storage_key}`;
  return <StudioShell image={image} imageUrl={imageUrl} />;
}
