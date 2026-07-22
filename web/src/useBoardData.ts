// useBoardData owns everything one board needs to live on screen: its state,
// the card mutators the components share, the lazy per-view card fetch, and
// the WebSocket watch that keeps it in sync. Extracted from App so the app can
// run TWO boards at once — the work board and the user's personal board — as
// two independent instances (each with its own socket, cache identity and
// server address) instead of one merged stream with per-card routing. See
// docs/design/personal-board.md.

import { useCallback, useEffect, useRef, useState } from "react";
import { clientId } from "./api/client";
import {
  resourceToCard,
  sprintStateFrom,
  type CardResource,
  type OrderingResource,
  type SprintResource,
  type WatchFrame,
  type PresenceResource,
} from "./api/resources";
import type { Board, Card as CardModel, Provider } from "./providers/types";
import { mergeNotes } from "./notes";

const errMessage = (err: unknown) =>
  err instanceof Error ? err.message : String(err);

// mergeCardLists flattens a view's lists (e.g. the Team grid + its weekly
// plan), deduping by item id; board order within each list is preserved.
export function mergeCardLists(lists: CardModel[][]): CardModel[] {
  const seen = new Set<string>();
  return lists.flat().filter((c) => {
    if (seen.has(c.itemId)) {
      return false;
    }
    seen.add(c.itemId);
    return true;
  });
}

export interface BoardData {
  board: Board | null;
  setBoard: React.Dispatch<React.SetStateAction<Board | null>>;
  /** Imperatively load a board (identity + sprints + the active view's cards). */
  load: (owner: string, number: number) => Promise<void>;
  reload: () => void;
  patchCard: (
    itemId: string,
    patch: Partial<CardModel> | ((c: CardModel) => Partial<CardModel>),
  ) => void;
  addCard: (card: CardModel) => void;
  replaceCard: (itemId: string, card: CardModel) => void;
  removeCard: (itemId: string) => void;
  reorderCards: (orderedIds: string[]) => void;
  /** Live selections of other users (login -> card uid). */
  presence: Record<string, string>;
  /** Write-behind queue depth reported by this board's watch. */
  pendingSync: number;
  /** Drop all state (used when the personal board is detached). */
  reset: () => void;
}

export interface UseBoardDataOpts {
  provider: Provider;
  /** LIST selectors of the active view; refetched when queriesKey changes. */
  queries: Record<string, string>[];
  queriesKey: string;
  /** Scoped-watch selector (serialised); the socket resubscribes on change. */
  watchKey: string;
  onError: (message: string) => void;
  /** Wrap slow loads so the app's progress bar covers them. */
  beginLoad: () => void;
  endLoad: () => void;
}

