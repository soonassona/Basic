// Tests for the autosave diff helper (Phase 4 spec §10).
// We test diffPatch as a pure function — the hook itself is exercised
// indirectly via the studio-store integration tests + Slice B4 Playwright E2E.
import { describe, expect, it } from "vitest";

import type { Annotation } from "./api";
import { diffPatch } from "./use-autosave";

function ann(over: Partial<Annotation> = {}): Annotation {
  return {
    id: "a",
    annotation_set_id: "set-1",
    label_id: null,
    kind: "bbox",
    geometry: { x: 0, y: 0, w: 10, h: 10 },
    human_accepted: null,
    ...over,
  };
}

describe("diffPatch", () => {
  it("returns null when nothing changed", () => {
    const a = ann();
    expect(diffPatch({ a }, { a }, "a")).toBeNull();
  });

  it("returns null when the id is missing from either side", () => {
    expect(diffPatch({}, { a: ann() }, "a")).toBeNull();
    expect(diffPatch({ a: ann() }, {}, "a")).toBeNull();
  });

  it("emits geometry only when the geometry actually moved", () => {
    const orig = ann();
    const moved = ann({ geometry: { x: 5, y: 5, w: 10, h: 10 } });
    const patch = diffPatch({ a: moved }, { a: orig }, "a");
    expect(patch).toEqual({ geometry: { x: 5, y: 5, w: 10, h: 10 } });
    expect(patch).not.toHaveProperty("label_id");
    expect(patch).not.toHaveProperty("human_accepted");
  });

  it("emits label_id when the label changed", () => {
    const orig = ann({ label_id: "lbl-old" });
    const next = ann({ label_id: "lbl-new" });
    expect(diffPatch({ a: next }, { a: orig }, "a")).toEqual({ label_id: "lbl-new" });
  });

  it("emits human_accepted when the accept flag flipped", () => {
    const orig = ann();
    const accepted = ann({ human_accepted: true });
    expect(diffPatch({ a: accepted }, { a: orig }, "a")).toEqual({ human_accepted: true });
  });

  it("combines multiple changed fields in one PATCH", () => {
    const orig = ann();
    const next = ann({
      label_id: "lbl-1",
      human_accepted: false,
      geometry: { x: 1, y: 2, w: 3, h: 4 },
    });
    expect(diffPatch({ a: next }, { a: orig }, "a")).toEqual({
      geometry: { x: 1, y: 2, w: 3, h: 4 },
      label_id: "lbl-1",
      human_accepted: false,
    });
  });
});
