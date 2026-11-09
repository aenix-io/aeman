// The ⌘K search over every card the visitor can read: which cards answer a
// query and in what order. Ranked the way a launcher ranks — what the title IS
// before what it merely contains, a word's start before the inside of a word,
// open work before finished work, the newest first among equals — so the card
// meant is the first row and Enter opens it. The listing it searches is the
// server's (view=all, already filtered by what the visitor may read); this
// file decides nothing else.
import { isComplete } from "./stages";
import type { Card } from "./providers/types";

/** The hidden per-team card the sprint pointers live on (board.SprintStateTitle). */
const SPRINT_STATE = "aeman:sprint-state";

const words = (q: string): string[] => q.toLowerCase().split(/\s+/).filter(Boolean);

/** atWordStart reports that needle occurs in hay at the start of a word. */
function atWordStart(hay: string, needle: string): boolean {
  for (let i = hay.indexOf(needle); i !== -1; i = hay.indexOf(needle, i + 1)) {
    if (i === 0 || !/[\p{L}\p{N}]/u.test(hay[i - 1])) {
      return true;
    }
  }
  return false;
}

/** score is how strongly a card answers the query, 0 when it does not: every
 *  word must be in its title or in the title of a plan item above it. */
function score(c: Card, q: string, ws: string[]): number {
  const title = c.title.toLowerCase();
  const plan = (c.lineage ?? []).map((l) => l.title.toLowerCase()).join(" ");
  if (!ws.every((w) => title.includes(w) || plan.includes(w))) {
    return 0;
  }
  let s: number;
  if (title === q) {
    s = 1000;
  } else if (title.startsWith(q)) {
    s = 800;
  } else if (atWordStart(title, q)) {
    s = 600;
  } else if (title.includes(q)) {
    s = 400;
  } else if (ws.every((w) => title.includes(w))) {
    s = ws.every((w) => atWordStart(title, w)) ? 300 : 200;
  } else {
    s = 100; // a word found only in the plan above it
  }
  return isComplete(c) ? s : s + 50;
}

/** searchCards is the ranked answer to a query, at most limit cards. A pasted
 *  uid opens that card alone. */
export function searchCards(cards: readonly Card[], query: string, limit = 50): Card[] {
  const q = query.trim().toLowerCase().replace(/\s+/g, " ");
  if (!q) {
    return [];
  }
  const byUid = cards.find((c) => c.itemId.toLowerCase() === q);
  if (byUid) {
    return [byUid];
  }
  const ws = words(q);
  const hits: { c: Card; s: number; at: number }[] = [];
  cards.forEach((c, at) => {
    if (c.title === SPRINT_STATE) {
      return;
    }
    const s = score(c, q, ws);
    if (s > 0) {
      hits.push({ c, s, at });
    }
  });
  hits.sort(
    (a, b) =>
      b.s - a.s ||
      (b.c.createdAt ?? "").localeCompare(a.c.createdAt ?? "") ||
      a.at - b.at,
  );
  return hits.slice(0, limit).map((h) => h.c);
}

/** A run of a title the result row draws, marked when a query word matched it. */
export interface Run {
  text: string;
  hit: boolean;
}

/** highlight splits a title into the runs a row draws: every place a word of
 *  the query matched is marked, overlapping matches merged into one run. */
export function highlight(title: string, query: string): Run[] {
  const lower = title.toLowerCase();
  const marked = new Array<boolean>(title.length).fill(false);
  for (const w of words(query)) {
    for (let i = lower.indexOf(w); i !== -1; i = lower.indexOf(w, i + 1)) {
      marked.fill(true, i, i + w.length);
    }
  }
  const runs: Run[] = [];
  for (let i = 0; i < title.length; i++) {
    const last = runs[runs.length - 1];
    if (last && last.hit === marked[i]) {
      last.text += title[i];
    } else {
      runs.push({ text: title[i], hit: marked[i] });
    }
  }
  return runs.length ? runs : [{ text: title, hit: false }];
}
