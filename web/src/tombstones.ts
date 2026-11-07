// The cards this tab deleted, for a while after it deleted them. A delete is
// several writes on the server — a plan item's children move up before the
// card goes — and a watch frame sent between them can still carry the card,
// arriving before the delete's answer or after the DELETED. Such a frame
// would draw the card again ("the epic comes back"); for the span below it
// is turned away. The DELETED itself always gets through, and a delete the
// server refused lifts the mark so the card comes back with the reload.

export class Tombstones {
  private readonly at = new Map<string, number>();

  constructor(private readonly spanMs = 60_000) {}

  /** mark records that this tab deleted a card at `now`. */
  mark(uid: string, now = Date.now()): void {
    this.at.set(uid, now);
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
