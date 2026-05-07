"use client";

// Tool picker — left rail, vertical button stack (spec §10 Tools).
// Stage A renders all 5 tools but only "select" is functional; the
// drawing/editing handlers land in Stage B with the canvas tool wiring.
import { useStudio, type Tool } from "@/lib/studio-store";

const TOOLS: ReadonlyArray<{ id: Tool; label: string; key: string }> = [
  { id: "select", label: "Select", key: "V" },
  { id: "bbox", label: "Box", key: "B" },
  { id: "point", label: "Point", key: "P" },
  { id: "polygon", label: "Polygon", key: "G" },
  { id: "auto", label: "Auto-segment", key: "U" },
];

export function ToolPicker() {
  const tool = useStudio((s) => s.selectedTool);
  const setTool = useStudio((s) => s.setTool);

  return (
    <nav
      aria-label="Annotation tools"
      className="flex flex-col gap-1 border-r border-[var(--color-border-2)] p-2"
      data-testid="studio-tool-picker"
    >
      {TOOLS.map((t) => {
        const active = t.id === tool;
        return (
          <button
            key={t.id}
            type="button"
            onClick={() => setTool(t.id)}
            aria-pressed={active}
            data-testid={`tool-${t.id}`}
            className={`flex w-24 items-center justify-between rounded-sm px-2 py-1.5 text-xs ${
              active
                ? "bg-[var(--color-primary)] text-white"
                : "text-[var(--color-muted)] hover:bg-[var(--color-surface-2)]"
            }`}
          >
            <span>{t.label}</span>
            <kbd className="font-mono text-[10px] opacity-70">{t.key}</kbd>
          </button>
        );
      })}
    </nav>
  );
}
