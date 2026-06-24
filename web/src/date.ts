/** todayIso returns today's date as a local yyyy-mm-dd string. */
export function todayIso(): string {
  const now = new Date();
  const off = now.getTimezoneOffset();
  return new Date(now.getTime() - off * 60_000).toISOString().slice(0, 10);
}

/** localDateIso returns the local yyyy-mm-dd date for an ISO timestamp. */
export function localDateIso(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) {
    return "";
  }
  const off = d.getTimezoneOffset();
  return new Date(d.getTime() - off * 60_000).toISOString().slice(0, 10);
}

/** activeOnDay is true when `day` falls within a card's [start, finish] range.
 * A missing bound collapses to the other date, so a card with a single date is
 * active only on that day; a card with neither date is never on the day board. */
export function activeOnDay(
  start: string | undefined,
  finish: string | undefined,
  day: string,
): boolean {
  const from = start ?? finish;
  const to = finish ?? start;
  if (!from || !to) {
    return false;
  }
  return from <= day && day <= to;
}

/** daysSince returns whole days between an ISO timestamp's local date and the
 * `asOf` day (a yyyy-mm-dd string, default today). */
export function daysSince(iso: string | undefined, asOf?: string): number {
  if (!iso) {
    return 0;
  }
  const then = localDateIso(iso);
  if (!then) {
    return 0;
  }
  const [py, pm, pd] = then.split("-").map(Number);
  const [ty, tm, td] = (asOf || todayIso()).split("-").map(Number);
  const ms = Date.UTC(ty, tm - 1, td) - Date.UTC(py, pm - 1, pd);
  return Math.max(0, Math.round(ms / 86_400_000));
}

/** addDays shifts a yyyy-mm-dd date by delta days, returning yyyy-mm-dd. */
export function addDays(iso: string, delta: number): string {
  const [y, m, d] = iso.split("-").map(Number);
  if (!y || !m || !d) {
    return iso;
  }
  const dt = new Date(y, m - 1, d + delta);
  const mm = String(dt.getMonth() + 1).padStart(2, "0");
  const dd = String(dt.getDate()).padStart(2, "0");
  return `${dt.getFullYear()}-${mm}-${dd}`;
}
