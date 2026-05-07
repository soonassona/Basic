"use client";

// Studio right rail — annotation list + label picker placeholder.
// Stage B wires the label dropdown (L shortcut), accept/reject buttons (A/R),
// and inline geometry edits.
import type { Annotation, AnnotationSet } from "@/lib/api";
import { useStudio } from "@/lib/studio-store";

export function StudioSidebar({ set }: { set: AnnotationSet }) {
  const selectedId = useStudio((s) => s.selectedAnnotationId);
  const select = useStudio((s) => s.selectAnnotation);

  return (
    <aside
      aria-label="Annotation sidebar"
      className="flex h-full flex-col border-l border-[var(--color-border-2)]"
      data-testid="studio-sidebar"
    >
      <header className="border-b border-[var(--color-border-2)] p-3">
        <div className="text-xs uppercase text-[var(--color-muted)]">Set</div>
        <div className="mt-1 font-mono text-xs">{set.id.slice(0, 8)}</div>
        <div className="mt-2 text-xs text-[var(--color-muted)]">
          version <span className="font-mono">{set.version}</span> · {set.annotations.length}
          {set.annotations.length === 1 ? " annotation" : " annotations"}
        </div>
      </header>

      <ul className="flex-1 overflow-y-auto" data-testid="annotation-list">
        {set.annotations.length === 0 ? (
          <li className="p-4 text-sm text-[var(--color-muted)]">
            No annotations yet — draw a box to start.
          </li>
        ) : (
          set.annotations.map((a) => (
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
        <div className="text-xs uppercase text-[var(--color-muted)]">Label</div>
        <button
          type="button"
          disabled
          aria-label="Label picker (Stage B)"
          className="mt-2 w-full rounded-sm border border-[var(--color-border-2)] px-2 py-1.5 text-left text-sm text-[var(--color-muted)]"
        >
          {/* TODO Stage B: wire label dropdown + L shortcut */}
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
