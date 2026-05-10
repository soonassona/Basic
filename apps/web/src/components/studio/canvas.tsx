"use client";

// Konva-backed annotation canvas (spec §10).
// Slice B1 wires:
//   - read annotations from the studio buffer (not directly from the server),
//   - bbox tool: drag on the stage to draw → push a CreateCommand,
//   - select tool: click a bbox to select; drag a selected bbox → UpdateCommand.
// Slice B2 will add the full keyboard-shortcut bundle and the polygon /
// point tools; Slice B3 wires autosave from the dirty entries.
//
// Konva touches `window` on import — this module is loaded via `next/dynamic`
// with `ssr:false` from studio-shell.tsx.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Stage, Layer, Rect, Image as KonvaImage } from "react-konva";
import type Konva from "konva";

import type { Annotation, ImageRecord } from "@/lib/api";
import { studioSelectors, useStudio } from "@/lib/studio-store";

type Bbox = { x: number; y: number; w: number; h: number };

function isBbox(g: unknown): g is Bbox {
  return (
    typeof g === "object" &&
    g !== null &&
    typeof (g as Bbox).x === "number" &&
    typeof (g as Bbox).y === "number" &&
    typeof (g as Bbox).w === "number" &&
    typeof (g as Bbox).h === "number"
  );
}

function newBboxAnnotation(setId: string, geom: Bbox): Annotation {
  // Local UUID until the server assigns one on first PATCH (Slice B3).
  // crypto.randomUUID is available on all modern browsers + Node ≥ 19.
  return {
    id: crypto.randomUUID(),
    annotation_set_id: setId,
    label_id: null,
    kind: "bbox",
    geometry: geom,
    human_accepted: null,
  };
}

