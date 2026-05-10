// Autosave for the annotation studio (Phase 4 spec §10).
//
// Debounces 2000ms after the last edit, then walks three batches in order:
//   1. POST every locally-created annotation (createdIds)
//   2. PATCH every dirty existing annotation (dirtyIds)
//   3. DELETE every locally-deleted annotation (deletedIds)
// Each verb bumps the parent set's version, so the chain threads the
// returned new_version forward as If-Match for the next request. On 200/201
// the row is reconciled with the server (replaceAnnotationId / markSaved /
// forgetOriginal). On 409 the conflict callback fires and the loop stops
// to avoid version drift.
import { useEffect, useRef } from "react";

import { ApiClientError, api, type Annotation, type AnnotationKind, type AnnotationPatch } from "./api";
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
  const createdIds = useStudio(studioSelectors.createdIds);
  const deletedIds = useStudio(studioSelectors.deletedIds);
  const setVersion = useStudio((s) => s.setVersion);
  const setSetVersion = useStudio((s) => s.setSetVersion);
  const markSaved = useStudio((s) => s.markSaved);
  const replaceAnnotationId = useStudio((s) => s.replaceAnnotationId);
  const forgetOriginal = useStudio((s) => s.forgetOriginal);

  const inFlight = useRef(false);
  const onConflictRef = useRef(onConflict);
  const onErrorRef = useRef(onError);
  onConflictRef.current = onConflict;
  onErrorRef.current = onError;

  // Aggregate signal that triggers the debounced flush — re-running the
  // effect on either dirty / created / deleted change keeps the debounce
  // window tied to the latest edit regardless of which kind it was.
  const pendingCount = dirtyIds.length + createdIds.length + deletedIds.length;

  function handleErr(err: unknown, id: string): "conflict" | "error" {
    if (err instanceof ApiClientError && err.status === 409) {
      const cv = err.currentVersion;
      if (typeof cv === "number") onConflictRef.current(cv);
      else onErrorRef.current?.(err, id);
      return "conflict";
    }
    onErrorRef.current?.(err, id);
    return "error";
  }

  async function flush() {
    if (inFlight.current) return;
    const version = setVersion;
    if (version == null) return;

    inFlight.current = true;
    try {
      const s = useStudio.getState();
      let v = version;

      // 1. Creates first — they assign permanent ids, which subsequent
      //    patches in the same batch may target.
      for (const localId of studioSelectors.createdIds(s)) {
        const localAnn = s.buffer[localId];
        if (!localAnn) continue;
        try {
          const out = await api.createAnnotation(v, {
            annotation_set_id: localAnn.annotation_set_id,
            kind: localAnn.kind as AnnotationKind,
            geometry: localAnn.geometry,
            label_id: localAnn.label_id,
          });
          v = out.new_version;
          replaceAnnotationId(localId, out.annotation as Annotation);
        } catch (err) {
          if (handleErr(err, localId) !== "error") return;
          return;
        }
      }

      // 2. Updates against existing rows.
      const liveState = useStudio.getState();
      for (const id of studioSelectors.dirtyIds(liveState)) {
        const patch = diffPatch(liveState.buffer, liveState.original, id);
        if (!patch) continue;
        try {
          const out = await api.patchAnnotation(id, v, patch);
          v = out.new_version;
          markSaved(id);
        } catch (err) {
          if (handleErr(err, id) !== "error") return;
          return;
        }
      }

      // 3. Deletes.
      for (const id of studioSelectors.deletedIds(liveState)) {
        try {
          const out = await api.deleteAnnotation(id, v);
          v = out.new_version;
          forgetOriginal(id);
        } catch (err) {
          if (handleErr(err, id) !== "error") return;
          return;
        }
      }

      setSetVersion(v);
    } finally {
      inFlight.current = false;
    }
  }

  useEffect(() => {
    if (pendingCount === 0) return;
    const timer = setTimeout(flush, AUTOSAVE_DEBOUNCE_MS);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pendingCount, setVersion]);

  return { flush };
}
