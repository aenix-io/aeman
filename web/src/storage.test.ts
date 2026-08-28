import { describe, expect, it } from "vitest";

import {
  LS_LEGACY_OWNER,
  LS_LEGACY_PROJECT,
  LS_SINGLE_BOARD_MARK,
  migrateBoardScopedKeys,
  type StorageLike,
} from "./storage";

const ROSTER = "aeman.teamRoster";
const FILTER = "aeman.teamFilter";
const BASES = [ROSTER, FILTER];

function fakeStorage(seed: Record<string, string> = {}): StorageLike & {
  dump: () => Record<string, string>;
} {
  const map = new Map(Object.entries(seed));
  return {
    get length() {
      return map.size;
    },
    key: (i: number) => [...map.keys()][i] ?? null,
    getItem: (k: string) => (map.has(k) ? map.get(k)! : null),
    setItem: (k: string, v: string) => void map.set(k, v),
    removeItem: (k: string) => void map.delete(k),
    dump: () => Object.fromEntries(map),
  };
}

describe("migrateBoardScopedKeys", () => {
  it("copies the last board's roster and filter (named by the legacy owner/project keys) onto the single keys", () => {
    const s = fakeStorage({
      [LS_LEGACY_OWNER]: "acme",
      [LS_LEGACY_PROJECT]: "37",
      [`${ROSTER}.acme/37`]: '["alpha","beta"]',
      [`${FILTER}.acme/37`]: '["alpha"]',
      [`${ROSTER}.acme/36`]: '["old"]',
    });
    expect(migrateBoardScopedKeys(s, BASES)).toBe("acme/37");
    expect(s.getItem(ROSTER)).toBe('["alpha","beta"]');
    expect(s.getItem(FILTER)).toBe('["alpha"]');
    expect(s.getItem(LS_SINGLE_BOARD_MARK)).toBe("1");
    // The pointer to a board is meaningless now and goes away with it.
    expect(s.getItem(LS_LEGACY_OWNER)).toBeNull();
    expect(s.getItem(LS_LEGACY_PROJECT)).toBeNull();
  });

  it("runs once: a second call is a no-op even when the scoped values changed", () => {
    const s = fakeStorage({
      [LS_LEGACY_OWNER]: "acme",
      [LS_LEGACY_PROJECT]: "37",
      [`${ROSTER}.acme/37`]: '["alpha"]',
    });
    migrateBoardScopedKeys(s, BASES);
    s.setItem(`${ROSTER}.acme/37`, '["changed"]');
    s.setItem(LS_LEGACY_OWNER, "acme");
    s.setItem(LS_LEGACY_PROJECT, "37");
    expect(migrateBoardScopedKeys(s, BASES)).toBeNull();
    expect(s.getItem(ROSTER)).toBe('["alpha"]');
  });

  it("replaces a pre-scoping leftover under the plain key with the board's saved state", () => {
    // Before boards were scoped the plain key was written; the scoped era
    // ignored it, so on first load it holds nothing the user last saw.
    const s = fakeStorage({
      [LS_LEGACY_OWNER]: "acme",
      [LS_LEGACY_PROJECT]: "37",
      [ROSTER]: '["stale"]',
      [`${ROSTER}.acme/37`]: '["alpha"]',
    });
    migrateBoardScopedKeys(s, BASES);
    expect(s.getItem(ROSTER)).toBe('["alpha"]');
  });

  it("leaves a plain key alone when the board never saved that value", () => {
    const s = fakeStorage({
      [LS_LEGACY_OWNER]: "acme",
      [LS_LEGACY_PROJECT]: "37",
      [`${ROSTER}.acme/37`]: '["alpha"]',
    });
    migrateBoardScopedKeys(s, BASES);
    expect(s.getItem(FILTER)).toBeNull();
  });

  it("without the legacy pointer (a pinned deployment), migrates from the only board with saved state", () => {
    const s = fakeStorage({
      [`${ROSTER}.acme/37`]: '["alpha"]',
      [`${FILTER}.acme/37`]: '["alpha"]',
      // Another scoped-looking key of an unrelated base is not a board.
      ["aeman.notesCollapsed.narrow"]: "1",
    });
    expect(migrateBoardScopedKeys(s, BASES)).toBe("acme/37");
    expect(s.getItem(ROSTER)).toBe('["alpha"]');
    expect(s.getItem(FILTER)).toBe('["alpha"]');
  });

  it("without the legacy pointer, several boards are ambiguous and nothing is copied", () => {
    const s = fakeStorage({
      [`${ROSTER}.acme/37`]: '["alpha"]',
      [`${FILTER}.acme/36`]: '["beta"]',
    });
    expect(migrateBoardScopedKeys(s, BASES)).toBeNull();
    expect(s.getItem(ROSTER)).toBeNull();
    expect(s.getItem(FILTER)).toBeNull();
    expect(s.getItem(LS_SINGLE_BOARD_MARK)).toBe("1");
  });

  it("marks an empty storage done and writes nothing else", () => {
    const s = fakeStorage();
    expect(migrateBoardScopedKeys(s, BASES)).toBeNull();
    expect(s.dump()).toEqual({ [LS_SINGLE_BOARD_MARK]: "1" });
  });

  it("swallows a storage that refuses writes", () => {
    const throwing: StorageLike = {
      length: 0,
      key: () => null,
      getItem: () => null,
      setItem: () => {
        throw new Error("quota");
      },
      removeItem: () => undefined,
    };
    expect(() => migrateBoardScopedKeys(throwing, BASES)).not.toThrow();
  });
});
