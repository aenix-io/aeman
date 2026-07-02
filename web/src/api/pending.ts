/** Registry of in-flight card creations, keyed by the optimistic `tmp-` id the
 * board renders while the create round-trips. Mutations fired against a card
 * that is still being created wait here for the real uid instead of hitting
 * the API with an id the server has never seen ("card not found"): the
 * provider resolves every card id through `resolveCardId` before sending, so
 * a create → edit → edit burst is applied in order once the create lands. If
 * the create fails, the queued mutations reject and their optimistic patches
 * roll back through the callers' existing error paths. */
const pending = new Map<string, Promise<string>>();

/** registerPendingCard maps an optimistic tmp id onto the create call's real
 * uid. The entry removes itself once the create settles — by then the boards
 * have swapped the tmp id for the real one in local state. */
export function registerPendingCard(
  tmpId: string,
  realId: Promise<string>,
): void {
  // Swallow the rejection on the registry's own reference: the create call
  // site handles the failure; queued consumers get their own rejections.
  realId.catch(() => undefined).finally(() => pending.delete(tmpId));
  pending.set(tmpId, realId);
}

/** resolveCardId maps a possibly-optimistic card id onto the real one,
 * waiting for the in-flight create when needed. */
export async function resolveCardId(id: string): Promise<string> {
  if (!id.startsWith("tmp-")) {
    return id;
  }
  const real = pending.get(id);
  if (!real) {
    throw new Error("card is still being created");
  }
  return real;
}

/** Tmp cards the user deleted while their create was still in flight. The
 * create's completion handler consumes the mark: it skips re-adding the card
 * and deletes the freshly created twin on the server instead. */
const cancelled = new Set<string>();

/** cancelPendingCard marks an in-flight create as cancelled; false when no
 * create is pending under this tmp id (nothing to cancel). */
export function cancelPendingCard(tmpId: string): boolean {
  if (!pending.has(tmpId)) {
    return false;
  }
  cancelled.add(tmpId);
  return true;
}

/** consumePendingCancel reports (and clears) a cancellation mark. */
export function consumePendingCancel(tmpId: string): boolean {
  return cancelled.delete(tmpId);
}
