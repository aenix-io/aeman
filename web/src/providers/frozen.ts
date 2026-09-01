// A provider over a board that is being LOOKED AT rather than worked on: the
// snapshot of a past day. Reads pass through; anything that would change the
// board is refused with a reason the UI can show.
//
// The guard lives here, at the one door to the server, rather than in each of
// the boards' twenty-odd handlers: a handler that forgot the check would
// write today's board from a view of the past — the card would jump to its
// live state and the day's picture would be quietly wrong.

import type { Provider } from "./types";

// READS are the calls a snapshot needs. Everything else is refused, so a
// provider method added later is refused until it is classified — a read
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

/** frozenProvider wraps a provider so a past day cannot be written to. */
export function frozenProvider(inner: Provider, reason: string): Provider {
  return new Proxy(inner, {
    get(target, prop, receiver) {
      const value = Reflect.get(target, prop, receiver) as unknown;
      if (typeof value !== "function") {
        return value;
      }
      const name = String(prop);
      if (READS.has(name)) {
        return (value as (...args: unknown[]) => unknown).bind(target);
      }
      if (QUIET.has(name)) {
        return () => Promise.resolve();
      }
      return () => Promise.reject(new Error(reason));
    },
  }) as Provider;
}
