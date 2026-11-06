// The board's watch sockets: one per selector, kept in step. The server ends
// a watch that fell behind with one ERROR frame — a Status of code 410,
// reason Expired (G85) — and nothing after it; that is answered at once the
// way a dropped socket is answered after a pause: re-LIST the view and open
// the watches again. No page reload: the drawer and the selection stay.
import type { WatchFrame } from "./api/resources";

/** SocketLike is the part of a WebSocket the watches use. */
export interface SocketLike {
  addEventListener(type: "message" | "close", fn: (e: { data?: unknown }) => void): void;
  close(): void;
}

export interface WatchOptions {
  urls: readonly string[];
  /** Applies one frame; the 410 never reaches it. */
  onFrame: (frame: WatchFrame) => void;
  /** Re-LISTs the view: what the client holds may be out of date. */
  relist: () => void;
  /** Runs before every (re)connection, the first included. */
  onConnect?: () => void;
  /** The pause before a dropped socket is rebuilt. */
  retryMs?: number;
  makeSocket?: (url: string) => SocketLike;
  /** A watch that lived this long before its 410 was healthy: the next 410
   *  is answered at once again. */
  healthyMs?: number;
  /** The longest wait before answering a 410. */
  maxBackoffMs?: number;
  /** Jitter's source, [0, 1). */
  random?: () => number;
}

/** expiredDelay is the wait before answering the streak-th 410 in a row:
 *  none for the first, then 1 s doubling up to the ceiling, each scaled by a
 *  jitter in [0.5, 1) so many tabs do not come back in step. */
export function expiredDelay(streak: number, maxMs: number, random: () => number): number {
  if (streak <= 1) {
    return 0;
  }
  const base = Math.min(1000 * 2 ** (streak - 2), maxMs);
  return Math.min(base * (0.5 + random() / 2), maxMs);
}

/** isExpired is the server's "you fell behind, re-list" frame. */
export function isExpired(frame: WatchFrame): boolean {
  return frame.type === "ERROR" && frame.kind === "Status";
}

/** openWatches opens the sockets and keeps them open until the returned
 *  function tears them down. Any socket dropping rebuilds the whole set
 *  after a re-LIST, so they stay in step. */
export function openWatches(opts: WatchOptions): () => void {
  const make = opts.makeSocket ?? ((url: string) => new WebSocket(url) as unknown as SocketLike);
  const retryMs = opts.retryMs ?? 3000;
  const healthyMs = opts.healthyMs ?? 30_000;
  const maxBackoffMs = opts.maxBackoffMs ?? 30_000;
  const random = opts.random ?? Math.random;
  // 410s in a row, each from a watch that died young (G84).
  let streak = 0;
  let openedAt = Date.now();
  const sockets: SocketLike[] = [];
  let stopped = false;
  let retry: ReturnType<typeof setTimeout> | undefined;
  const closeAll = () => {
    for (const s of sockets.splice(0)) {
      s.close();
    }
  };
  const rebuild = () => {
    if (retry !== undefined) {
      clearTimeout(retry);
      retry = undefined;
    }
    closeAll();
    opts.relist();
    connect();
  };
  // A 410 is answered at once the first time; a board that keeps falling
  // behind waits longer each time before it re-lists, so it does not hammer
  // the server in a loop. The sockets are let go at once either way.
  const expired = () => {
    streak = Date.now() - openedAt >= healthyMs ? 1 : streak + 1;
    const wait = expiredDelay(streak, maxBackoffMs, random);
    if (wait === 0) {
      rebuild();
      return;
    }
    closeAll();
    if (retry !== undefined) {
      clearTimeout(retry);
    }
    retry = setTimeout(() => {
      retry = undefined;
      rebuild();
    }, wait);
  };
  const connect = () => {
    openedAt = Date.now();
    opts.onConnect?.();
    for (const url of opts.urls) {
      const socket = make(url);
      sockets.push(socket);
      socket.addEventListener("message", (e) => {
        if (!sockets.includes(socket)) {
          return;
        }
        let frame: WatchFrame;
        try {
          frame = JSON.parse(e.data as string) as WatchFrame;
        } catch {
          return;
        }
        if (isExpired(frame)) {
          expired();
          return;
        }
        opts.onFrame(frame);
      });
      socket.addEventListener("close", () => {
        // A socket we closed ourselves (teardown, or a rebuild) is no longer
        // in the set and must not schedule another rebuild.
        if (stopped || !sockets.includes(socket) || retry !== undefined) {
          return;
        }
        retry = setTimeout(() => {
          retry = undefined;
          rebuild();
        }, retryMs);
      });
    }
  };
  connect();
  return () => {
    stopped = true;
    if (retry !== undefined) {
      clearTimeout(retry);
    }
    closeAll();
  };
}
