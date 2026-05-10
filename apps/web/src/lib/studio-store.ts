// Studio client state (Phase 4 spec §10).
//
// Pure editor state shared by canvas / sidebar / tool picker. The annotation
// buffer here is the source of truth for what the user SEES — it's seeded
// from the server snapshot via `seedBuffer()` and then mutated through
// commands. Stage B autosave (next slice) reads dirty entries and PATCHes
// the API; the server cache stays in react-query.
//
// History (spec §10): command-pattern undo/redo with depth 50. Past
// commands live in `history`; redoables in `future`. Any new command
// clears `future` (standard editor behavior).
import { create } from "zustand";

import type { Annotation } from "./api";

export type Tool = "select" | "bbox" | "point" | "polygon" | "auto";

export const HISTORY_DEPTH = 50;

export type Command =
  | { type: "create"; annotation: Annotation }
  | { type: "update"; id: string; before: Annotation; after: Annotation }
  | { type: "delete"; annotation: Annotation };

export type StudioState = {
  imageId: string | null;
  /** Latest version received from the server; used as If-Match on PATCH. */
  setVersion: number | null;
  selectedTool: Tool;
  selectedAnnotationId: string | null;

  /** Editable annotation buffer. Keyed by annotation id. */
  buffer: Record<string, Annotation>;
  history: Command[];
  future: Command[];

  setImage(id: string | null): void;
  setSetVersion(v: number | null): void;
  setTool(tool: Tool): void;
  selectAnnotation(id: string | null): void;

  /** Replace the buffer with a fresh server snapshot. Clears history. */
  seedBuffer(annotations: Annotation[]): void;
  /** Apply a command, push it on history, clear future. */
  applyCommand(cmd: Command): void;
  undo(): void;
  redo(): void;

  reset(): void;
};

const initial = {
  imageId: null,
  setVersion: null,
  selectedTool: "select" as Tool,
  selectedAnnotationId: null,
  buffer: {} as Record<string, Annotation>,
  history: [] as Command[],
  future: [] as Command[],
};

function applyToBuffer(
  buffer: Record<string, Annotation>,
  cmd: Command,
): Record<string, Annotation> {
  switch (cmd.type) {
    case "create":
      return { ...buffer, [cmd.annotation.id]: cmd.annotation };
    case "update":
      return { ...buffer, [cmd.id]: cmd.after };
    case "delete": {
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      const { [cmd.annotation.id]: _gone, ...rest } = buffer;
      return rest;
    }
  }
}

function revertFromBuffer(
  buffer: Record<string, Annotation>,
  cmd: Command,
): Record<string, Annotation> {
  switch (cmd.type) {
    case "create": {
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      const { [cmd.annotation.id]: _gone, ...rest } = buffer;
      return rest;
    }
    case "update":
      return { ...buffer, [cmd.id]: cmd.before };
    case "delete":
      return { ...buffer, [cmd.annotation.id]: cmd.annotation };
  }
}

export const useStudio = create<StudioState>((set) => ({
  ...initial,
  setImage: (id) => set({ imageId: id, selectedAnnotationId: null }),
  setSetVersion: (v) => set({ setVersion: v }),
  setTool: (tool) => set({ selectedTool: tool }),
  selectAnnotation: (id) => set({ selectedAnnotationId: id }),

  seedBuffer: (annotations) =>
    set({
      buffer: Object.fromEntries(annotations.map((a) => [a.id, a])),
      history: [],
      future: [],
    }),

  applyCommand: (cmd) =>
    set((s) => {
      const nextHistory = [...s.history, cmd];
      // Cap history depth — drop the oldest entry once we exceed HISTORY_DEPTH.
      // Older edits become un-undoable but stay in the buffer.
      if (nextHistory.length > HISTORY_DEPTH) nextHistory.shift();
      return {
        buffer: applyToBuffer(s.buffer, cmd),
        history: nextHistory,
        future: [],
      };
    }),

  undo: () =>
    set((s) => {
      if (s.history.length === 0) return s;
      const cmd = s.history[s.history.length - 1];
      return {
        buffer: revertFromBuffer(s.buffer, cmd),
        history: s.history.slice(0, -1),
        future: [...s.future, cmd],
      };
    }),

  redo: () =>
    set((s) => {
      if (s.future.length === 0) return s;
      const cmd = s.future[s.future.length - 1];
      return {
        buffer: applyToBuffer(s.buffer, cmd),
        history: [...s.history, cmd],
        future: s.future.slice(0, -1),
      };
    }),

  reset: () => set(initial),
}));

/** Selectors — keep components subscribed to small slices of state. */
export const studioSelectors = {
  bufferList: (s: StudioState): Annotation[] => Object.values(s.buffer),
  canUndo: (s: StudioState): boolean => s.history.length > 0,
  canRedo: (s: StudioState): boolean => s.future.length > 0,
};
