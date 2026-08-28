// The personal board: a repository of the visitor's own, attached to the board
// as the domain `~<login>` for them alone. Its cards ride in the same card list
// as everything else and are told apart by that domain; these helpers do the
// telling so the Me board can draw them as a column of their own.

import type { Card } from "./providers/types";

/** PersonalBoard is the visitor's link as GET /board reports it. */
export interface PersonalBoard {
  /** The domain name: `~<login>`. */
  domain: string;
  /** The repository URL the visitor linked. */
  url: string;
}

/** personalRepoName is the short `owner/name` of a git URL — https or ssh,
 *  with or without `.git` — for the column header and the user menu. A
 *  nested (grouped) path keeps its last two segments; a lone segment stands
 *  alone; an unparsable string is returned trimmed. */
export function personalRepoName(url: string): string {
  const s = url.trim().replace(/\/+$/, "").replace(/\.git$/i, "");
  if (!s) {
    return "";
  }
  let path: string;
  const scheme = /^[a-z][a-z0-9+.-]*:\/\//i.exec(s);
  if (scheme) {
    const rest = s.slice(scheme[0].length);
    const slash = rest.indexOf("/");
    path = slash >= 0 ? rest.slice(slash + 1) : "";
  } else {
    // git@host:owner/name — the scp-like form.
    const scp = /^[^/@]+@[^/:]+:(.*)$/.exec(s);
    path = scp ? scp[1] : s;
  }
  const segments = path.split("/").filter(Boolean);
  return segments.length ? segments.slice(-2).join("/") : s;
}

/** isPersonalCard tells a card of the visitor's personal board: its domain is
 *  the personal one. A card without a domain lives on the shared board. */
export function isPersonalCard(
  card: Card,
  personal: PersonalBoard | null | undefined,
): boolean {
  return !!personal && !!card.domain && card.domain === personal.domain;
}

/** splitPersonal divides one card list into the day board's cards and the
 *  personal column's, both in their original order. Without a personal board
 *  everything is the day board's. */
export function splitPersonal(
  cards: readonly Card[],
  personal: PersonalBoard | null | undefined,
): { team: Card[]; personal: Card[] } {
  const team: Card[] = [];
  const mine: Card[] = [];
  for (const c of cards) {
    (isPersonalCard(c, personal) ? mine : team).push(c);
  }
  return { team, personal: mine };
}

/** personalShows mirrors view=personal: every open card, plus the ones done
 *  today — a card done before today has left the column. `doneAt` is the
 *  board day (yyyy-mm-dd) the card reached done. */
export function personalShows(card: Card, today: string): boolean {
  return !card.doneAt || card.doneAt >= today;
}

/** RecurrenceCycle is a recurrent card's stored reseed cycle: "" is the
 *  board's own turn — the sprint on a team board, the day on a personal one. */
export type RecurrenceCycle = "" | "week" | "month";

export const recurrenceCycles: readonly RecurrenceCycle[] = ["", "week", "month"];

/** recurrenceLabel names a cycle in the stage menu. A personal board has no
 *  sprint to turn with, so the default cycle turns with the day there: the
 *  same stored "" reads "Every day" on a personal card and "Every sprint" on
 *  a team card. */
export function recurrenceLabel(cycle: RecurrenceCycle, personal: boolean): string {
  switch (cycle) {
    case "week":
      return "Weekly";
    case "month":
      return "Monthly";
    default:
      return personal ? "Every day" : "Every sprint";
  }
}

/** recurrenceTitle is the status icon's tooltip on a recurrent card. */
export function recurrenceTitle(cycle: string | undefined, personal: boolean): string {
  switch (cycle) {
    case "week":
      return "Recurrent (weekly)";
    case "month":
      return "Recurrent (monthly)";
    default:
      return personal ? "Recurrent (daily)" : "Recurrent";
  }
}