export function useBoardData({
  provider,
  queries,
  queriesKey,
  watchKey,
  onError,
  beginLoad,
  endLoad,
}: UseBoardDataOpts): BoardData {
  const [board, setBoard] = useState<Board | null>(null);
  const [presence, setPresenceMap] = useState<Record<string, string>>({});

  // Queue-depth badge, debounced: frames arrive on every enqueue/drain step —
  // a rapid burst of edits would re-render the app dozens of times — so the
  // badge updates at most a few times a second (trailing debounce keeps the
  // final value accurate).
  const [pendingSync, setPendingSync] = useState(0);
  const pendingSyncNext = useRef(0);
  const pendingSyncTimer = useRef<number | null>(null);
  const queuePendingSync = useCallback((n: number) => {
    pendingSyncNext.current = n;
    if (pendingSyncTimer.current !== null) {
      return;
    }
    pendingSyncTimer.current = window.setTimeout(() => {
      pendingSyncTimer.current = null;
      setPendingSync(pendingSyncNext.current);
    }, 300);
  }, []);

  // patch is a plain partial, or an updater computing one from the card's
  // CURRENT state — rapid successive changes (notes typed Enter-Enter-Enter)
  // must not base themselves on a stale render's copy.
  const patchCard = useCallback(
    (
      itemId: string,
      patch: Partial<CardModel> | ((c: CardModel) => Partial<CardModel>),
    ) => {
      setBoard((cur) => {
        if (!cur) {
          return cur;
        }
        // An empty patch (an updater deciding nothing changed) keeps the
        // exact same state object, so React bails out of the re-render.
        let changed = false;
        const cards = cur.cards.map((c) => {
          if (c.itemId !== itemId) {
            return c;
          }
          const p = typeof patch === "function" ? patch(c) : patch;
          if (!p || Object.keys(p).length === 0) {
            return c;
          }
          changed = true;
          return { ...c, ...p };
        });
        return changed ? { ...cur, cards } : cur;
      });
    },
    [],
  );

  // addCard upserts by item id: the watch stream may deliver a created card
  // before (or after) the creator's own response lands, so both paths must
  // converge on a single copy instead of appending twice. Locally-loaded notes
  // survive the upsert — Card resources never carry notes (they live in the
  // notes subresource).
  const addCard = useCallback((card: CardModel) => {
    setBoard((cur) => {
      if (!cur) {
        return cur;
      }
      const exists = cur.cards.some((c) => c.itemId === card.itemId);
      return {
        ...cur,
        cards: exists
          ? cur.cards.map((c) =>
              c.itemId === card.itemId
                ? { ...card, notes: card.notes ?? c.notes }
                : c,
            )
          : [...cur.cards, card],
      };
    });
  }, []);

  // Swap an optimistic card for its server twin IN PLACE, so a burst of
  // creates keeps its visual order (append-on-ack would reshuffle them).
  const replaceCard = useCallback((itemId: string, card: CardModel) => {
    setBoard((cur) => {
      if (!cur) {
        return cur;
      }
      if (!cur.cards.some((c) => c.itemId === itemId)) {
        const exists = cur.cards.some((c) => c.itemId === card.itemId);
        return exists ? cur : { ...cur, cards: [...cur.cards, card] };
      }
      return {
        ...cur,
        cards: cur.cards
          .filter((c) => c.itemId !== card.itemId || c.itemId === itemId)
          .map((c) => {
            if (c.itemId === itemId) {
              return card;
            }
            // Optimistic subtasks created under the tmp id follow the swap,
            // or they would orphan (and their rows flicker away) until their
            // own acks land.
            if (c.parent === itemId) {
              return { ...c, parent: card.itemId };
            }
            return c;
          }),
      };
    });
  }, []);

  const removeCard = useCallback((itemId: string) => {
    setBoard((cur) =>
      cur ? { ...cur, cards: cur.cards.filter((c) => c.itemId !== itemId) } : cur,
    );
  }, []);

  // Reorder board.cards to match orderedIds. Cards whose ids are not listed
  // keep their relative order and are appended after the ordered ones.
  const reorderCards = useCallback((orderedIds: string[]) => {
    setBoard((cur) => {
      if (!cur) {
        return cur;
      }
      const rank = new Map(orderedIds.map((id, i) => [id, i]));
      const ranked = cur.cards
        .filter((c) => rank.has(c.itemId))
        .sort((a, b) => rank.get(a.itemId)! - rank.get(b.itemId)!);
      const rest = cur.cards.filter((c) => !rank.has(c.itemId));
      return { ...cur, cards: [...ranked, ...rest] };
    });
  }, []);

  const queriesRef = useRef(queries);
  queriesRef.current = queries;

  const load = useCallback(
    async (owner: string, number: number) => {
      beginLoad();
      try {
        // Identity + sprints AND the active view's cards together, swapped in
        // as one board: a reload() must never leave the board empty while the
        // cards are still in flight (loadBoard itself carries no cards).
        const addr = { owner, number };
        const [loaded, lists] = await Promise.all([
          provider.loadBoard(owner, number),
          Promise.all(queriesRef.current.map((q) => provider.listCards(addr, q))),
        ]);
        setBoard({ ...loaded, cards: mergeCardLists(lists) });
      } finally {
        endLoad();
      }
    },
    [provider, beginLoad, endLoad],
  );

  const boardRef = useRef(board);
  boardRef.current = board;
  const reload = useCallback(() => {
    const cur = boardRef.current;
    if (cur) {
      load(cur.owner, cur.number).catch((err: unknown) =>
        onError(errMessage(err)),
      );
    }
  }, [load, onError]);

  const reset = useCallback(() => {
    setBoard(null);
    setPresenceMap({});
  }, []);

  // Load the active view's cards whenever the selection (view/day/teams)
  // changes on an already-loaded board. loadBoard brings only identity +
  // sprints; the cards for one view arrive here, so the UI holds just what it
  // shows.
  const bOwner = board?.owner;
  const bNumber = board?.number;
  useEffect(() => {
    if (!bOwner || bNumber == null) {
      return;
    }
    let cancelled = false;
    const addr = { owner: bOwner, number: bNumber };
    beginLoad();
    Promise.all(queriesRef.current.map((q) => provider.listCards(addr, q)))
      .then((lists) => {
        if (cancelled) {
          return;
        }
        setBoard((cur) => (cur ? { ...cur, cards: mergeCardLists(lists) } : cur));
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          onError(errMessage(err));
        }
      })
      .finally(endLoad);
    return () => {
      cancelled = true;
    };
  }, [bOwner, bNumber, queriesKey, provider, beginLoad, endLoad, onError]);

  // Live updates: the server pushes resource events over a scoped WebSocket
  // watch (Kubernetes style). We LIST via load, then mirror the ADDED /
  // MODIFIED / DELETED frames: Card frames upsert/remove by uid, Sprint frames
  // update the team's pointer, Ordering frames re-sort the local cards. On a
  // socket drop we re-LIST after reconnecting to reconcile anything missed.
  // Refs keep the socket from being rebuilt on every board change.
  const reloadRef = useRef(reload);
  reloadRef.current = reload;
  const boardLoaded = board !== null;
  useEffect(() => {
    if (!boardLoaded || !bOwner || bNumber == null) {
      return;
    }
    let socket: WebSocket | null = null;
    let closed = false;
    let retry: number | undefined;
    const applyFrame = (frame: WatchFrame) => {
      // Sync marks a finished server-side reload; the diff already arrived as
      // ordinary events, so there is nothing left to do here.
      if (frame.kind === "Sync") {
        return;
      }
      // The write-behind queue's depth: changes applied everywhere but not
      // yet confirmed by GitHub.
      if (frame.kind === "Queue") {
        queuePendingSync((frame.object as { pending?: number })?.pending ?? 0);
        return;
      }
      // A write GitHub finally rejected: the board has been rolled back to
      // the server's reloaded state; surface what was lost.
      if (frame.kind === "SyncError") {
        const msg = (frame.object as { message?: string })?.message;
        onError(msg || "a change could not be written to GitHub");
        return;
      }
      if (!frame.object) {
        return;
      }
      if (frame.kind === "Card") {
        const card = resourceToCard(frame.object as CardResource);
        if (frame.type === "DELETED") {
          removeCard(card.itemId);
          return;
        }
        // Note/event changes arrive as plain card changes, and the resource
        // carries neither: refetch the log when this card's was already loaded.
        const existing = boardRef.current?.cards.find(
          (c) => c.itemId === card.itemId,
        );
        addCard(card);
        if (existing?.notes !== undefined) {
          void provider
            .listLog({ owner: bOwner, number: bNumber }, card.itemId)
            .then(({ notes, events }) =>
              // Merge, don't replace: the response may predate a local
              // optimistic note added while it was in flight.
              patchCard(card.itemId, (c) => ({
                notes: mergeNotes(notes, c.notes),
                events,
              })),
            )
            .catch(() => {});
        }
        return;
      }
      if (frame.kind === "Sprint") {
        const sprint = frame.object as SprintResource;
        setBoard((cur) =>
          cur
            ? {
                ...cur,
                sprintStates: {
                  ...cur.sprintStates,
                  [sprint.metadata.team ?? ""]: sprintStateFrom(sprint),
                },
              }
            : cur,
        );
        return;
      }
      if (frame.kind === "Ordering") {
        reorderCards((frame.object as OrderingResource).spec.uids);
        return;
      }
      if (frame.kind === "Presence") {
        const { login, card } = frame.object as PresenceResource;
        if (!login) {
          return;
        }
        setPresenceMap((cur) => {
          const next = { ...cur };
          if (card) {
            next[login] = card;
          } else {
            delete next[login];
          }
          return next;
        });
      }
    };
    const connect = () => {
      // A fresh connection replays the presence and queue snapshots; drop
      // stale marks (the queue may have drained while we were away).
      setPresenceMap({});
      queuePendingSync(0);
      const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
      // Scope the watch to the active view. ?client= keeps our own mutations
      // from echoing back. Re-subscribes when watchKey changes (a dep below).
      const url = `${proto}//${window.location.host}/api/v1/watch?owner=${encodeURIComponent(
        bOwner,
      )}&project=${bNumber}&client=${clientId}&${watchKey}`;
      socket = new WebSocket(url);
      socket.addEventListener("message", (e) => {
        let frame: WatchFrame;
        try {
          frame = JSON.parse(e.data as string) as WatchFrame;
        } catch {
          return;
        }
        applyFrame(frame);
      });
      socket.addEventListener("close", () => {
        if (closed) {
          return;
        }
        retry = window.setTimeout(() => {
          reloadRef.current();
          connect();
        }, 3000);
      });
    };
    connect();
    return () => {
      closed = true;
      window.clearTimeout(retry);
      socket?.close();
    };
  }, [
    boardLoaded,
    bOwner,
    bNumber,
    watchKey,
    provider,
    addCard,
    removeCard,
    patchCard,
    reorderCards,
    queuePendingSync,
    onError,
  ]);

  return {
    board,
    setBoard,
    load,
    reload,
    patchCard,
    addCard,
    replaceCard,
    removeCard,
    reorderCards,
    presence,
    pendingSync,
    reset,
  };
}
