import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

// These tests read the real stylesheet, resolve the theme/palette variable
// cascade for each of the six (theme × palette) cells, and assert WCAG contrast.
// They exist because the appearance palettes make concrete colour claims that a
// code review can't verify by eye and that jsdom can't check (it doesn't resolve
// var()); encoding them here turns the design intent into an enforced invariant.

const css = readFileSync(fileURLToPath(new URL("./styles.css", import.meta.url)), "utf8");

/** Parse every `selector { ... }` block into a map of its --custom: value pairs. */
function blocks(): Map<string, Map<string, string>> {
  const out = new Map<string, Map<string, string>>();
  // Strip comments first: a comment before a rule would otherwise be captured as
  // part of the following selector and break the exact-key lookups below.
  const clean = css.replace(/\/\*[\s\S]*?\*\//g, "");
  const re = /([^{}]+)\{([^{}]*)\}/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(clean))) {
    const sel = m[1].trim().replace(/\s+/g, " ");
    const vars = new Map<string, string>();
    for (const decl of m[2].split(";")) {
      const mm = decl.match(/^\s*(--[a-z0-9-]+)\s*:\s*(.+?)\s*$/i);
      if (mm) {
        vars.set(mm[1], mm[2].trim());
      }
    }
    if (vars.size) {
      out.set(sel, vars);
    }
  }
  return out;
}

const B = blocks();

/** Merge the given selectors' variables in cascade order (later wins), then
 *  resolve one variable, following at most a couple of var() hops to a hex. */
function resolve(name: string, selectors: string[]): string {
  const merged = new Map<string, string>();
  for (const sel of selectors) {
    const vars = B.get(sel);
    if (vars) {
      for (const [k, v] of vars) {
        merged.set(k, v);
      }
    }
  }
  let value = merged.get(name);
  for (let i = 0; i < 4 && value && value.startsWith("var("); i++) {
    const ref = value.slice(4, value.indexOf(")")).split(",")[0].trim();
    value = merged.get(ref);
  }
  if (!value || !/^#[0-9a-f]{3,6}$/i.test(value)) {
    throw new Error(`could not resolve ${name} to a hex (got ${value})`);
  }
  return value;
}

function toRgb(hex: string): [number, number, number] {
  let h = hex.slice(1);
  if (h.length === 3) {
    h = h.split("").map((c) => c + c).join("");
  }
  const n = parseInt(h, 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

function luminance(hex: string): number {
  const lin = (c: number) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  const [r, g, b] = toRgb(hex);
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

function contrast(a: string, b: string): number {
  const la = luminance(a);
  const lb = luminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

const LIGHT = [":root"];
const DARK = [":root", ':root[data-theme="dark"]'];
const cell = {
  "light default": LIGHT,
  "light vivid": [":root", ':root[data-palette="vivid"]'],
  "light marine": [":root", ':root[data-palette="marine"]'],
  "dark default": DARK,
  "dark vivid": [":root", ':root[data-palette="vivid"]', ':root[data-theme="dark"]', ':root[data-theme="dark"][data-palette="vivid"]'],
  "dark marine": [":root", ':root[data-palette="marine"]', ':root[data-theme="dark"]', ':root[data-theme="dark"][data-palette="marine"]'],
};
const ZONE_BGS = ["--zone-red-bg", "--zone-yellow-bg", "--zone-gray-bg", "--zone-green-bg",
  // The Backlog board's internal lane is a zone area too, cards and all.
  "--lane-internal-bg"];

// AA is 4.5 for normal text, 3.0 for large/secondary. --fg is body text (held
// to AAA 7), --muted is secondary text (AA 4.5). --faint is the dim tier
// (placeholders, tiny uppercase captions): in light it is the app's long-
// standing ~3:1 caption grey (AA-large), and dark must not undershoot it — dark
// --faint is held to the stricter 4.5 since it was raised there to clear AA.
describe("base text contrast", () => {
  for (const [name, sels, faintMin] of [
    ["light", LIGHT, 3.0],
    ["dark", DARK, 4.5],
  ] as const) {
    it(`${name}: fg/muted/faint clear their contrast floor on the background`, () => {
      const bg = resolve("--bg", sels);
      expect(contrast(resolve("--fg", sels), bg)).toBeGreaterThanOrEqual(7);
      expect(contrast(resolve("--muted", sels), bg)).toBeGreaterThanOrEqual(4.5);
      expect(contrast(resolve("--faint", sels), bg)).toBeGreaterThanOrEqual(faintMin);
    });
  }
});

describe("white label on solid accent/danger fills", () => {
  for (const [name, sels] of [["light", LIGHT], ["dark", DARK]] as const) {
    it(`${name}: on-accent text clears AA on the emphasis fills`, () => {
      const white = resolve("--on-accent", sels);
      expect(contrast(white, resolve("--accent-emphasis", sels))).toBeGreaterThanOrEqual(4.5);
      expect(contrast(white, resolve("--danger-emphasis", sels))).toBeGreaterThanOrEqual(4.5);
    });
  }
});

// A finished bar must be tellable from an in-progress one in every cell: the
// palettes separate the pair by luminance within one hue family (the channel
// that survives every CVD axis), so hold them to a minimum contrast between
// each other AND keep the deeper done fill visible on the card background.
describe("done vs in-progress bar separation", () => {
  for (const [name, sels] of Object.entries(cell)) {
    it(`${name}: --stage-done and --bar-default stay distinct`, () => {
      const done = resolve("--stage-done", sels);
      const bar = resolve("--bar-default", sels);
      expect(done).not.toBe(bar);
      expect(contrast(done, bar)).toBeGreaterThanOrEqual(1.5);
      expect(contrast(done, resolve("--bg", sels))).toBeGreaterThanOrEqual(3.0);
    });
  }
});

// The spine label is the only text that sits on a zone fill; in the CVD palettes
// and dark mode it is painted --fg, and must stay readable on every zone.
describe("zone spine contrast where the spine is --fg", () => {
  for (const name of ["light vivid", "light marine", "dark default", "dark vivid", "dark marine"] as const) {
    it(`${name}: --fg clears AA on all four zone fills`, () => {
      const sels = cell[name];
      const fg = resolve("--fg", sels);
      for (const zoneBg of ZONE_BGS) {
        expect(contrast(fg, resolve(zoneBg, sels)), `${name} ${zoneBg}`).toBeGreaterThanOrEqual(4.5);
      }
    });
  }
});

// Regression guard for the var()-locality bug: the spine must read --zone-accent
// directly (resolved on the zone, where it is set inline), never through a
// :root-declared --zone-spine-color indirection (which resolves against a
// missing --zone-accent on :root and silently falls back to inherited --fg).
describe("spine colour wiring", () => {
  it("reads --zone-accent directly and keeps no --zone-spine-color indirection", () => {
    expect(css).not.toContain("--zone-spine-color");
    const spineRule = css.slice(css.indexOf(".zone-spine {"));
    const body = spineRule.slice(0, spineRule.indexOf("}"));
    expect(body).toContain("color: var(--zone-accent)");
  });
});
