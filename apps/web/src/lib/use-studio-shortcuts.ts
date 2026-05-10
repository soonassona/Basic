// Keyboard shortcuts for the annotation studio (Phase 4 spec §10).
//
// CLAUDE.md non-negotiable rule: shortcuts ship all-or-nothing. The full
// 8-shortcut bundle is wired here; partial deployments are forbidden.
//
//   A         accept selected annotation (sets human_accepted = true)
//   R         reject selected annotation (sets human_accepted = false)
//   L         focus the label picker
//   D         delete selected annotation
//   Z         undo
//   Shift+Z   redo
//   Esc       deselect
//   ←  →      previous / next image
//
// Shortcuts are guarded against input focus per spec — typing in a textarea
// or contenteditable element never triggers an editor action.
import { useEffect, type RefObject } from "react";
import { useRouter } from "next/navigation";

import type { Annotation } from "./api";
import { useStudio } from "./studio-store";

export type ShortcutAction =
  | "accept"
  | "reject"
  | "label"
  | "delete"
  | "undo"
  | "redo"
  | "deselect"
  | "prev"
  | "next";

/** Pure key → action mapping. Returns null for non-shortcut keys, or when
 * the event originated inside an editable element. Exported for tests. */
export function shortcutFor(e: KeyboardEvent): ShortcutAction | null {
  if (isEditable(e.target)) return null;

  // Z / Shift+Z handled before the alpha block so Shift is honored.
  if (e.key === "z" || e.key === "Z") {
    return e.shiftKey ? "redo" : "undo";
  }
  // Modifier-bearing keys (Cmd/Ctrl/Alt) are ignored to avoid stealing
  // browser shortcuts. Shift on its own is fine for letter mapping below.
  if (e.metaKey || e.ctrlKey || e.altKey) return null;

  switch (e.key) {
    case "a":
    case "A":
      return "accept";
    case "r":
    case "R":
      return "reject";
    case "l":
    case "L":
      return "label";
    case "d":
    case "D":
      return "delete";
    case "Escape":
      return "deselect";
    case "ArrowLeft":
      return "prev";
    case "ArrowRight":
      return "next";
    default:
      return null;
  }
}

/** Skip shortcut handling when typing in form controls. Mirrors the spec's
 * "guarded against input focus" requirement. Exported for tests. */
export function isEditable(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  // isContentEditable isn't reliably populated outside real browsers (jsdom
  // returns undefined), so fall back to the attribute itself.
  return (
    target.isContentEditable === true ||
    target.getAttribute("contenteditable") === "true"
  );
}

export type UseStudioShortcutsOptions = {
  /** Image to navigate to on ArrowLeft. Null disables the shortcut. */
  prevImageId: string | null;
  /** Image to navigate to on ArrowRight. Null disables the shortcut. */
  nextImageId: string | null;
  /** Element to focus on L. Usually the label picker button. */
  labelPickerRef: RefObject<HTMLElement | null>;
};

/** Mounts the global keydown listener and dispatches actions against the
 * studio store + router. Runs only once per page (guard against double-mount
 * via React strict mode is implicit in the cleanup). */
export function useStudioShortcuts({
  prevImageId,
  nextImageId,
  labelPickerRef,
}: UseStudioShortcutsOptions): void {
  const router = useRouter();

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      const action = shortcutFor(e);
      if (!action) return;

      const s = useStudio.getState();
      const selectedId = s.selectedAnnotationId;
      const selected: Annotation | undefined = selectedId
        ? s.buffer[selectedId]
        : undefined;

      switch (action) {
        case "accept":
          if (selected && selected.human_accepted !== true) {
            s.applyCommand({
              type: "update",
              id: selected.id,
              before: selected,
              after: { ...selected, human_accepted: true },
            });
            e.preventDefault();
          }
          break;

        case "reject":
          if (selected && selected.human_accepted !== false) {
            s.applyCommand({
              type: "update",
              id: selected.id,
              before: selected,
              after: { ...selected, human_accepted: false },
            });
            e.preventDefault();
          }
          break;

        case "label":
          labelPickerRef.current?.focus();
          e.preventDefault();
          break;

        case "delete":
          if (selected) {
            s.applyCommand({ type: "delete", annotation: selected });
            s.selectAnnotation(null);
            e.preventDefault();
          }
          break;

        case "undo":
          s.undo();
          e.preventDefault();
          break;

        case "redo":
          s.redo();
          e.preventDefault();
          break;

        case "deselect":
          s.selectAnnotation(null);
          e.preventDefault();
          break;

        case "prev":
          if (prevImageId) {
            router.push(`/studio/${prevImageId}`);
            e.preventDefault();
          }
          break;

        case "next":
          if (nextImageId) {
            router.push(`/studio/${nextImageId}`);
            e.preventDefault();
          }
          break;
      }
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [router, prevImageId, nextImageId, labelPickerRef]);
}
