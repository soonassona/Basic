// Tests for the studio store command stack (Phase 4 spec §10).
// Exercises: seedBuffer / applyCommand / undo / redo / depth-50 cap /
// future cleared on new command.
import { beforeEach, describe, expect, it } from "vitest";

import type { Annotation } from "./api";
import {
  HISTORY_DEPTH,
  studioSelectors,
  useStudio,
  type Command,
} from "./studio-store";

function ann(id: string, x = 0, y = 0, w = 10, h = 10): Annotation {
  return {
    id,
    annotation_set_id: "set-1",
    label_id: null,
    kind: "bbox",
    geometry: { x, y, w, h },
    human_accepted: null,
  };
}

function createCmd(a: Annotation): Command {
  return { type: "create", annotation: a };
}

function updateCmd(id: string, before: Annotation, after: Annotation): Command {
  return { type: "update", id, before, after };
}

function deleteCmd(a: Annotation): Command {
  return { type: "delete", annotation: a };
}

beforeEach(() => {
  useStudio.getState().reset();
});

describe("studio-store: seedBuffer", () => {
  it("populates buffer keyed by id and clears history", () => {
    useStudio.getState().applyCommand(createCmd(ann("a"))); // dirty history
    useStudio.getState().seedBuffer([ann("x"), ann("y")]);

    const s = useStudio.getState();
    expect(Object.keys(s.buffer).sort()).toEqual(["x", "y"]);
    expect(s.history).toHaveLength(0);
    expect(s.future).toHaveLength(0);
  });
});

describe("studio-store: applyCommand", () => {
  it("create adds to buffer and pushes onto history", () => {
    useStudio.getState().applyCommand(createCmd(ann("a", 1, 2, 3, 4)));

    const s = useStudio.getState();
    expect(s.buffer.a).toMatchObject({ id: "a", geometry: { x: 1, y: 2, w: 3, h: 4 } });
    expect(s.history).toHaveLength(1);
  });

  it("update swaps the entry to `after`", () => {
    const before = ann("a", 0, 0);
    const after = ann("a", 5, 5);
    useStudio.getState().seedBuffer([before]);
    useStudio.getState().applyCommand(updateCmd("a", before, after));

    expect(useStudio.getState().buffer.a.geometry).toEqual({ x: 5, y: 5, w: 10, h: 10 });
  });

  it("delete removes the entry from the buffer", () => {
    const a = ann("a");
    useStudio.getState().seedBuffer([a]);
    useStudio.getState().applyCommand(deleteCmd(a));

    expect(useStudio.getState().buffer.a).toBeUndefined();
  });

  it("clears future on new command (no orphan redo)", () => {
    useStudio.getState().applyCommand(createCmd(ann("a")));
    useStudio.getState().undo(); // future has 1 entry now
    expect(useStudio.getState().future).toHaveLength(1);

    useStudio.getState().applyCommand(createCmd(ann("b")));
    expect(useStudio.getState().future).toHaveLength(0);
  });

  it("caps history at HISTORY_DEPTH (drops the oldest)", () => {
    for (let i = 0; i < HISTORY_DEPTH + 5; i++) {
      useStudio.getState().applyCommand(createCmd(ann(`a${i}`)));
    }
    const s = useStudio.getState();
    expect(s.history).toHaveLength(HISTORY_DEPTH);
    // Buffer still holds all created entries — only undoability is capped.
    expect(Object.keys(s.buffer)).toHaveLength(HISTORY_DEPTH + 5);
  });
});

describe("studio-store: undo / redo round-trip", () => {
  it("undo a create empties the buffer; redo restores it", () => {
    const a = ann("a", 1, 2, 3, 4);
    useStudio.getState().applyCommand(createCmd(a));

    useStudio.getState().undo();
    expect(useStudio.getState().buffer.a).toBeUndefined();
    expect(useStudio.getState().future).toHaveLength(1);

    useStudio.getState().redo();
    expect(useStudio.getState().buffer.a).toMatchObject({ geometry: { x: 1, y: 2, w: 3, h: 4 } });
    expect(useStudio.getState().future).toHaveLength(0);
  });

  it("undo an update restores `before`; redo re-applies `after`", () => {
    const before = ann("a", 0, 0);
    const after = ann("a", 9, 9);
    useStudio.getState().seedBuffer([before]);
    useStudio.getState().applyCommand(updateCmd("a", before, after));

    useStudio.getState().undo();
    expect(useStudio.getState().buffer.a.geometry).toEqual({ x: 0, y: 0, w: 10, h: 10 });

    useStudio.getState().redo();
    expect(useStudio.getState().buffer.a.geometry).toEqual({ x: 9, y: 9, w: 10, h: 10 });
  });

  it("undo a delete restores the original entry", () => {
    const a = ann("a", 7, 7);
    useStudio.getState().seedBuffer([a]);
    useStudio.getState().applyCommand(deleteCmd(a));

    useStudio.getState().undo();
    expect(useStudio.getState().buffer.a).toMatchObject({ geometry: { x: 7, y: 7, w: 10, h: 10 } });
  });

  it("undo on empty history is a no-op", () => {
    useStudio.getState().undo();
    expect(useStudio.getState().history).toHaveLength(0);
    expect(useStudio.getState().future).toHaveLength(0);
  });

  it("redo on empty future is a no-op", () => {
    useStudio.getState().applyCommand(createCmd(ann("a")));
    useStudio.getState().redo();
    expect(useStudio.getState().history).toHaveLength(1);
  });
});

