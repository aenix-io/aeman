/** The project chips' selection on the Project tab. null = every project;
 *  "" = the no-project bucket. Remembered per browser. */

const LS_FILTER = "aeman.projectFilter";

export function readProjectFilter(): string[] | null {
  try {
    const raw = localStorage.getItem(LS_FILTER);
    const v: unknown = raw ? JSON.parse(raw) : null;
    return Array.isArray(v) && v.every((x) => typeof x === "string") ? v : null;
  } catch {
    return null;
  }
}

export function writeProjectFilter(keys: string[] | null): void {
  try {
    localStorage.setItem(LS_FILTER, JSON.stringify(keys));
  } catch {
    // A full or blocked store is not worth a broken board.
  }
}
