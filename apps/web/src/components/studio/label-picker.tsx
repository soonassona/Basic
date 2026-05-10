"use client";

// Label picker (Phase 4 spec §10).
//
// Native <select> for keyboard accessibility + zero dependencies. The L
// shortcut focuses this element via the forwarded ref. When a label is
// chosen we push an UpdateCommand against the selected annotation; the
// autosave loop converts that into a PATCH with If-Match.
import { useQuery } from "@tanstack/react-query";
import { forwardRef } from "react";

import { api } from "@/lib/api";
import { useStudio } from "@/lib/studio-store";

export const LabelPicker = forwardRef<HTMLSelectElement>(function LabelPicker(_props, ref) {
  const labels = useQuery({
    queryKey: ["labels"],
    queryFn: () => api.listLabels(),
    staleTime: 5 * 60 * 1000, // labels rarely change inside a session
  });

  const selectedId = useStudio((s) => s.selectedAnnotationId);
  const buffer = useStudio((s) => s.buffer);
  const applyCommand = useStudio((s) => s.applyCommand);
  const selected = selectedId ? buffer[selectedId] : undefined;

  const disabled = !selected || labels.isLoading || !!labels.error;
  const value = selected?.label_id ?? "";
  const items = labels.data?.items ?? [];

  return (
    <div className="space-y-1">
      <select
        ref={ref}
        value={value}
        disabled={disabled}
        data-testid="label-picker-select"
        aria-label="Annotation label"
        onChange={(e) => {
          if (!selected) return;
          const next = e.target.value || null;
          if (next === selected.label_id) return;
          applyCommand({
            type: "update",
            id: selected.id,
            before: selected,
            after: { ...selected, label_id: next },
          });
        }}
        className="w-full rounded-sm border border-[var(--color-border-2)] bg-transparent px-2 py-1.5 text-sm focus:border-[var(--color-primary)] focus:outline-none disabled:opacity-50"
      >
        <option value="">(no label)</option>
        {items.map((l) => (
          <option key={l.id} value={l.id}>
            {l.name}
          </option>
        ))}
      </select>
      {selected && value && (
        <ColorSwatch
          color={items.find((l) => l.id === value)?.color}
          name={items.find((l) => l.id === value)?.name}
        />
      )}
      {!selected && (
        <p className="text-[10px] text-[var(--color-muted)]">Select an annotation to label it.</p>
      )}
      {labels.isError && (
        <p className="text-[10px] text-[var(--color-danger)]">Failed to load labels.</p>
      )}
    </div>
  );
});

function ColorSwatch({ color, name }: { color: string | undefined; name: string | undefined }) {
  if (!color) return null;
  return (
    <div className="flex items-center gap-2 text-[10px] text-[var(--color-muted)]">
      <span
        className="inline-block size-3 rounded-sm border border-black/30"
        style={{ background: color }}
        aria-hidden="true"
      />
      <span className="font-mono">{name ?? color}</span>
    </div>
  );
}
