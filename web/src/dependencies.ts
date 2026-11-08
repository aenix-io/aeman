// What a card waits for (docs/design/dependencies.md): the drawer's
// "Depends on" picker and list, its read-only "Blocks" list, and the lock's
// tooltip. The lock itself — whether a dependency is satisfied, what a plan
// item passes down — is the server's (status.locked, status.blockedBy); the
// client never decides it, so there is no rule here to drift from Go. What
// lives here is what the drawer offers and how it names what it shows.
import { isUnder, parentChainOf, type ParentOption } from "./roadmap";
import { isComplete } from "./stages";
import type { Card, LineageRef } from "./providers/types";

/** dependencyOptions is the "Depends on" picker's list for `self`: any card
 *  the board holds but a review card (a review neither waits nor is waited
 *  for), the card itself, its ancestors and the cards under it (a plan item
 *  is done when its work is — waiting on either is meaningless), and those
 *  already listed; found by its own title or an ancestor's, each with its
 *  chain. A review card is offered nothing. The server refuses the same
 *  targets (422 dependencyTarget) and a cycle (422 dependencyCycle). */
export function dependencyOptions(cards: readonly Card[], self: Card, query: string): ParentOption[] {
  if (self.reviewOf) {
    return [];
  }
  const q = query.trim().toLowerCase();
  const listed = new Set(self.dependsOn ?? []);
  const above = new Set([...parentChainOf(self, cards).map((c) => c.itemId), ...(self.lineage ?? []).map((l) => l.uid)]);
  const out: ParentOption[] = [];
  for (const c of cards) {
    if (c.reviewOf || c.itemId === self.itemId || listed.has(c.itemId) || above.has(c.itemId)) {
      continue;
    }
    if (isUnder(cards, c.itemId, self.itemId) || (c.lineage ?? []).some((l) => l.uid === self.itemId)) {
      continue;
    }
    const chain = parentChainOf(c, cards);
    if (q && ![c, ...chain].some((x) => x.title.toLowerCase().includes(q))) {
      continue;
    }
    out.push({ card: c, chain });
  }
  return out;
}

/** DependencyRow is one entry of the "Depends on" list: the target's uid and
 *  title, and the card itself when the board holds it (for its status pill
 *  and kind). A target the board does not hold is named by the server's
 *  blocker entry when it has one, else by its uid. */
export interface DependencyRow extends LineageRef {
  card?: Card;
}

export function dependencyRows(self: Card, cards: readonly Card[]): DependencyRow[] {
  const byId = new Map(cards.map((c) => [c.itemId, c]));
  const blockers = new Map((self.blockedBy ?? []).map((b) => [b.uid, b]));
  return (self.dependsOn ?? []).map((uid) => {
    const c = byId.get(uid);
    if (c) {
      return { uid, title: c.title, type: c.type, card: c };
    }
    const b = blockers.get(uid);
    return { uid, title: b?.title ?? uid, type: b?.type };
  });
}

/** blocksOf is what waits for this card directly: the server's answer
 *  (status.blocks, the cards the reader may see), else what the cards held
 *  say — for a card the drawer made before the server answered. */
export function blocksOf(self: Card, cards: readonly Card[]): LineageRef[] {
  if (self.blocks) {
    return self.blocks;
  }
  return cards
    .filter((c) => c.itemId !== self.itemId && (c.dependsOn ?? []).includes(self.itemId))
    .map((c) => ({ uid: c.itemId, title: c.title, type: c.type }));
}

/** lockTitle is the derived lock's tooltip — null when the card is not
 *  locked: the blockers the reader may see, each with the ancestor it is
 *  inherited through when it is (`titles` names those by uid), and how many
 *  more there are that the reader may not see. */
export function lockTitle(c: Pick<Card, "locked" | "blockedBy" | "hiddenBlockers">, titles?: ReadonlyMap<string, string>): string | null {
  if (!c.locked) {
    return null;
  }
  const named = (c.blockedBy ?? []).map((b) => {
    const via = b.via ? titles?.get(b.via) : undefined;
    return via ? `«${b.title}» (through «${via}»)` : `«${b.title}»`;
  });
  const hidden = c.hiddenBlockers ?? 0;
  if (named.length === 0) {
    return `waiting for ${hidden} hidden ${hidden === 1 ? "card" : "cards"}`;
  }
  return `waiting for ${named.join(", ")}${hidden > 0 ? ` and ${hidden} hidden` : ""}`;
}

/** withDependency adds an id to a list once; withoutDependency takes it off:
 *  the optimistic lists the drawer shows while the write is on its way. */
export function withDependency(list: readonly string[] | undefined, id: string): string[] {
  const out = [...(list ?? [])];
  if (!out.includes(id)) {
    out.push(id);
  }
  return out;
}

export function withoutDependency(list: readonly string[] | undefined, id: string): string[] {
  return (list ?? []).filter((x) => x !== id);
}

/** derivedLockOf is what the day boards draw for the derived lock: the
 *  locked stage's look, its tooltip naming what the card waits for — on a
 *  card no stage of a person's says more about and that is not done; null
 *  otherwise. It changes nothing a board lets the card do. */
export function derivedLockOf(c: Card): string | null {
  if (c.stage || isComplete(c)) {
    return null;
  }
  return lockTitle(c, new Map((c.lineage ?? []).map((l) => [l.uid, l.title])));
}

/** dependenciesToFetch is the dependencies the drawer still has to fetch by
 *  id: the ones neither held (listing, board, already fetched) nor already
 *  asked for. An id is asked for ONCE per drawer card, whatever the answer —
 *  a target the visitor cannot read, a deleted one, a network error: asking
 *  again would get the same answer, and asking on every render was a request
 *  loop. A new drawer card starts with nothing asked. */
export function dependenciesToFetch(
  dependsOn: readonly string[] | undefined,
  have: ReadonlySet<string>,
  tried: ReadonlySet<string>,
): string[] {
  return (dependsOn ?? []).filter((id) => !have.has(id) && !tried.has(id));
}

/** withFetched adds the cards a fetch brought back to the drawer's fetched
 *  list — and hands back the SAME list when nothing new came, so a fetch that
 *  found nothing changes no state and reruns no effect. */
export function withFetched(prev: readonly Card[], got: readonly (Card | null)[]): readonly Card[] {
  const held = new Set(prev.map((c) => c.itemId));
  const added: Card[] = [];
  for (const c of got) {
    if (c && !held.has(c.itemId)) {
      held.add(c.itemId);
      added.push(c);
    }
  }
  return added.length === 0 ? prev : [...prev, ...added];
}
