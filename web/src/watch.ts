// The board's watch sockets: one per selector, kept in step. The server ends
// a watch that fell behind with one ERROR frame — a Status of code 410,
// reason Expired (G84) — and nothing after it; that is answered at once the
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
  const connect = () => {
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
          rebuild();
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
