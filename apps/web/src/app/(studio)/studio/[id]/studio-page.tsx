"use client";

// Client wrapper that resolves the image record + presigned download URL
// and hands off to StudioShell. Slice B5 swapped the list+filter shortcut
// (and the dev-only MinIO public bucket dependency) for the dedicated
// GET /v1/images/:id endpoint, which returns an image-scoped presigned URL.
//
// Prev/next sibling ids for the ←/→ shortcut still come from the images
// list — that's a navigation concern, not a load concern.
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import { api } from "@/lib/api";
import { StudioShell } from "@/components/studio/studio-shell";

export function StudioPage({ imageId }: { imageId: string }) {
  // Primary load: the image + its presigned URL.
  const imageQuery = useQuery({
    queryKey: ["image", imageId],
    queryFn: () => api.getImage(imageId),
    // Presigned URLs eventually expire (15 min default); refetch a bit
    // before that so a long studio session doesn't load a stale URL.
    staleTime: 10 * 60 * 1000,
  });

  // Secondary load: the images list, only for ←/→ navigation. Cached
  // aggressively because the order rarely changes during a session.
  const list = useQuery({
    queryKey: ["images"],
    queryFn: () => api.listImages(200),
    staleTime: 60_000,
  });

  const { prevImageId, nextImageId } = useMemo(() => {
    const items = list.data?.items ?? [];
    const idx = items.findIndex((i) => i.id === imageId);
    if (idx < 0) return { prevImageId: null, nextImageId: null };
    return {
      prevImageId: idx > 0 ? items[idx - 1].id : null,
      nextImageId: idx < items.length - 1 ? items[idx + 1].id : null,
    };
  }, [list.data, imageId]);

  if (imageQuery.isLoading) {
    return (
      <div className="grid h-[100dvh] place-items-center text-sm text-[var(--color-muted)]">
        Loading image…
      </div>
    );
  }
  if (imageQuery.isError || !imageQuery.data) {
    return (
      <div role="alert" className="grid h-[100dvh] place-items-center text-sm text-[var(--color-danger)]">
        Image not found.
      </div>
    );
  }

  const { image, download } = imageQuery.data;
  return (
    <StudioShell
      image={image}
      imageUrl={download.url}
      prevImageId={prevImageId}
      nextImageId={nextImageId}
    />
  );
}
