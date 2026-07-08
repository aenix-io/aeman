import { describe, expect, it } from "vitest";
import {
  DEFAULT_APPEARANCE,
  LS_MODE,
  LS_PALETTE,
  PALETTE_ATTR,
  THEME_ATTR,
  applyAppearance,
  persistAppearance,
  readAppearance,
  resolveTheme,
} from "./theme";

function fakeStorage(seed: Record<string, string> = {}) {
  const map = new Map(Object.entries(seed));
  return {
    getItem: (k: string) => (map.has(k) ? map.get(k)! : null),
    setItem: (k: string, v: string) => void map.set(k, v),
    read: (k: string) => map.get(k) ?? null,
  };
}

function fakeRoot() {
  const attrs = new Map<string, string>();
  return {
    setAttribute: (n: string, v: string) => void attrs.set(n, v),
    get: (n: string) => attrs.get(n) ?? null,
  };
}

describe("readAppearance", () => {
  it("defaults to system + default when nothing is stored", () => {
    expect(readAppearance(fakeStorage())).toEqual(DEFAULT_APPEARANCE);
  });

  it("reads valid stored values on both axes", () => {
    const s = fakeStorage({ [LS_MODE]: "dark", [LS_PALETTE]: "vivid" });
    expect(readAppearance(s)).toEqual({ mode: "dark", palette: "vivid" });
  });

  it("falls back per-axis for unrecognised values", () => {
    expect(readAppearance(fakeStorage({ [LS_MODE]: "neon", [LS_PALETTE]: "marine" }))).toEqual({
      mode: "system",
      palette: "marine",
    });
    expect(readAppearance(fakeStorage({ [LS_MODE]: "light", [LS_PALETTE]: "garish" }))).toEqual({
      mode: "light",
      palette: "default",
    });
  });
});

describe("persistAppearance", () => {
  it("round-trips through readAppearance", () => {
    const s = fakeStorage();
    persistAppearance(s, { mode: "light", palette: "marine" });
    expect(s.read(LS_MODE)).toBe("light");
    expect(s.read(LS_PALETTE)).toBe("marine");
    expect(readAppearance(s)).toEqual({ mode: "light", palette: "marine" });
  });

  it("swallows storage failures instead of throwing", () => {
    const throwing = {
      getItem: () => null,
      setItem: () => {
        throw new Error("quota");
      },
    };
    expect(() => persistAppearance(throwing, DEFAULT_APPEARANCE)).not.toThrow();
  });
});

describe("resolveTheme", () => {
  it("passes explicit light/dark through unchanged", () => {
    expect(resolveTheme("light", true)).toBe("light");
    expect(resolveTheme("dark", false)).toBe("dark");
  });

  it("follows the OS preference only for system mode", () => {
    expect(resolveTheme("system", true)).toBe("dark");
    expect(resolveTheme("system", false)).toBe("light");
  });
});

describe("applyAppearance", () => {
  it("writes the resolved theme and palette attributes", () => {
    const root = fakeRoot();
    applyAppearance(root, { mode: "system", palette: "vivid" }, true);
    expect(root.get(THEME_ATTR)).toBe("dark");
    expect(root.get(PALETTE_ATTR)).toBe("vivid");

    applyAppearance(root, { mode: "light", palette: "default" }, true);
    expect(root.get(THEME_ATTR)).toBe("light");
    expect(root.get(PALETTE_ATTR)).toBe("default");
  });
});
