// Studio client state (Phase 4 spec §10).
//
// Pure UI state that the canvas / sidebar / tool picker share. Persistence
// belongs to the API client (see lib/api.ts) — Stage B will wire autosave
// against this store. We deliberately do NOT mirror the annotation array
// here yet: react-query owns the server cache, the store only tracks
// editor-local state (selected tool, selected annotation, draft set version).
import { create } from "zustand";

export type Tool = "select" | "bbox" | "point" | "polygon" | "auto";

export type StudioState = {
  imageId: string | null;
  /** Latest version received from the server; used as If-Match on PATCH. */
  setVersion: number | null;
  selectedTool: Tool;
  selectedAnnotationId: string | null;

  setImage(id: string | null): void;
  setSetVersion(v: number | null): void;
  setTool(tool: Tool): void;
  selectAnnotation(id: string | null): void;
  reset(): void;
};

const initial = {
  imageId: null,
  setVersion: null,
  selectedTool: "select" as Tool,
  selectedAnnotationId: null,
};

export const useStudio = create<StudioState>((set) => ({
  ...initial,
  setImage: (id) => set({ imageId: id, selectedAnnotationId: null }),
  setSetVersion: (v) => set({ setVersion: v }),
  setTool: (tool) => set({ selectedTool: tool }),
  selectAnnotation: (id) => set({ selectedAnnotationId: id }),
  reset: () => set(initial),
}));
