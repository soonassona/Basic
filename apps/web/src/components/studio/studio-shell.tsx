"use client";

// Top-level studio client component. Holds:
//   - the react-query loader for the annotation set,
//   - the Konva canvas (loaded via dynamic to keep it out of SSR), and
//   - the sidebar + tool picker rails.
//
// Stage B will add: tool draw handlers, autosave (debounce 2000ms) with
// If-Match=set.version, 409 conflict UI, undo/redo, keyboard shortcuts.
import { useQuery } from "@tanstack/react-query";
import dynamic from "next/dynamic";
import { useEffect } from "react";

import { api, type ImageRecord } from "@/lib/api";
import { useStudio } from "@/lib/studio-store";
import { ToolPicker } from "./tool-picker";
import { StudioSidebar } from "./sidebar";

const StudioCanvas = dynamic(
  () => import("./canvas").then((m) => m.StudioCanvas),
  {
    ssr: false,
    loading: () => (
      <div className="grid h-full place-items-center text-sm text-[var(--color-muted)]">
        Loading canvas…
      </div>
    ),
  },
);

export function StudioShell({ image, imageUrl }: { image: ImageRecord; imageUrl: string }) {
  const setImage = useStudio((s) => s.setImage);
  const setSetVersion = useStudio((s) => s.setSetVersion);
  const reset = useStudio((s) => s.reset);

  useEffect(() => {
    setImage(image.id);
    return () => reset();
  }, [image.id, setImage, reset]);

  const setQuery = useQuery({
    queryKey: ["annotation-set", image.id],
    queryFn: () => api.getAnnotationSet(image.id),
  });

  useEffect(() => {
    if (setQuery.data) setSetVersion(setQuery.data.version);
  }, [setQuery.data, setSetVersion]);

  return (
    <div className="grid h-[100dvh] grid-cols-[auto_1fr_320px] grid-rows-1">
      <ToolPicker />
      <section className="relative" aria-label="Annotation canvas">
        {setQuery.isLoading && (
          <div className="grid h-full place-items-center text-sm text-[var(--color-muted)]">
            Loading annotations…
          </div>
        )}
        {setQuery.isError && (
          <div role="alert" className="grid h-full place-items-center text-sm text-[var(--color-danger)]">
            Failed to load annotation set.
          </div>
        )}
        {setQuery.data && (
          <StudioCanvas image={image} imageUrl={imageUrl} set={setQuery.data} />
        )}
      </section>
      {setQuery.data ? (
        <StudioSidebar set={setQuery.data} />
      ) : (
        <div className="border-l border-[var(--color-border-2)]" />
      )}
    </div>
  );
}
