// Tests for the keyboard shortcut mapping (Phase 4 spec §10).
// We test the pure shortcutFor() function directly so we don't have to
// boot a JSDOM Konva canvas — the integration with the store + router
// is exercised by Slice B4's Playwright E2E.
import { describe, expect, it } from "vitest";

import { isEditable, shortcutFor } from "./use-studio-shortcuts";

function ev(
  key: string,
  opts: { shift?: boolean; meta?: boolean; ctrl?: boolean; alt?: boolean; target?: HTMLElement | null } = {},
): KeyboardEvent {
  const e = new KeyboardEvent("keydown", {
    key,
    shiftKey: opts.shift ?? false,
    metaKey: opts.meta ?? false,
    ctrlKey: opts.ctrl ?? false,
    altKey: opts.alt ?? false,
    bubbles: true,
  });
  if (opts.target !== undefined) {
    Object.defineProperty(e, "target", { value: opts.target, writable: false });
  }
  return e;
}

describe("shortcutFor — full 8-shortcut bundle (spec §10)", () => {
  const cases: Array<[string, ReturnType<typeof shortcutFor>]> = [
    ["a", "accept"],
    ["A", "accept"],
    ["r", "reject"],
    ["R", "reject"],
    ["l", "label"],
    ["L", "label"],
    ["d", "delete"],
    ["D", "delete"],
    ["z", "undo"],
    ["Z", "undo"],
    ["Escape", "deselect"],
    ["ArrowLeft", "prev"],
    ["ArrowRight", "next"],
  ];

  it.each(cases)("maps %s → %s", (key, expected) => {
    expect(shortcutFor(ev(key))).toBe(expected);
  });

  it("Shift+Z maps to redo (not undo)", () => {
    expect(shortcutFor(ev("z", { shift: true }))).toBe("redo");
    expect(shortcutFor(ev("Z", { shift: true }))).toBe("redo");
  });

  it("returns null for unrelated keys", () => {
    expect(shortcutFor(ev("x"))).toBeNull();
    expect(shortcutFor(ev(" "))).toBeNull();
    expect(shortcutFor(ev("Enter"))).toBeNull();
  });
});

describe("shortcutFor — modifier guards", () => {
  it("ignores Cmd+letter (browser shortcut conflicts)", () => {
    expect(shortcutFor(ev("a", { meta: true }))).toBeNull();
    expect(shortcutFor(ev("d", { meta: true }))).toBeNull();
  });

  it("ignores Ctrl+letter", () => {
    expect(shortcutFor(ev("r", { ctrl: true }))).toBeNull();
  });

  it("ignores Alt+letter", () => {
    expect(shortcutFor(ev("l", { alt: true }))).toBeNull();
  });

  it("Shift+Z still maps even though Shift is a modifier", () => {
    expect(shortcutFor(ev("z", { shift: true }))).toBe("redo");
  });
});

describe("shortcutFor — input focus guard", () => {
  it("returns null when target is an INPUT", () => {
    const input = document.createElement("input");
    expect(shortcutFor(ev("d", { target: input }))).toBeNull();
  });

  it("returns null when target is a TEXTAREA", () => {
    const ta = document.createElement("textarea");
    expect(shortcutFor(ev("z", { target: ta }))).toBeNull();
  });

  it("returns null when target is a SELECT", () => {
    const sel = document.createElement("select");
    expect(shortcutFor(ev("a", { target: sel }))).toBeNull();
  });

  it("returns null when target is contentEditable", () => {
    const div = document.createElement("div");
    div.setAttribute("contenteditable", "true");
    expect(shortcutFor(ev("r", { target: div }))).toBeNull();
  });

  it("non-editable targets do NOT block shortcuts", () => {
    const button = document.createElement("button");
    expect(shortcutFor(ev("d", { target: button }))).toBe("delete");
  });
});

describe("isEditable", () => {
  it("recognizes form controls", () => {
    expect(isEditable(document.createElement("input"))).toBe(true);
    expect(isEditable(document.createElement("textarea"))).toBe(true);
    expect(isEditable(document.createElement("select"))).toBe(true);
  });

  it("recognizes contentEditable elements", () => {
    const div = document.createElement("div");
    div.setAttribute("contenteditable", "true");
    expect(isEditable(div)).toBe(true);
  });

  it("plain elements are not editable", () => {
    expect(isEditable(document.createElement("div"))).toBe(false);
    expect(isEditable(document.createElement("button"))).toBe(false);
  });

  it("null target is not editable", () => {
    expect(isEditable(null)).toBe(false);
  });
});
