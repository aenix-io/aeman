// Live selections (#290): every board that draws cards — Team, Me and Backlog
// — selects a card on the first click and opens it on the second, shares the
// selection through the server's presence (login → card uid), and draws the
// others' marks on the cards they have selected. One rule for all three.
import type { Card } from "./providers/types";

/** PresenceMap is the live selections, keyed by the TAB they are from (the
 *  client id the server keys them by): two tabs of one person are two
 *  selections, and closing one leaves the other's mark. */
export type PresenceMap = Record<string, { login: string; card: string }>;

/** presenceAfter is the map once a Presence frame is applied: a selection
 *  sets its tab's entry, an empty card clears that tab's alone. A frame
 *  that names no tab is keyed by its login. */
export function presenceAfter(map: PresenceMap, frame: { login: string; card: string; client?: string }): PresenceMap {
  const key = frame.client || frame.login;
  const next = { ...map };
  if (frame.card) {
    next[key] = { login: frame.login, card: frame.card };
  } else {
    delete next[key];
  }
  return next;
}

/** selectedByOf is the logins that have the card selected: a selection of
 *  the card itself, or — on a parent whose subtasks are folded away — of
 *  one of its subtasks, whose mark it wears until they are shown. A person
 *  is one mark however many tabs they have it in, and the reader (me) is
 *  never a mark: their own other tabs are not somebody else. */
export function selectedByOf(
  presence: PresenceMap | undefined,
  card: Pick<Card, "itemId">,
  subs?: { children: (id: string) => readonly Pick<Card, "itemId">[]; open: (id: string) => boolean },
  me?: string,
): string[] | undefined {
  if (!presence) {
    return undefined;
  }
  const logins = new Set<string>();
  for (const { login, card: uid } of Object.values(presence)) {
    if (login === me) {
      continue;
    }
    const on =
      uid === card.itemId || (!!subs && !subs.open(card.itemId) && subs.children(card.itemId).some((c) => c.itemId === uid));
    if (on) {
      logins.add(login);
    }
  }
  return logins.size > 0 ? [...logins] : undefined;
}

/** clickOn is what a click on a card does: the first selects it, a click on
 *  the card already selected opens it. */
export function clickOn(selected: boolean): "select" | "open" {
  return selected ? "open" : "select";
}
