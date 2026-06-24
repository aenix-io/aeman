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
