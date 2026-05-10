"use client";

// Konva-backed annotation canvas (spec §10).
//
// Tools wired:
//   - select: click to select; drag selected bbox → UpdateCommand
//   - bbox:    drag on stage → CreateCommand with {x,y,w,h}
//   - point:   click on stage → CreateCommand with {x,y}
//   - polygon: click to add vertex; click first vertex / press Enter to close;
//              Esc cancels; double-click closes mid-stream
// "auto" is server-side (job submission, not a draw tool); the picker shows
// it for affordance but stage clicks in that mode are no-ops.
//
// Konva touches `window` on import — this module is loaded via `next/dynamic`
// with `ssr:false` from studio-shell.tsx.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Stage, Layer, Rect, Circle, Line, Image as KonvaImage } from "react-konva";
import type Konva from "konva";

import type { Annotation, AnnotationKind, ImageRecord } from "@/lib/api";
import { studioSelectors, useStudio } from "@/lib/studio-store";

// ── geometry shape guards ────────────────────────────────────────────────────

type Bbox = { x: number; y: number; w: number; h: number };
type Point = { x: number; y: number };
type Polygon = { points: Array<[number, number]> };

function isBbox(g: unknown): g is Bbox {
  if (!g || typeof g !== "object") return false;
  const o = g as Record<string, unknown>;
  return (
    typeof o.x === "number" &&
    typeof o.y === "number" &&
    typeof o.w === "number" &&
    typeof o.h === "number"
  );
}

function isPoint(g: unknown): g is Point {
  if (!g || typeof g !== "object") return false;
  const o = g as Record<string, unknown>;
  return typeof o.x === "number" && typeof o.y === "number" && !("w" in o);
}

function isPolygon(g: unknown): g is Polygon {
  if (!g || typeof g !== "object") return false;
  const o = g as Record<string, unknown>;
  return Array.isArray(o.points) && o.points.every((p) => Array.isArray(p) && p.length === 2);
}

function newAnnotation(setId: string, kind: AnnotationKind, geometry: unknown): Annotation {
  // Local UUID until the server assigns one on first POST (autosave handles
  // the swap via studioStore.replaceAnnotationId).
  return {
    id: crypto.randomUUID(),
    annotation_set_id: setId,
    label_id: null,
    kind,
    geometry,
    human_accepted: null,
  };
}

// Polygon vertices flatten to [x1,y1,x2,y2,...] for Konva's Line.points prop.
function flat(points: Array<[number, number]>): number[] {
  const out: number[] = [];
  for (const [x, y] of points) out.push(x, y);
  return out;
}

