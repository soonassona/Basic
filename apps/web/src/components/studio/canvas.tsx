"use client";

// Konva-backed annotation canvas (spec §10).
// Stage A: image background + non-interactive bbox overlays + selection sync.
// Stage B will add tool drawing handlers, polygon vertices, mask compositing,
// and the command-pattern history that drives Z / Shift+Z.
//
// Konva touches `window` on import — this module is loaded via `next/dynamic`
// with `ssr:false` from the studio page wrapper.
import { useEffect, useMemo, useRef, useState } from "react";
import { Stage, Layer, Rect, Image as KonvaImage } from "react-konva";

import type { Annotation, AnnotationSet, ImageRecord } from "@/lib/api";
import { useStudio } from "@/lib/studio-store";

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

export function StudioCanvas({
  image,
  imageUrl,
  set,
}: {
  image: ImageRecord;
  imageUrl: string;
  set: AnnotationSet;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });
  const [htmlImage, setHtmlImage] = useState<HTMLImageElement | null>(null);
  const selectedId = useStudio((s) => s.selectedAnnotationId);
  const selectAnnotation = useStudio((s) => s.selectAnnotation);

  // Track container size so the Stage fills the viewport and resizes with it.
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

  // Decode the image once so Konva can render it on a Layer.
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

  return (
    <div
      ref={containerRef}
      className="relative h-full w-full bg-[var(--color-bg)]"
      data-testid="studio-canvas"
      onClick={(e) => {
        // Click outside the bboxes deselects.
        if (e.target === containerRef.current) selectAnnotation(null);
      }}
    >
      {size.width > 0 && size.height > 0 && (
        <Stage width={size.width} height={size.height} scaleX={scale} scaleY={scale}>
          <Layer listening={false}>
            {htmlImage && <KonvaImage image={htmlImage} />}
          </Layer>
          <Layer>
            {set.annotations.map((a) => (
              <AnnotationShape
                key={a.id}
                annotation={a}
                selected={a.id === selectedId}
                onSelect={() => selectAnnotation(a.id)}
              />
            ))}
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
  onSelect,
}: {
  annotation: Annotation;
  selected: boolean;
  onSelect: () => void;
}) {
  if (annotation.kind === "bbox" && isBbox(annotation.geometry)) {
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
      />
    );
  }
  // Stage B: polygon, point, mask shapes.
  return null;
}
