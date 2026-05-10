"use client";

// Studio right rail — annotation list + label picker placeholder + history
// controls. Reads annotations from the studio buffer (the canvas's source of
// truth), so a freshly drawn bbox shows up immediately without a server
// roundtrip. Keyboard A/R/L/D shortcuts (spec §10) target the same actions
// via the studio store.
import type { RefObject } from "react";

import type { Annotation } from "@/lib/api";
import { studioSelectors, useStudio } from "@/lib/studio-store";

export function StudioSidebar({
  setId,
  setVersion,
  labelPickerRef,
}: {
  setId: string;
  setVersion: number;
  labelPickerRef?: RefObject<HTMLButtonElement | null>;
}) {
  const annotations = useStudio(studioSelectors.bufferList);
  const selectedId = useStudio((s) => s.selectedAnnotationId);
  const select = useStudio((s) => s.selectAnnotation);

  const undo = useStudio((s) => s.undo);
  const redo = useStudio((s) => s.redo);
  const canUndo = useStudio(studioSelectors.canUndo);
  const canRedo = useStudio(studioSelectors.canRedo);
  const applyCommand = useStudio((s) => s.applyCommand);

  // Local-only counts that aren't yet persistable (POST/DELETE annotation
  // endpoints not implemented). Surfaced in the banner so the user knows
  // these won't survive a reload yet.
  const draftCreates = useStudio((s) => studioSelectors.createdIds(s).length);
  const draftDeletes = useStudio((s) => studioSelectors.deletedIds(s).length);
  const dirty = useStudio((s) => studioSelectors.dirtyIds(s).length);

  const selected = selectedId ? annotations.find((a) => a.id === selectedId) : undefined;

  return (
    <aside
      aria-label="Annotation sidebar"
      className="flex h-full flex-col border-l border-[var(--color-border-2)]"
      data-testid="studio-sidebar"
    >
      <header className="border-b border-[var(--color-border-2)] p-3">
        <div className="text-xs uppercase text-[var(--color-muted)]">Set</div>
        <div className="mt-1 font-mono text-xs">{setId.slice(0, 8)}</div>
        <div className="mt-2 text-xs text-[var(--color-muted)]">
          version <span className="font-mono">{setVersion}</span> · {annotations.length}
          {annotations.length === 1 ? " annotation" : " annotations"}
        </div>
        {dirty > 0 && (
          <div data-testid="dirty-banner" className="mt-2 text-xs text-[var(--color-primary)]">
            saving {dirty} edit{dirty === 1 ? "" : "s"}…
          </div>
        )}
        {(draftCreates > 0 || draftDeletes > 0) && (
          <div data-testid="draft-banner" className="mt-2 text-xs text-[var(--color-primary)]">
            {draftCreates > 0 && (
              <>
                {draftCreates} new · saving…
              </>
            )}
            {draftCreates > 0 && draftDeletes > 0 && " · "}
            {draftDeletes > 0 && (
              <>
                {draftDeletes} pending delete{draftDeletes === 1 ? "" : "s"}
              </>
            )}
          </div>
        )}
        <div className="mt-3 flex gap-1">
          <button
            type="button"
            onClick={undo}
            disabled={!canUndo}
            data-testid="undo-button"
            aria-label="Undo"
            className="flex-1 rounded-sm border border-[var(--color-border-2)] px-2 py-1 text-xs disabled:opacity-40 hover:bg-[var(--color-surface-2)]"
          >
            Undo
          </button>
          <button
            type="button"
            onClick={redo}
            disabled={!canRedo}
            data-testid="redo-button"
            aria-label="Redo"
            className="flex-1 rounded-sm border border-[var(--color-border-2)] px-2 py-1 text-xs disabled:opacity-40 hover:bg-[var(--color-surface-2)]"
          >
            Redo
          </button>
          <button
            type="button"
            onClick={() => {
              if (selected) applyCommand({ type: "delete", annotation: selected });
            }}
            disabled={!selected}
            data-testid="delete-button"
            aria-label="Delete selected"
            className="flex-1 rounded-sm border border-[var(--color-border-2)] px-2 py-1 text-xs disabled:opacity-40 hover:bg-[var(--color-surface-2)]"
          >
            Delete
          </button>
        </div>
      </header>

      <ul className="flex-1 overflow-y-auto" data-testid="annotation-list">
        {annotations.length === 0 ? (
          <li className="p-4 text-sm text-[var(--color-muted)]">
            No annotations yet — switch to the Box tool and drag to draw.
          </li>
        ) : (
          annotations.map((a) => (
            <AnnotationRow
              key={a.id}
              annotation={a}
              active={a.id === selectedId}
              onSelect={() => select(a.id)}
            />
          ))
        )}
      </ul>

      <footer className="border-t border-[var(--color-border-2)] p-3">
        <div className="text-xs uppercase text-[var(--color-muted)]">
          Label <kbd className="font-mono text-[10px] opacity-70">L</kbd>
        </div>
        <button
          ref={labelPickerRef}
          type="button"
          aria-label="Label picker"
          data-testid="label-picker-button"
          className="mt-2 w-full rounded-sm border border-[var(--color-border-2)] px-2 py-1.5 text-left text-sm text-[var(--color-muted)] focus:border-[var(--color-primary)] focus:outline-none"
        >
          {/* TODO Slice B4: wire label dropdown options. The L shortcut
              already focuses this button via labelPickerRef. */}
          Pick a label…
        </button>
      </footer>
    </aside>
  );
}

function AnnotationRow({
  annotation,
  active,
  onSelect,
}: {
  annotation: Annotation;
  active: boolean;
  onSelect: () => void;
}) {
  const accepted = annotation.human_accepted;
  const status = accepted === true ? "✓" : accepted === false ? "✗" : "·";
  return (
    <li>
      <button
        type="button"
        onClick={onSelect}
        aria-pressed={active}
        data-testid={`annotation-row-${annotation.id}`}
        className={`flex w-full items-center justify-between border-b border-[var(--color-border-2)] px-3 py-2 text-left text-xs ${
          active ? "bg-[var(--color-surface-2)]" : "hover:bg-[var(--color-surface-2)]"
        }`}
      >
        <span className="font-mono">{annotation.id.slice(0, 8)}</span>
        <span className="text-[var(--color-muted)]">
          {annotation.kind} {status}
        </span>
      </button>
    </li>
  );
}
