// The cards this tab deleted, for a while after it deleted them. A delete is
// several writes on the server — a plan item's children move up before the
// card goes — and a watch frame sent between them can still carry the card,
// arriving before the delete's answer or after the DELETED. Such a frame
// would draw the card again ("the epic comes back"); for the span below it
// is turned away. The DELETED itself always gets through, and a delete the
// server refused lifts the mark so the card comes back with the reload.

import { resolveCardId } from "./api/pending";
import type { Provider } from "./providers/types";

export class Tombstones {
  private readonly at = new Map<string, number>();

  constructor(private readonly spanMs = 60_000) {}

  /** mark records that this tab deleted a card at `now`, and forgets the
   *  marks that have run out, so the set does not only grow. */
  mark(uid: string, now = Date.now()): void {
    for (const [id, t] of this.at) {
      if (now - t > this.spanMs) {
        this.at.delete(id);
      }
    }
    this.at.set(uid, now);
  }

  /** size is how many marks are held. */
  get size(): number {
    return this.at.size;
  }

  /** lift drops the mark: the delete was refused. */
  lift(uid: string): void {
    this.at.delete(uid);
  }

  /** blocks reports a frame to turn away: any but a DELETED, for a card this
   *  tab deleted within the span. */
  blocks(uid: string, type: string, now = Date.now()): boolean {
    const t = this.at.get(uid);
    if (t === undefined) {
      return false;
    }
    if (now - t > this.spanMs) {
      this.at.delete(uid);
      return false;
    }
    return type !== "DELETED";
  }
}

/** withTombstones marks the cards a delete takes away: every deleteCard, and
 *  a remove the board that sent it says deletes ({deletes: true} — a plan
 *  item's ×, where the board knows; a day board's remove may hand a task
 *  back instead, and the server's DELETED says which). A card whose create
 *  is still in flight is marked under its real id too, once the server names
 *  it (resolveCardId). A delete refused lifts the marks. */
export function withTombstones(p: Provider, t: Tombstones, resolve: (id: string) => Promise<string> = resolveCardId): Provider {
  const marking = (uid: string, write: () => Promise<void>): Promise<void> => {
    t.mark(uid);
    let real = uid;
    void resolve(uid).then((id) => {
      real = id;
      if (id !== uid) {
        t.mark(id);
      }
    });
    return write().catch((err: unknown) => {
      t.lift(uid);
      t.lift(real);
      throw err;
    });
  };
  return {
    ...p,
    deleteCard: (uid) => marking(uid, () => p.deleteCard(uid)),
    removeCard: (uid, intent, opts) =>
      opts?.deletes ? marking(uid, () => p.removeCard(uid, intent, opts)) : p.removeCard(uid, intent, opts),
  };
}
