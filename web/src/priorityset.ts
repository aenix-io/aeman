// Setting a card's OWN priority (docs/design/backlog.md, K15) from wherever
// it is offered — the Roadmap's badge menu and the card drawer's Priority
// row. One press: the card's own value and what counts under it (the cards
// under it, their review cards — roadmap.priorityAfter) shown at once,
// settled by the card the server answers with (the watch re-sends the rest),
// put back on a refusal; an answer to an older press never overwrites a
// newer one (roadmap.latestWins).
import { priorityAfter, refusalRollback } from "./roadmap";
import type { Card, Priority } from "./providers/types";

export interface PriorityDeps {
  cards: readonly Card[];
  patchCard: (id: string, patch: Partial<Card>) => void;
  provider: { setPriority: (uid: string, priority: Priority | "") => Promise<Card> };
  seq: { next: (id: string) => number; isLatest: (id: string, n: number) => boolean };
  onError: (message: string) => void;
  reload: () => void;
}

const errText = (err: unknown) => (err instanceof Error ? err.message : String(err));

export async function pressPriority(d: PriorityDeps, card: Card, value: Priority | ""): Promise<void> {
  const shown = priorityAfter(d.cards, card.itemId, value);
  const before = d.cards
    .filter((c) => c.itemId === card.itemId || shown.has(c.itemId))
    .map((c) => ({ id: c.itemId, patch: { priority: c.priority, effectivePriority: c.effectivePriority, effectivePriorityFrom: c.effectivePriorityFrom } }));
  d.patchCard(card.itemId, { priority: value || undefined });
  for (const [id, p] of shown) {
    d.patchCard(id, p);
  }
  const n = d.seq.next(card.itemId);
  try {
    const c = await d.provider.setPriority(card.itemId, value);
    if (d.seq.isLatest(card.itemId, n)) {
      d.patchCard(c.itemId, { priority: c.priority, effectivePriority: c.effectivePriority, effectivePriorityFrom: c.effectivePriorityFrom });
    }
  } catch (err: unknown) {
    for (const b of refusalRollback(d.seq, card.itemId, n, before)) {
      d.patchCard(b.id, b.patch);
    }
    d.onError(errText(err));
    d.reload();
  }
}