export function StudioCanvas({
  image,
  imageUrl,
  annotationSetId,
}: {
  image: ImageRecord;
  imageUrl: string;
  annotationSetId: string;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const stageRef = useRef<Konva.Stage>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });
  const [htmlImage, setHtmlImage] = useState<HTMLImageElement | null>(null);

  const tool = useStudio((s) => s.selectedTool);
  const selectedId = useStudio((s) => s.selectedAnnotationId);
  const selectAnnotation = useStudio((s) => s.selectAnnotation);
  const annotations = useStudio(studioSelectors.bufferList);
  const applyCommand = useStudio((s) => s.applyCommand);

  // In-progress bbox while the user drags. Local state — only commits to the
  // store on mouseup so undo/redo doesn't see partial drags.
  const [draft, setDraft] = useState<Bbox | null>(null);
  const draftStart = useRef<{ x: number; y: number } | null>(null);

  // Track container size so the Stage fills the viewport.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => {
      const r = entry.contentRect;
      setSize({ width: Math.max(0, Math.floor(r.width)), height: Math.max(0, Math.floor(r.height)) });
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  useEffect(() => {
    if (!imageUrl) return;
    const img = new window.Image();
    img.crossOrigin = "anonymous";
    img.src = imageUrl;
    img.onload = () => setHtmlImage(img);
    return () => {
      img.onload = null;
    };
  }, [imageUrl]);

  // Fit-to-viewport scale that preserves the image aspect ratio.
  const scale = useMemo(() => {
    if (!image.width || !image.height || size.width === 0 || size.height === 0) return 1;
    return Math.min(size.width / image.width, size.height / image.height);
  }, [image.width, image.height, size]);

  // Convert pointer event to image-space (unscaled) coordinates.
  const pointerOnImage = useCallback((stage: Konva.Stage | null) => {
    if (!stage) return null;
    const p = stage.getPointerPosition();
    if (!p) return null;
    return { x: p.x / scale, y: p.y / scale };
  }, [scale]);

  const handleMouseDown = useCallback(
    (e: Konva.KonvaEventObject<MouseEvent>) => {
      if (tool !== "bbox") return;
      // Only start drawing on empty stage area.
      if (e.target !== e.target.getStage()) return;
      const p = pointerOnImage(e.target.getStage());
      if (!p) return;
      draftStart.current = p;
      setDraft({ x: p.x, y: p.y, w: 0, h: 0 });
    },
    [tool, pointerOnImage],
  );

  const handleMouseMove = useCallback(
    (e: Konva.KonvaEventObject<MouseEvent>) => {
      if (!draftStart.current) return;
      const p = pointerOnImage(e.target.getStage());
      if (!p) return;
      const x = Math.min(draftStart.current.x, p.x);
      const y = Math.min(draftStart.current.y, p.y);
      const w = Math.abs(p.x - draftStart.current.x);
      const h = Math.abs(p.y - draftStart.current.y);
      setDraft({ x, y, w, h });
    },
    [pointerOnImage],
  );

  const handleMouseUp = useCallback(() => {
    const start = draftStart.current;
    const d = draft;
    draftStart.current = null;
    setDraft(null);
    if (!start || !d) return;
    // Discard tiny clicks that aren't real boxes.
    if (d.w < 4 || d.h < 4) return;
    const ann = newBboxAnnotation(annotationSetId, d);
    applyCommand({ type: "create", annotation: ann });
    selectAnnotation(ann.id);
  }, [draft, annotationSetId, applyCommand, selectAnnotation]);

  const handleStageClick = useCallback(
    (e: Konva.KonvaEventObject<MouseEvent>) => {
      // Click on empty stage (not on a Rect) → deselect.
      if (tool === "select" && e.target === e.target.getStage()) {
        selectAnnotation(null);
      }
    },
    [tool, selectAnnotation],
  );

  return (
    <div
      ref={containerRef}
      className="relative h-full w-full bg-[var(--color-bg)]"
      data-testid="studio-canvas"
    >
      {size.width > 0 && size.height > 0 && (
        <Stage
          ref={stageRef}
          width={size.width}
          height={size.height}
          scaleX={scale}
          scaleY={scale}
          onMouseDown={handleMouseDown}
          onMouseMove={handleMouseMove}
          onMouseUp={handleMouseUp}
          onClick={handleStageClick}
          style={{ cursor: tool === "bbox" ? "crosshair" : "default" }}
        >
          <Layer listening={false}>
            {htmlImage && <KonvaImage image={htmlImage} />}
          </Layer>
          <Layer>
            {annotations.map((a) => (
              <AnnotationShape
                key={a.id}
                annotation={a}
                selected={a.id === selectedId}
                draggable={tool === "select" && a.id === selectedId}
                onSelect={() => selectAnnotation(a.id)}
                onMove={(after) =>
                  applyCommand({ type: "update", id: a.id, before: a, after })
                }
              />
            ))}
            {draft && (
              <Rect
                x={draft.x}
                y={draft.y}
                width={draft.w}
                height={draft.h}
                stroke="#4a8ff5"
                strokeWidth={2}
                dash={[4, 4]}
                listening={false}
              />
            )}
          </Layer>
        </Stage>
      )}
      {!htmlImage && (
        <div className="absolute inset-0 grid place-items-center text-sm text-[var(--color-muted)]">
          Loading image…
        </div>
      )}
    </div>
  );
}

function AnnotationShape({
  annotation,
  selected,
  draggable,
  onSelect,
  onMove,
}: {
  annotation: Annotation;
  selected: boolean;
  draggable: boolean;
  onSelect: () => void;
  onMove: (after: Annotation) => void;
}) {
  if (annotation.kind !== "bbox" || !isBbox(annotation.geometry)) return null;
  const g = annotation.geometry;
  return (
    <Rect
      x={g.x}
      y={g.y}
      width={g.w}
      height={g.h}
      stroke={selected ? "#4a8ff5" : "#3ecf8e"}
      strokeWidth={selected ? 3 : 2}
      dash={selected ? undefined : [6, 4]}
      onClick={onSelect}
      onTap={onSelect}
      draggable={draggable}
      onDragEnd={(e) => {
        const next = e.target;
        const after: Annotation = {
          ...annotation,
          geometry: { x: next.x(), y: next.y(), w: g.w, h: g.h },
        };
        onMove(after);
      }}
    />
  );
}
