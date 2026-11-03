// Live selections (#290): every board that draws cards — Team, Me and Backlog
// — selects a card on the first click and opens it on the second, shares the
// selection through the server's presence (login → card uid), and draws the
// others' marks on the cards they have selected. One rule for all three.
import type { Card } from "./providers/types";

/** selectedByOf is the logins that have the card selected: a selection of
 *  the card itself, or — on a parent whose subtasks are folded away — of
 *  one of its subtasks, whose mark it wears until they are shown. */
export function selectedByOf(
  presence: Record<string, string> | undefined,
  card: Pick<Card, "itemId">,
  subs?: { children: (id: string) => readonly Pick<Card, "itemId">[]; open: (id: string) => boolean },
): string[] | undefined {
  if (!presence) {
    return undefined;
  }
  const logins = Object.entries(presence)
    .filter(([, uid]) => {
      if (uid === card.itemId) {
        return true;
      }
      if (subs && !subs.open(card.itemId)) {
        return subs.children(card.itemId).some((c) => c.itemId === uid);
      }
      return false;
    })
    .map(([login]) => login);
  return logins.length > 0 ? logins : undefined;
}

/** clickOn is what a click on a card does: the first selects it, a click on
 *  the card already selected opens it. */
export function clickOn(selected: boolean): "select" | "open" {
  return selected ? "open" : "select";
}
