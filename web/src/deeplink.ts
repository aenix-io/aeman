// The board's address is its state (docs/api.md, "Deep links"): the view and
// the open card live in the query of the root document —
// /?view=roadmap&card=<uid> — and a day board (Me, Team) looked at on a day
// other than today carries it too (&day=YYYY-MM-DD).
//
// The query of the root rather than a path: the address is then always the
// one document the server serves uncached (spaHandler), no board path can
// shadow anything the server routes (/api, /auth, /oauth, /mcp), and the
// bundle's asset paths never depend on how deep an address is.
import { storedView, type ViewMode } from "./viewquery";

export interface LinkState {
  view: ViewMode | null;
  card: string | null;
  day: string | null;
}

const UID = /^[A-Za-z0-9_-]{1,64}$/;
const DAY = /^\d{4}-\d{2}-\d{2}$/;
const VIEWS = new Set(["me", "team", "nixon", "project", "plan", "backlog", "triage", "roadmap"]);

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
    day: day && DAY.test(day) ? day : null,
  };
}

const hasDays = (view: ViewMode | null) => view === "me" || view === "team";

/** buildLink writes the board's address: the view, the day on a day board
 *  when it is not today, and the open card. */
export function buildLink(s: LinkState, today: string): string {
  const q = new URLSearchParams();
  if (s.view) {
    q.set("view", s.view);
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

/** cardLink is a card's address on this board, for sharing. */
export function cardLink(origin: string, view: ViewMode, uid: string): string {
  return `${origin}/${buildLink({ view, card: uid, day: null }, "")}`;
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

// An address on this board: a path from its root, never a scheme, a host
// ("//evil") or a backslash a browser would read as one.
const local = (p: string) => p.startsWith("/") && !p.startsWith("//") && !/[\\\r\n]/.test(p);

/** saveReturnTo keeps the address a sign-in begins on, for the round trip
 *  through the forge — in the tab's session storage, which survives it. */
export function saveReturnTo(storage: KeyStore, path: string): void {
  if (local(path)) {
    storage.setItem(RETURN_TO, path);
  }
}

/** takeReturnTo gives the kept address back once, or null. */
export function takeReturnTo(storage: KeyStore): string | null {
  const p = storage.getItem(RETURN_TO);
  storage.removeItem(RETURN_TO);
  return p && local(p) ? p : null;
}
