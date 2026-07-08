// Appearance preferences: a light/dark theme mode and a colour palette. Both
// are persisted in localStorage (like the app's other UI prefs) and applied as
// data-theme / data-palette attributes on <html>, which styles.css keys on.
//
// The palette axis is where the accessibility support lives: `vivid` is a
// red-green-friendly palette and `marine` a blue-yellow-friendly one, both
// verified against colour-vision-deficiency simulations. The on-screen labels
// stay purely aesthetic (Default / Vivid / Marine) so choosing one reveals
// nothing about the person choosing it.

export type ThemeMode = "light" | "dark" | "system";
export type Palette = "default" | "vivid" | "marine";
export type ResolvedTheme = "light" | "dark";

export interface Appearance {
  mode: ThemeMode;
  palette: Palette;
}

export const LS_MODE = "aeman.themeMode";
export const LS_PALETTE = "aeman.palette";
export const THEME_ATTR = "data-theme";
export const PALETTE_ATTR = "data-palette";

export const THEME_MODES: readonly ThemeMode[] = ["light", "dark", "system"];
export const PALETTES: readonly Palette[] = ["default", "vivid", "marine"];

export const DEFAULT_APPEARANCE: Appearance = { mode: "system", palette: "default" };

type StorageReader = Pick<Storage, "getItem">;
type StorageWriter = Pick<Storage, "setItem">;
type AttrRoot = Pick<Element, "setAttribute">;

function oneOf<T extends string>(value: string | null, allowed: readonly T[], fallback: T): T {
  return (allowed as readonly string[]).includes(value ?? "") ? (value as T) : fallback;
}

/** readAppearance validates both axes against their allowed values, falling
 *  back to the defaults for anything missing or unrecognised. */
export function readAppearance(storage: StorageReader): Appearance {
  return {
    mode: oneOf(storage.getItem(LS_MODE), THEME_MODES, DEFAULT_APPEARANCE.mode),
    palette: oneOf(storage.getItem(LS_PALETTE), PALETTES, DEFAULT_APPEARANCE.palette),
  };
}

/** persistAppearance stores both axes, swallowing storage failures (quota,
 *  private mode) the same way the rest of the app treats persisted prefs. */
export function persistAppearance(storage: StorageWriter, appearance: Appearance): void {
  try {
    storage.setItem(LS_MODE, appearance.mode);
    storage.setItem(LS_PALETTE, appearance.palette);
  } catch {
    // ignore persistence failures
  }
}

/** resolveTheme turns the mode into a concrete light/dark, consulting the OS
 *  preference only when the mode is "system". */
export function resolveTheme(mode: ThemeMode, prefersDark: boolean): ResolvedTheme {
  if (mode === "system") {
    return prefersDark ? "dark" : "light";
  }
  return mode;
}

/** applyAppearance writes the resolved theme and the palette onto the root
 *  element as the attributes styles.css targets. */
export function applyAppearance(root: AttrRoot, appearance: Appearance, prefersDark: boolean): void {
  root.setAttribute(THEME_ATTR, resolveTheme(appearance.mode, prefersDark));
  root.setAttribute(PALETTE_ATTR, appearance.palette);
}
