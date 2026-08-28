// The day feed is ONE question — GET /logs?day=…&uids=… — answering every
// visible card's notes and events on the viewed day in a single batch, where
// a per-card GET /cards/{uid}/log walked the whole git history once per card
// (20-40 parallel walks on a board open, ~1s each). This module is the pure
// part of that call: splitting the wire entries back into the notes and
// events the UI knows. The whole-history question is still the card log,
// asked only inside a card's own pane.

import type { CardDayLog, CardEvent, Note } from "./providers/types";

/** DayLogEntry is one wire entry of GET /logs — the same LogEntry shape the
 *  card log returns. */
export interface DayLogEntry {
  type: string;
  id: string;
  at?: string;
  actor?: string;
  kind?: string;
  from?: string;
  to?: string;
  text?: string;
}

/** splitDayLogs turns the response's per-card entry lists into notes and
 *  events, keeping the server's (oldest-first) order within each. An entry of
 *  an unknown type is ignored — a newer server may know kinds this build does
 *  not. A quiet card keeps its empty entry ("nothing that day" is an answer),
 *  and a card the response left out (one the visitor cannot see) stays out. */
/** CardFrame is what a Card watch frame tells the day feed: which card, and
 *  whether it was deleted. */
export interface CardFrame {
  uid: string;
  deleted: boolean;
}

/** dayFeedUpdates decides what a burst of Card watch frames means for the day
 *  feed: which entries to drop (the card is gone — a stale entry must not
 *  stay, whether or not the card is still in the set) and which cards to
 *  re-ask in ONE batch (only those the feed shows, each once, first-seen
 *  order). A card's last frame wins when a burst contradicts itself. */
export function dayFeedUpdates(
  frames: readonly CardFrame[],
  inFeed: ReadonlySet<string>,
): { drop: string[]; refresh: string[] } {
  const order: string[] = [];
  const last = new Map<string, boolean>();
  for (const f of frames) {
    if (!last.has(f.uid)) {
      order.push(f.uid);
    }
    last.set(f.uid, f.deleted);
  }
  const drop: string[] = [];
  const refresh: string[] = [];
  for (const uid of order) {
    if (last.get(uid)) {
      drop.push(uid);
    } else if (inFeed.has(uid)) {
      refresh.push(uid);
    }
  }
  return { drop, refresh };
}

export function splitDayLogs(
  cards: Record<string, DayLogEntry[] | null> | null | undefined,
): Record<string, CardDayLog> {
  const out: Record<string, CardDayLog> = {};
  for (const [uid, entries] of Object.entries(cards ?? {})) {
    const notes: Note[] = [];
    const events: CardEvent[] = [];
    for (const e of entries ?? []) {
      if (e.type === "event") {
        events.push({
          id: e.id,
          kind: e.kind ?? "",
          actor: e.actor,
          from: e.from,
          to: e.to,
          at: e.at ?? "",
        });
      } else if (e.type === "note") {
        notes.push({
          id: e.id,
          body: e.text ?? "",
          createdAt: e.at ?? "",
          author: e.actor,
          source: "comment",
        });
      }
    }
    out[uid] = { notes, events };
  }
  return out;
}
