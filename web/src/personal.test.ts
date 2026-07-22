import { describe, expect, it } from "vitest";
import {
  clearPersonalBoard,
  nextPane,
  personalPaneVisible,
  prevPane,
  readPersonalBoard,
  samePointer,
  savePersonalBoard,
} from "./personal";

function fakeStorage(): Storage {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
    clear: () => m.clear(),
    key: () => null,
    get length() {
      return m.size;
    },
  } as Storage;
}

describe("personal board pointer", () => {
  it("round-trips per login", () => {
    const s = fakeStorage();
    savePersonalBoard(s, "alice", { owner: "alice", number: 3 });
    expect(readPersonalBoard(s, "alice")).toEqual({ owner: "alice", number: 3 });
    // Another login sees nothing: the pointer is strictly per user.
    expect(readPersonalBoard(s, "bob")).toBeNull();
  });

  it("clear detaches", () => {
    const s = fakeStorage();
    savePersonalBoard(s, "alice", { owner: "alice", number: 3 });
    clearPersonalBoard(s, "alice");
    expect(readPersonalBoard(s, "alice")).toBeNull();
  });

  it("no login → no pointer, and writes are refused", () => {
    const s = fakeStorage();
    savePersonalBoard(s, "", { owner: "alice", number: 3 });
    expect(readPersonalBoard(s, "")).toBeNull();
  });

  it("rejects corrupt or non-positive stored values", () => {
    const s = fakeStorage();
    s.setItem("aeman.personal.alice", "not json");
    expect(readPersonalBoard(s, "alice")).toBeNull();
    s.setItem("aeman.personal.alice", JSON.stringify({ owner: "", number: 3 }));
    expect(readPersonalBoard(s, "alice")).toBeNull();
    s.setItem("aeman.personal.alice", JSON.stringify({ owner: "a", number: 0 }));
    expect(readPersonalBoard(s, "alice")).toBeNull();
    s.setItem(
      "aeman.personal.alice",
      JSON.stringify({ owner: "a", number: 1.5 }),
    );
    expect(readPersonalBoard(s, "alice")).toBeNull();
  });
});

describe("samePointer (work-board guard)", () => {
  it("matches case-insensitively on owner", () => {
    expect(
      samePointer({ owner: "Acme", number: 7 }, { owner: "acme", number: 7 }),
    ).toBe(true);
  });
  it("differs on number or owner", () => {
    expect(
      samePointer({ owner: "acme", number: 7 }, { owner: "acme", number: 8 }),
    ).toBe(false);
    expect(
      samePointer({ owner: "acme", number: 7 }, { owner: "alice", number: 7 }),
    ).toBe(false);
  });
});

describe("pane switcher", () => {
  it("arrows clamp at the edges (no wrap)", () => {
    expect(nextPane("work")).toBe("personal");
    expect(nextPane("personal")).toBe("personal");
    expect(prevPane("personal")).toBe("work");
    expect(prevPane("work")).toBe("work");
  });
});

describe("personalPaneVisible (the single gate)", () => {
  const ptr = { owner: "alice", number: 3 };
  it("visible only for the owner with a loaded board", () => {
    expect(
      personalPaneVisible({ lockBoard: false, viewAs: null, pointer: ptr, boardLoaded: true }),
    ).toBe(true);
  });
  it("never while impersonating", () => {
    expect(
      personalPaneVisible({ lockBoard: false, viewAs: "bob", pointer: ptr, boardLoaded: true }),
    ).toBe(false);
  });
  it("never in lock-board mode — the server would pin the request to the work project", () => {
    expect(
      personalPaneVisible({ lockBoard: true, viewAs: null, pointer: ptr, boardLoaded: true }),
    ).toBe(false);
  });
  it("not before the board loads, not without a pointer", () => {
    expect(
      personalPaneVisible({ lockBoard: false, viewAs: null, pointer: ptr, boardLoaded: false }),
    ).toBe(false);
    expect(
      personalPaneVisible({ lockBoard: false, viewAs: null, pointer: null, boardLoaded: true }),
    ).toBe(false);
  });
});