describe("studio-store: selectors", () => {
  it("canUndo / canRedo reflect stack depths", () => {
    expect(studioSelectors.canUndo(useStudio.getState())).toBe(false);
    expect(studioSelectors.canRedo(useStudio.getState())).toBe(false);

    useStudio.getState().applyCommand(createCmd(ann("a")));
    expect(studioSelectors.canUndo(useStudio.getState())).toBe(true);

    useStudio.getState().undo();
    expect(studioSelectors.canUndo(useStudio.getState())).toBe(false);
    expect(studioSelectors.canRedo(useStudio.getState())).toBe(true);
  });

  it("bufferList returns annotations as an array", () => {
    useStudio.getState().seedBuffer([ann("a"), ann("b"), ann("c")]);
    const list = studioSelectors.bufferList(useStudio.getState());
    expect(list).toHaveLength(3);
    expect(list.map((a) => a.id).sort()).toEqual(["a", "b", "c"]);
  });

  it("dirtyIds is empty after seedBuffer, populated after update", () => {
    const a = ann("a", 0, 0);
    useStudio.getState().seedBuffer([a]);
    expect(studioSelectors.dirtyIds(useStudio.getState())).toEqual([]);

    const after = ann("a", 5, 5);
    useStudio.getState().applyCommand(updateCmd("a", a, after));
    expect(studioSelectors.dirtyIds(useStudio.getState())).toEqual(["a"]);
  });

  it("dirtyIds returns to empty after markSaved", () => {
    const a = ann("a", 0, 0);
    const after = ann("a", 5, 5);
    useStudio.getState().seedBuffer([a]);
    useStudio.getState().applyCommand(updateCmd("a", a, after));
    useStudio.getState().markSaved("a");
    expect(studioSelectors.dirtyIds(useStudio.getState())).toEqual([]);
  });

  it("createdIds tracks ids absent from the original snapshot", () => {
    useStudio.getState().seedBuffer([ann("server-1")]);
    useStudio.getState().applyCommand(createCmd(ann("local-1")));
    expect(studioSelectors.createdIds(useStudio.getState())).toEqual(["local-1"]);
    expect(studioSelectors.dirtyIds(useStudio.getState())).toEqual([]);
  });

  it("deletedIds tracks ids present in original but missing from buffer", () => {
    const a = ann("a");
    useStudio.getState().seedBuffer([a]);
    useStudio.getState().applyCommand(deleteCmd(a));
    expect(studioSelectors.deletedIds(useStudio.getState())).toEqual(["a"]);
  });
});

describe("studio-store: replaceAnnotationId / forgetOriginal", () => {
  it("replaceAnnotationId swaps local id for server id, seeds original", () => {
    const local = ann("local-1", 1, 2);
    useStudio.getState().applyCommand(createCmd(local));
    expect(studioSelectors.createdIds(useStudio.getState())).toEqual(["local-1"]);

    const server = { ...local, id: "server-1" };
    useStudio.getState().replaceAnnotationId("local-1", server);

    const s = useStudio.getState();
    expect(s.buffer["local-1"]).toBeUndefined();
    expect(s.buffer["server-1"]).toMatchObject({ geometry: { x: 1, y: 2, w: 10, h: 10 } });
    // After replacement the row is no longer "created" (it's saved to server).
    expect(studioSelectors.createdIds(s)).toEqual([]);
    expect(studioSelectors.dirtyIds(s)).toEqual([]);
  });

  it("replaceAnnotationId updates the selected id when it was the local one", () => {
    const local = ann("local-2");
    useStudio.getState().applyCommand(createCmd(local));
    useStudio.getState().selectAnnotation("local-2");
    useStudio.getState().replaceAnnotationId("local-2", { ...local, id: "server-2" });
    expect(useStudio.getState().selectedAnnotationId).toBe("server-2");
  });

  it("forgetOriginal drops the id from the snapshot so deletedIds stops listing it", () => {
    const a = ann("a");
    useStudio.getState().seedBuffer([a]);
    useStudio.getState().applyCommand(deleteCmd(a));
    expect(studioSelectors.deletedIds(useStudio.getState())).toEqual(["a"]);

    useStudio.getState().forgetOriginal("a");
    expect(studioSelectors.deletedIds(useStudio.getState())).toEqual([]);
  });
});