// ── component ────────────────────────────────────────────────────────────────

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

  // In-progress draw state. Bbox uses a single rect; polygon uses a vertex
  // list; point commits immediately on click. Local state — only commits to
  // the store on completion so undo/redo never sees partial drafts.
  const [draftBbox, setDraftBbox] = useState<Bbox | null>(null);
  const draftBboxStart = useRef<Point | null>(null);
  const [draftPoly, setDraftPoly] = useState<Array<[number, number]>>([]);
  const [hoverPoint, setHoverPoint] = useState<Point | null>(null);

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

  // Switching tools mid-draw cancels any in-progress polygon.
  useEffect(() => {
    setDraftPoly([]);
    setHoverPoint(null);
    setDraftBbox(null);
    draftBboxStart.current = null;
  }, [tool]);

  const scale = useMemo(() => {
    if (!image.width || !image.height || size.width === 0 || size.height === 0) return 1;
    return Math.min(size.width / image.width, size.height / image.height);
  }, [image.width, image.height, size]);

  const pointerOnImage = useCallback((stage: Konva.Stage | null) => {
    if (!stage) return null;
    const p = stage.getPointerPosition();
    if (!p) return null;
    return { x: p.x / scale, y: p.y / scale };
  }, [scale]);

  // ── draw handlers (per tool) ───────────────────────────────────────────────

  const commitPolygon = useCallback(() => {
    if (draftPoly.length < 3) return; // a polygon needs at least 3 vertices
    const ann = newAnnotation(annotationSetId, "polygon", { points: draftPoly });
    applyCommand({ type: "create", annotation: ann });
    selectAnnotation(ann.id);
    setDraftPoly([]);
    setHoverPoint(null);
  }, [draftPoly, annotationSetId, applyCommand, selectAnnotation]);

  const handleMouseDown = useCallback(
    (e: Konva.KonvaEventObject<MouseEvent>) => {
      if (tool !== "bbox") return;
      if (e.target !== e.target.getStage()) return;
      const p = pointerOnImage(e.target.getStage());
      if (!p) return;
      draftBboxStart.current = p;
      setDraftBbox({ x: p.x, y: p.y, w: 0, h: 0 });
    },
    [tool, pointerOnImage],
  );

  const handleMouseMove = useCallback(
    (e: Konva.KonvaEventObject<MouseEvent>) => {
      const p = pointerOnImage(e.target.getStage());
      if (!p) return;
      if (tool === "bbox" && draftBboxStart.current) {
        const start = draftBboxStart.current;
        setDraftBbox({
          x: Math.min(start.x, p.x),
          y: Math.min(start.y, p.y),
          w: Math.abs(p.x - start.x),
          h: Math.abs(p.y - start.y),
        });
      } else if (tool === "polygon" && draftPoly.length > 0) {
        setHoverPoint(p);
      }
    },
    [tool, pointerOnImage, draftPoly.length],
  );

  const handleMouseUp = useCallback(() => {
    if (tool !== "bbox") return;
    const start = draftBboxStart.current;
    const d = draftBbox;
    draftBboxStart.current = null;
    setDraftBbox(null);
    if (!start || !d) return;
    if (d.w < 4 || d.h < 4) return; // ignore tiny clicks
    const ann = newAnnotation(annotationSetId, "bbox", d);
    applyCommand({ type: "create", annotation: ann });
    selectAnnotation(ann.id);
  }, [tool, draftBbox, annotationSetId, applyCommand, selectAnnotation]);

  const handleStageClick = useCallback(
    (e: Konva.KonvaEventObject<MouseEvent>) => {
      const stage = e.target.getStage();
      const onEmpty = e.target === stage;
      const p = pointerOnImage(stage);

      if (tool === "select") {
        if (onEmpty) selectAnnotation(null);
        return;
      }
      if (tool === "point" && onEmpty && p) {
        const ann = newAnnotation(annotationSetId, "point", { x: p.x, y: p.y });
        applyCommand({ type: "create", annotation: ann });
        selectAnnotation(ann.id);
        return;
      }
      if (tool === "polygon" && onEmpty && p) {
        // Click on/near the first vertex (within 8 image-px) closes the polygon.
        if (draftPoly.length >= 3) {
          const [fx, fy] = draftPoly[0];
          if (Math.hypot(p.x - fx, p.y - fy) < 8) {
            commitPolygon();
            return;
          }
        }
        setDraftPoly((prev) => [...prev, [p.x, p.y]]);
      }
    },
    [tool, pointerOnImage, selectAnnotation, annotationSetId, applyCommand, draftPoly, commitPolygon],
  );

  const handleStageDblClick = useCallback(() => {
    if (tool === "polygon") commitPolygon();
  }, [tool, commitPolygon]);

  // Esc cancels an in-progress polygon (Esc in the global shortcut handler
  // also deselects, but cancellation lives here so it doesn't leak into the
  // shortcut hook's scope).
  useEffect(() => {
    if (tool !== "polygon" || draftPoly.length === 0) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setDraftPoly([]);
        setHoverPoint(null);
      } else if (e.key === "Enter") {
        commitPolygon();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [tool, draftPoly.length, commitPolygon]);

  const cursor =
    tool === "bbox" || tool === "point" || tool === "polygon" ? "crosshair" : "default";

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
          onDblClick={handleStageDblClick}
          onDblTap={handleStageDblClick}
          style={{ cursor }}
        >
          <Layer listening={false}>{htmlImage && <KonvaImage image={htmlImage} />}</Layer>
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
            {/* In-progress drafts (preview only, never reach the buffer). */}
            {draftBbox && (
              <Rect
                x={draftBbox.x}
                y={draftBbox.y}
                width={draftBbox.w}
                height={draftBbox.h}
                stroke="#4a8ff5"
                strokeWidth={2}
                dash={[4, 4]}
                listening={false}
              />
            )}
            {draftPoly.length > 0 && (
              <PolygonDraft vertices={draftPoly} hover={hoverPoint} />
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

// ── shapes ───────────────────────────────────────────────────────────────────

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
  const stroke = selected ? "#4a8ff5" : "#3ecf8e";
  const strokeW = selected ? 3 : 2;

  if (annotation.kind === "bbox" && isBbox(annotation.geometry)) {
    const g = annotation.geometry;
    return (
      <Rect
        x={g.x}
        y={g.y}
        width={g.w}
        height={g.h}
        stroke={stroke}
        strokeWidth={strokeW}
        dash={selected ? undefined : [6, 4]}
        onClick={onSelect}
        onTap={onSelect}
        draggable={draggable}
        onDragEnd={(e) => {
          const node = e.target;
          onMove({
            ...annotation,
            geometry: { x: node.x(), y: node.y(), w: g.w, h: g.h },
          });
        }}
      />
    );
  }
  if (annotation.kind === "point" && isPoint(annotation.geometry)) {
    const g = annotation.geometry;
    return (
      <Circle
        x={g.x}
        y={g.y}
        radius={selected ? 7 : 5}
        stroke={stroke}
        strokeWidth={strokeW}
        fill={selected ? "rgba(74,143,245,0.35)" : "rgba(62,207,142,0.25)"}
        onClick={onSelect}
        onTap={onSelect}
        draggable={draggable}
        onDragEnd={(e) => {
          const node = e.target;
          onMove({ ...annotation, geometry: { x: node.x(), y: node.y() } });
        }}
      />
    );
  }
  if (annotation.kind === "polygon" && isPolygon(annotation.geometry)) {
    const g = annotation.geometry;
    return (
      <Line
        points={flat(g.points)}
        stroke={stroke}
        strokeWidth={strokeW}
        closed
        fill={selected ? "rgba(74,143,245,0.15)" : "rgba(62,207,142,0.10)"}
        onClick={onSelect}
        onTap={onSelect}
        // Polygon move via drag — we shift every vertex by the drag delta.
        draggable={draggable}
        onDragEnd={(e) => {
          const dx = e.target.x();
          const dy = e.target.y();
          const moved = g.points.map(([x, y]) => [x + dx, y + dy] as [number, number]);
          // Reset the node's offset so it doesn't accumulate next drag.
          e.target.x(0);
          e.target.y(0);
          onMove({ ...annotation, geometry: { points: moved } });
        }}
      />
    );
  }
  return null;
}

function PolygonDraft({
  vertices,
  hover,
}: {
  vertices: Array<[number, number]>;
  hover: Point | null;
}) {
  const points = hover ? [...vertices, [hover.x, hover.y] as [number, number]] : vertices;
  return (
    <>
      <Line
        points={flat(points)}
        stroke="#4a8ff5"
        strokeWidth={2}
        dash={[4, 4]}
        listening={false}
      />
      {/* Visible vertex markers — first vertex is larger so the user can
          aim for it to close the polygon. */}
      {vertices.map(([x, y], i) => (
        <Circle
          key={i}
          x={x}
          y={y}
          radius={i === 0 ? 6 : 4}
          fill={i === 0 ? "#4a8ff5" : "#ffffff"}
          stroke="#4a8ff5"
          strokeWidth={1.5}
          listening={false}
        />
      ))}
    </>
  );
}
