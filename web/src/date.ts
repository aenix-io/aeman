// The board's days live in ONE time zone for every user (the server's
// AEMAN_TZ, delivered via /api/config): without it a user east of the server
// crosses their local midnight and starts seeing deferred "tomorrow" cards on
// today's board. Unset = the browser's own zone (single-zone teams).
let boardTz: string | undefined;

/** setBoardTimezone installs the board's day zone (an IANA name, "" = local). */
export function setBoardTimezone(tz: string | undefined): void {
  boardTz = tz && tz !== "Local" ? tz : undefined;
}

// en-CA formats as yyyy-mm-dd; an invalid zone falls back to the browser's.
function dayInBoardZone(d: Date): string {
  try {
    return new Intl.DateTimeFormat("en-CA", {
      timeZone: boardTz,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(d);
  } catch {
    return new Intl.DateTimeFormat("en-CA", {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(d);
  }
}

/** todayIso returns today's date as a yyyy-mm-dd string in the BOARD zone. */
export function todayIso(): string {
  return dayInBoardZone(new Date());
}

/** localDateIso returns the board-zone yyyy-mm-dd date for an ISO timestamp. */
export function localDateIso(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) {
    return "";
  }
  return dayInBoardZone(d);
}

/** activeOnDay is true when `day` falls within a card's visible span. The span
 * runs from `start` (the card's day) to the later of start/finish, so a card
 * whose start day is past its sprint (added on a later day) still shows on its
 * day, while a carried card shows from its origin through the current sprint. A
 * card with neither date is never on the day board. */
export function activeOnDay(
  start: string | undefined,
  finish: string | undefined,
  day: string,
): boolean {
  const from = start || finish;
  if (!from) {
    return false;
  }
  const to = finish && finish > from ? finish : from;
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

/** mondayOf returns the yyyy-mm-dd Monday of the week containing `iso`. */
export function mondayOf(iso: string): string {
  const [y, m, d] = iso.split("-").map(Number);
  if (!y || !m || !d) {
    return iso;
  }
  const dt = new Date(y, m - 1, d);
  dt.setDate(dt.getDate() - ((dt.getDay() + 6) % 7));
  const mm = String(dt.getMonth() + 1).padStart(2, "0");
  const dd = String(dt.getDate()).padStart(2, "0");
  return `${dt.getFullYear()}-${mm}-${dd}`;
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

/** weeksBetween counts whole weeks from Monday a to Monday b (0 = same week).
 *  Both boards measure a slot's span with it: the Project board to place a
 *  row, the Team board to keep that span when the plan panel moves a slot. */
export function weeksBetween(a: string, b: string): number {
  const [ay, am, ad] = a.split("-").map(Number);
  const [by, bm, bd] = b.split("-").map(Number);
  return Math.round(
    (Date.UTC(by, bm - 1, bd) - Date.UTC(ay, am - 1, ad)) / (7 * 86400000),
  );
}
