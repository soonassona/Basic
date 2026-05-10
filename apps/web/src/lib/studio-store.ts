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
  /** Pristine server snapshot. Used to derive dirty/created/deleted ids
   * for autosave (Slice B3). Never mutated after seedBuffer. */
  original: Record<string, Annotation>;
  history: Command[];
  future: Command[];

  setImage(id: string | null): void;
  setSetVersion(v: number | null): void;
  setTool(tool: Tool): void;
  selectAnnotation(id: string | null): void;

  /** Replace the buffer + original with a fresh server snapshot. Clears history. */
  seedBuffer(annotations: Annotation[]): void;
  /** Mark an existing annotation as clean — removes it from the dirty set
   * by overwriting `original[id]` with the current buffer entry. Called after
   * a successful PATCH so the autosave loop doesn't re-save the same row. */
  markSaved(id: string): void;
  /** Swap a local-only annotation id for the server-assigned id and seed
   * the original snapshot with the server's row. Called after a successful
   * POST so subsequent edits PATCH against the real id. */
  replaceAnnotationId(localId: string, serverAnnotation: Annotation): void;
  /** Drop an id from the original snapshot — used after a successful DELETE
   * so the row stops appearing in deletedIds. */
  forgetOriginal(id: string): void;
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
  original: {} as Record<string, Annotation>,
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

  seedBuffer: (annotations) => {
    const snapshot = Object.fromEntries(annotations.map((a) => [a.id, a]));
    set({
      buffer: snapshot,
      original: snapshot,
      history: [],
      future: [],
    });
  },

  markSaved: (id) =>
    set((s) => {
      const current = s.buffer[id];
      if (!current) return s;
      return { original: { ...s.original, [id]: current } };
    }),

  replaceAnnotationId: (localId, serverAnnotation) =>
    set((s) => {
      if (!s.buffer[localId]) return s;
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      const { [localId]: _gone, ...restBuf } = s.buffer;
      const newBuf = { ...restBuf, [serverAnnotation.id]: serverAnnotation };
      const newOrig = { ...s.original, [serverAnnotation.id]: serverAnnotation };
      return {
        buffer: newBuf,
        original: newOrig,
        selectedAnnotationId:
          s.selectedAnnotationId === localId ? serverAnnotation.id : s.selectedAnnotationId,
      };
    }),

  forgetOriginal: (id) =>
    set((s) => {
      if (!s.original[id]) return s;
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      const { [id]: _gone, ...rest } = s.original;
      return { original: rest };
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

/** Stable JSON for shallow content comparison. Annotation geometry is plain
 * primitives so insertion order matches between buffer and original copies. */
function annotationSig(a: Annotation): string {
  return JSON.stringify([a.label_id, a.kind, a.geometry, a.human_accepted]);
}

/** Selectors — keep components subscribed to small slices of state. */
export const studioSelectors = {
  bufferList: (s: StudioState): Annotation[] => Object.values(s.buffer),
  canUndo: (s: StudioState): boolean => s.history.length > 0,
  canRedo: (s: StudioState): boolean => s.future.length > 0,

  /** IDs of existing (originally-from-server) annotations whose buffer
   * content differs from the snapshot. These are what autosave PATCHes. */
  dirtyIds: (s: StudioState): string[] => {
    const out: string[] = [];
    for (const id of Object.keys(s.buffer)) {
      const orig = s.original[id];
      if (!orig) continue; // brand-new (created locally) — see createdIds
      if (annotationSig(orig) !== annotationSig(s.buffer[id])) out.push(id);
    }
    return out;
  },

  /** IDs of annotations that exist locally but not on the server. Until a
   * POST /v1/annotations endpoint lands these are draft-only — surfaced to
   * the user via a banner. */
  createdIds: (s: StudioState): string[] =>
    Object.keys(s.buffer).filter((id) => !s.original[id]),

  /** IDs the server still has but the user has deleted locally. Until a
   * DELETE endpoint lands these are draft-only too. */
  deletedIds: (s: StudioState): string[] =>
    Object.keys(s.original).filter((id) => !s.buffer[id]),
};
