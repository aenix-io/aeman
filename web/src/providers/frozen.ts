// A provider over a board where some cards are being LOOKED AT rather than
// worked on: a past day is over for the teams whose sprint has moved on, and
// their cards are a record of that evening while the rest of the board is
// today's and stays workable.
//
// The guard lives here, at the one door to the server, rather than in each of
// the boards' twenty-odd handlers: a handler that forgot would write today's
// board from a view of the past — the card would jump to its live state and
// the day's picture would be quietly wrong.
//
// What the reader gets instead of a refusal is the board they were reaching
// for: the day leaves the record for TODAY, and the team whose card was
// touched is offered its carry-over, since a lead reaching into a day that is
// over is usually a lead whose sprint has not been closed yet.

import type { Provider } from "./types";

// READS are the calls a board being read needs. Everything else is guarded,
// so a provider method added later is guarded until it is classified — a read
// mistakenly refused shows up at once as a missing panel, while a write
// mistakenly allowed would silently edit the live board from a view of the
// past.
const READS = new Set([
  "loadBoard",
  "listCards",
  "getCard",
  "listProcesses",
  "listLinks",
  "listLog",
  "listDayLogs",
  "listNotes",
  "getPersonal",
]);

// QUIET are calls that change nothing durable and are not worth an error:
// presence is "who is looking at what right now", and looking at a past day
// is still looking.
const QUIET = new Set(["setPresence"]);

// CARD_WRITES take the card's uid first, so the guard can ask whether THAT
// card is a record. Anything else that writes is refused whenever any part of
// the view is a record: a create or a carry-over made from a picture of a
// past day would land in today's board with nobody having meant it.
const CARD_WRITES = new Set([
  "patchCard",
  "deleteCard",
  "removeCard",
  "moveCard",
  "moveCardBefore",
  "deferCard",
  "setInProgress",
  "reopen",
  "sendToReview",
  "removeReviewer",
  "placeCard",
  "untriageCard",
  "mirrorCard",
  "unmirrorCard",
  "removeFromProject",
  "addNote",
  "editNote",
  "deleteNote",
]);

/**
 * frozenProvider wraps a provider so the cards of a day that is over cannot
 * be written to. `isRecord(uid)` answers for one card; `anyRecord()` says
 * whether the view holds any at all, which is what guards the writes that
 * name no card.
 */
/** onFrozen is called when a write is refused, with the uid of the card it
 *  named (empty for a write that names none). What it returns is the message
 *  the caller sees; "" says nothing at all.
 *
 *  It is a CALLBACK and not a message because the answer to "this day is
 *  over" is an action, not a sentence: the board leaves the record for today,
 *  where the work can actually be done. The write itself is still refused —
 *  it was aimed at a picture of the past, and replaying it onto today's board
 *  is the very thing this guard exists to prevent. */
export function frozenProvider(
  inner: Provider,
  isRecord: (uid: string) => boolean,
  anyRecord: () => boolean,
  onFrozen: (uid: string) => string,
): Provider {
  return new Proxy(inner, {
    get(target, prop, receiver) {
      const value = Reflect.get(target, prop, receiver) as unknown;
      if (typeof value !== "function") {
        return value;
      }
      const name = String(prop);
      const call = (value as (...args: unknown[]) => unknown).bind(target);
      if (READS.has(name)) {
        return call;
      }
      if (QUIET.has(name)) {
        return () => Promise.resolve();
      }
      if (CARD_WRITES.has(name)) {
        return (...args: unknown[]) => {
          const uid = typeof args[0] === "string" ? args[0] : "";
          return isRecord(uid)
            ? Promise.reject(new Error(onFrozen(uid)))
            : call(...args);
        };
      }
      return anyRecord()
        ? () => Promise.reject(new Error(onFrozen("")))
        : call;
    },
  }) as Provider;
}
