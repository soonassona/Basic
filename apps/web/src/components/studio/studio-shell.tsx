"use client";

// Top-level studio client component (Phase 4 §10).
//   - Loads the annotation set via react-query
//   - Seeds the studio buffer once the data lands (single source of truth
//     for the canvas + sidebar)
//   - Hosts the canvas (dynamic ssr:false) + tool picker + sidebar
//
// Slice B2 will mount keyboard shortcuts here; Slice B3 wires autosave that
// reads the dirty entries off the buffer.
import { useQuery } from "@tanstack/react-query";
import dynamic from "next/dynamic";
import { useEffect, useRef } from "react";

import { api, type ImageRecord } from "@/lib/api";
import { useStudio } from "@/lib/studio-store";
import { useStudioShortcuts } from "@/lib/use-studio-shortcuts";
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

export function StudioShell({
  image,
  imageUrl,
  prevImageId,
  nextImageId,
}: {
  image: ImageRecord;
  imageUrl: string;
  prevImageId: string | null;
  nextImageId: string | null;
}) {
  const setImage = useStudio((s) => s.setImage);
  const setSetVersion = useStudio((s) => s.setSetVersion);
  const seedBuffer = useStudio((s) => s.seedBuffer);
  const reset = useStudio((s) => s.reset);

  // Shared ref so the L shortcut can focus the label picker button living
  // in the sidebar.
  const labelPickerRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    setImage(image.id);
    return () => reset();
  }, [image.id, setImage, reset]);

  const setQuery = useQuery({
    queryKey: ["annotation-set", image.id],
    queryFn: () => api.getAnnotationSet(image.id),
  });

  // Seed the buffer + version once the server snapshot lands. Re-seeding
  // would clobber unsaved local edits, so we tie this to the query data
  // identity (changes only on a fresh fetch).
  useEffect(() => {
    if (!setQuery.data) return;
    setSetVersion(setQuery.data.version);
    seedBuffer(setQuery.data.annotations);
  }, [setQuery.data, setSetVersion, seedBuffer]);

  // Mount the full keyboard-shortcut bundle (spec §10, all-or-nothing).
  useStudioShortcuts({ prevImageId, nextImageId, labelPickerRef });

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
          <StudioCanvas
            image={image}
            imageUrl={imageUrl}
            annotationSetId={setQuery.data.id}
          />
        )}
      </section>
      {setQuery.data ? (
        <StudioSidebar
          setId={setQuery.data.id}
          setVersion={setQuery.data.version}
          labelPickerRef={labelPickerRef}
        />
      ) : (
        <div className="border-l border-[var(--color-border-2)]" />
      )}
    </div>
  );
}
