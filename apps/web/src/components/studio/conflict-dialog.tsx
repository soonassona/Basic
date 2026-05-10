"use client";

// 409 conflict dialog (Phase 4 spec §10).
//
// Slice B3 ships the minimal "the server moved on — discard your edits and
// reload?" prompt. A richer 3-way merge UI is tracked for Phase 8 polish.
import { useEffect, useRef } from "react";

export function ConflictDialog({
  open,
  currentVersion,
  localChanges,
  onDiscardAndReload,
  onDismiss,
}: {
  open: boolean;
  currentVersion: number;
  localChanges: number;
  onDiscardAndReload: () => void;
  onDismiss: () => void;
}) {
  const dismissRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (open) dismissRef.current?.focus();
  }, [open]);

  if (!open) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="conflict-title"
      data-testid="conflict-dialog"
      className="fixed inset-0 z-50 grid place-items-center bg-black/60"
    >
      <div className="surface w-[420px] rounded-md border border-[var(--color-border-2)] p-5">
        <h2 id="conflict-title" className="text-lg font-semibold">
          Annotation set updated by someone else
        </h2>
        <p className="mt-3 text-sm text-[var(--color-muted)]">
          The server has version{" "}
          <span className="font-mono text-[var(--color-text)]">{currentVersion}</span>;
          your edits were made against an older copy. Reload to pull the
          latest, or keep editing and resolve manually.
        </p>
        <p className="mt-3 text-xs text-[var(--color-warning)]">
          {localChanges} unsaved change{localChanges === 1 ? "" : "s"} will be discarded.
        </p>

        <div className="mt-5 flex justify-end gap-2">
          <button
            ref={dismissRef}
            type="button"
            onClick={onDismiss}
            data-testid="conflict-keep-editing"
            className="rounded-sm border border-[var(--color-border-2)] px-3 py-1.5 text-sm hover:bg-[var(--color-surface-2)]"
          >
            Keep editing
          </button>
          <button
            type="button"
            onClick={onDiscardAndReload}
            data-testid="conflict-discard-reload"
            className="rounded-sm bg-[var(--color-danger)] px-3 py-1.5 text-sm text-white hover:opacity-90"
          >
            Discard &amp; reload
          </button>
        </div>
      </div>
    </div>
  );
}
