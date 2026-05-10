// Autosave for the annotation studio (Phase 4 spec §10).
//
// Debounces 2000ms after the last edit, then PATCHes every dirty annotation
// with If-Match=setVersion. On 200 the row is marked saved + setVersion is
// bumped. On 409 we surface the conflict so the shell can render the
// resolution UI (Slice B3 minimal: "discard + reload").
//
// Creates and deletes are NOT autosaved here — POST /v1/annotations and
// DELETE /v1/annotations/:id don't exist yet (tracked as Stage B follow-up).
// The sidebar shows a "draft" banner when these exist so the user knows.
import { useEffect, useRef } from "react";

import { ApiClientError, api, type AnnotationPatch } from "./api";
import { studioSelectors, useStudio, type StudioState } from "./studio-store";

/** Debounce window for autosave (spec §10). */
export const AUTOSAVE_DEBOUNCE_MS = 2000;

export type ConflictDetected = (currentVersion: number) => void;

export type UseAutosaveOptions = {
  /** Called when a PATCH returns 409 — opens the conflict resolution UI. */
  onConflict: ConflictDetected;
  /** Called for any other PATCH failure (network, 4xx/5xx besides 409). */
  onError?: (err: unknown, annotationId: string) => void;
};

/** Builds the patch payload for a dirty annotation by diffing the buffer
 * entry against the original. Sends only fields that actually changed so
 * we never accidentally null out a field the server still owns. */
export function diffPatch(
  buffer: StudioState["buffer"],
  original: StudioState["original"],
  id: string,
): AnnotationPatch | null {
  const orig = original[id];
  const curr = buffer[id];
  if (!orig || !curr) return null;

  const patch: AnnotationPatch = {};
  if (JSON.stringify(orig.geometry) !== JSON.stringify(curr.geometry)) {
    patch.geometry = curr.geometry;
  }
  if (orig.label_id !== curr.label_id) {
    patch.label_id = curr.label_id;
  }
  if (orig.human_accepted !== curr.human_accepted) {
    patch.human_accepted = curr.human_accepted;
  }
  return Object.keys(patch).length > 0 ? patch : null;
}

export function useAutosave({ onConflict, onError }: UseAutosaveOptions): {
  /** Forces an immediate save bypass of the debounce — useful for the
   * conflict modal's "save anyway" path or visibility-change handlers. */
  flush: () => Promise<void>;
} {
  const dirtyIds = useStudio(studioSelectors.dirtyIds);
  const setVersion = useStudio((s) => s.setVersion);
  const setSetVersion = useStudio((s) => s.setSetVersion);
  const markSaved = useStudio((s) => s.markSaved);

  // Refs so the debounce timer reads the latest values without retriggering.
  const dirtyRef = useRef(dirtyIds);
  const versionRef = useRef(setVersion);
  const inFlight = useRef(false);
  dirtyRef.current = dirtyIds;
  versionRef.current = setVersion;

  const onConflictRef = useRef(onConflict);
  const onErrorRef = useRef(onError);
  onConflictRef.current = onConflict;
  onErrorRef.current = onError;

  async function flush() {
    if (inFlight.current) return;
    const ids = dirtyRef.current;
    const version = versionRef.current;
    if (ids.length === 0 || version == null) return;

    inFlight.current = true;
    try {
      const s = useStudio.getState();
      // Sequential to keep version monotonic — each successful PATCH bumps
      // the version which the next PATCH must use as If-Match.
      let v = version;
      for (const id of ids) {
        const patch = diffPatch(s.buffer, s.original, id);
        if (!patch) continue;
        try {
          const out = await api.patchAnnotation(id, v, patch);
          v = out.new_version;
          markSaved(id);
        } catch (err) {
          if (err instanceof ApiClientError && err.status === 409) {
            const cv = err.currentVersion;
            if (typeof cv === "number") {
              onConflictRef.current(cv);
            } else {
              onErrorRef.current?.(err, id);
            }
            return; // stop the batch on conflict
          }
          onErrorRef.current?.(err, id);
          return; // stop on first error to avoid version drift
        }
      }
      setSetVersion(v);
    } finally {
      inFlight.current = false;
    }
  }

  useEffect(() => {
    if (dirtyIds.length === 0) return;
    const timer = setTimeout(flush, AUTOSAVE_DEBOUNCE_MS);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dirtyIds.length, setVersion]);

  return { flush };
}
