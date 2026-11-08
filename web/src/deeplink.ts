// The board's address is its state (docs/api.md, "Deep links"): the view and
// the open card live in the query of the root document —
// /?view=strategy&card=<uid> — and a day board (Me, Team) looked at on a day
// other than today carries it too (&day=YYYY-MM-DD).
//
// The query of the root rather than a path: the address is then always the
// one document the server serves uncached (spaHandler), no board path can
// shadow anything the server routes (/api, /auth, /oauth, /mcp), and the
// bundle's asset paths never depend on how deep an address is.
import { storedView, wireView, type ViewMode } from "./viewquery";

export interface LinkState {
  view: ViewMode | null;
  card: string | null;
  day: string | null;
}

const UID = /^[A-Za-z0-9_-]{1,64}$/;
const DAY = /^(\d{4})-(\d{2})-(\d{2})$/;

// A real calendar day: four, two and two digits that name a date that
// exists — 2026-02-31 does not.
function realDay(s: string): boolean {
  const m = DAY.exec(s);
  if (!m) {
    return false;
  }
  const d = new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])));
  return d.toISOString().slice(0, 10) === s;
}
// "roadmap" is the Strategy board's old name: an old link still opens it, and
// the address effect writes it back as "strategy".
const VIEWS = new Set(["me", "team", "nixon", "project", "plan", "backlog", "triage", "roadmap", "strategy"]);

/** parseLink reads the board's address. An unknown view, an id that is not
 *  one, a day that is not one: each is taken as not there. */
export function parseLink(search: string): LinkState {
  const q = new URLSearchParams(search);
  const v = q.get("view");
  const card = q.get("card");
  const day = q.get("day");
  return {
    view: v && VIEWS.has(v) ? storedView(v) : null,
    card: card && UID.test(card) ? card : null,
    day: day && realDay(day) ? day : null,
  };
}

const hasDays = (view: ViewMode | null) => view === "me" || view === "team";

/** buildLink writes the board's address: the view, the day on a day board
 *  when it is not today, and the open card. */
export function buildLink(s: LinkState, today: string): string {
  const q = new URLSearchParams();
  if (s.view) {
    q.set("view", wireView(s.view));
  }
  if (hasDays(s.view) && s.day && s.day !== today) {
    q.set("day", s.day);
  }
  if (s.card) {
    q.set("card", s.card);
  }
  const out = q.toString();
  return out ? `?${out}` : "";
}

/** startDay is the day the board starts on: the linked one, else today.
 *  The config that sets the board's zone puts the board there, so a linked
 *  day outlives the start. */
export function startDay(link: LinkState, today: string): string {
  return link.day ?? today;
}

/** cardLink is a card's address on this board, for sharing: the address bar
 *  as it would read with this card open — the day a day board shows
 *  included. */
export function cardLink(origin: string, pathname: string, view: ViewMode, uid: string, day: string, today: string): string {
  return `${origin}${pathname}${buildLink({ view, card: uid, day }, today)}`;
}

/** cardUnreadable reports a refusal to fetch a linked card that means the
 *  visitor may not read it, or it is gone (403, 404) — a notice, and the
 *  card leaves the address. Anything else is an error to show. */
export function cardUnreadable(err: unknown): boolean {
  const status = (err as { status?: unknown } | null)?.status;
  return status === 403 || status === 404;
}

/** linkChange says how a new address reaches the history: opening, closing
 *  or switching the card is a step Back undoes (push); a tab or a day only
 *  rewrites the address (replace). */
export function linkChange(was: string, now: string): "push" | "replace" | "none" {
  if (was === now) {
    return "none";
  }
  return parseLink(was).card !== parseLink(now).card ? "push" : "replace";
}

interface KeyStore {
  getItem(k: string): string | null;
  setItem(k: string, v: string): void;
  removeItem(k: string): void;
}

const RETURN_TO = "aeman.returnTo";

// How long a kept address waits for the sign-in to come back: the forge's
// state lives ten minutes, and a sign-in abandoned longer ago is over.
const RETURN_TTL_MS = 10 * 60 * 1000;

// An address on this board: a path from its root, never a scheme, a host
// ("//evil"), a backslash a browser would read as one, or a control
// character a browser would drop.
// eslint-disable-next-line no-control-regex
const local = (p: string) => p.startsWith("/") && !p.startsWith("//") && !/[\\\u0000-\u001f\u007f]/.test(p);

/** saveReturnTo keeps the address a sign-in begins on, for the round trip
 *  through the forge — in the tab's session storage, which survives it —
 *  with the moment it was kept. */
export function saveReturnTo(storage: KeyStore, path: string, now: number): void {
  if (local(path)) {
    storage.setItem(RETURN_TO, JSON.stringify({ path, at: now }));
  }
}

/** takeReturnTo takes the kept address off on every load, and gives it back
 *  only while the sign-in it was kept for can still be coming back. */
export function takeReturnTo(storage: KeyStore, now: number): string | null {
  const raw = storage.getItem(RETURN_TO);
  storage.removeItem(RETURN_TO);
  if (!raw) {
    return null;
  }
  try {
    const kept = JSON.parse(raw) as { path?: unknown; at?: unknown };
    if (typeof kept.path !== "string" || typeof kept.at !== "number" || now - kept.at > RETURN_TTL_MS || now < kept.at) {
      return null;
    }
    return local(kept.path) ? kept.path : null;
  } catch {
    return null;
  }
}
