import { describe, expect, it, vi } from "vitest";

import { openWatches, type SocketLike } from "./watch";
import type { WatchFrame } from "./api/resources";

// G85 — a watch that fell behind is ended by the server with one ERROR frame
// (a Status of code 410, reason Expired) and nothing after it. The board
// answers it the way it answers a dropped socket, only at once: it re-LISTs
// its view and opens its watches again — no page reload, so the drawer and
// the selection stay as they are. Any other frame is applied; a socket that
// drops on its own is rebuilt after a pause, with a re-LIST.

class FakeSocket implements SocketLike {
  static all: FakeSocket[] = [];
  closed = false;
  private listeners: Record<string, ((e: { data?: unknown }) => void)[]> = {};
  constructor(public url: string) {
    FakeSocket.all.push(this);
  }
  addEventListener(type: "message" | "close", fn: (e: { data?: unknown }) => void) {
    (this.listeners[type] ??= []).push(fn);
  }
  close() {
    if (!this.closed) {
      this.closed = true;
      this.emit("close", {});
    }
  }
  emit(type: string, e: { data?: unknown }) {
    for (const fn of this.listeners[type] ?? []) {
      fn(e);
    }
  }
  send(frame: unknown) {
    this.emit("message", { data: JSON.stringify(frame) });
  }
}

const expired = {
  type: "ERROR",
  kind: "Status",
  object: { kind: "Status", status: "Failure", reason: "Expired", code: 410, message: "re-list" },
};

function open(extra: Partial<Parameters<typeof openWatches>[0]> = {}) {
  FakeSocket.all = [];
  const frames: WatchFrame[] = [];
  const relist = vi.fn();
  const connected = vi.fn();
  const stop = openWatches({
    urls: ["ws://x/a", "ws://x/b"],
    onFrame: (f) => frames.push(f),
    relist,
    onConnect: connected,
    retryMs: 3000,
    makeSocket: (url) => new FakeSocket(url),
    ...extra,
  });
  return { frames, relist, connected, stop };
}

describe("the board's watches", () => {
  it("apply every frame but the 410", () => {
    const { frames, relist } = open();
    FakeSocket.all[0].send({ type: "MODIFIED", kind: "Ordering", object: { spec: { uids: [] } } });
    expect(frames).toHaveLength(1);
    expect(relist).not.toHaveBeenCalled();
  });

  it("re-list and re-open at once on the 410, without applying it", () => {
    vi.useFakeTimers();
    const { frames, relist, connected } = open();
    const first = [...FakeSocket.all];
    first[1].send(expired);
    expect(frames).toHaveLength(0);
    expect(relist).toHaveBeenCalledTimes(1);
    expect(first.every((s) => s.closed)).toBe(true);
    expect(FakeSocket.all).toHaveLength(4);
    expect(connected).toHaveBeenCalledTimes(2);
    // The server's own close that follows the 410 schedules nothing more.
    vi.advanceTimersByTime(10_000);
    expect(relist).toHaveBeenCalledTimes(1);
    expect(FakeSocket.all).toHaveLength(4);
    vi.useRealTimers();
  });

  it("rebuild after a pause, with a re-list, when a socket drops on its own", () => {
    vi.useFakeTimers();
    const { relist } = open();
    FakeSocket.all[0].close();
    expect(relist).not.toHaveBeenCalled();
    vi.advanceTimersByTime(3000);
    expect(relist).toHaveBeenCalledTimes(1);
    expect(FakeSocket.all).toHaveLength(4);
    vi.useRealTimers();
  });

  it("stop for good when torn down", () => {
    vi.useFakeTimers();
    const { relist, stop } = open();
    stop();
    vi.advanceTimersByTime(10_000);
    expect(relist).not.toHaveBeenCalled();
    expect(FakeSocket.all).toHaveLength(2);
    vi.useRealTimers();
  });
});

// A board that keeps falling behind — a burst the server cannot get out
// before the next one — must not re-list in a tight loop: the first 410 is
// answered at once, each one after it within a short life waits longer, with
// jitter so many tabs do not come back in step, up to a ceiling; a watch that
// lived a while before its 410 starts over.
describe("the board's answer to repeated 410s", () => {
  it("backs off, with jitter, and starts over after a healthy watch", () => {
    vi.useFakeTimers();
    const { relist } = open({ random: () => 0.5, healthyMs: 30_000 });
    const latest = () => FakeSocket.all[FakeSocket.all.length - 1];
    latest().send(expired);
    expect(relist).toHaveBeenCalledTimes(1); // the first: at once
    latest().send(expired);
    expect(relist).toHaveBeenCalledTimes(1); // the second waits
    vi.advanceTimersByTime(749);
    expect(relist).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(1); // 1 s × (0.5 + 0.5/2)
    expect(relist).toHaveBeenCalledTimes(2);
    latest().send(expired);
    vi.advanceTimersByTime(1499);
    expect(relist).toHaveBeenCalledTimes(2);
    vi.advanceTimersByTime(1); // twice as long
    expect(relist).toHaveBeenCalledTimes(3);
    // A watch that lived past healthyMs: the next 410 is answered at once.
    vi.advanceTimersByTime(30_000);
    latest().send(expired);
    expect(relist).toHaveBeenCalledTimes(4);
    vi.useRealTimers();
  });

  it("never waits past the ceiling", () => {
    vi.useFakeTimers();
    const { relist } = open({ random: () => 1, healthyMs: 1e12, maxBackoffMs: 5000 });
    const latest = () => FakeSocket.all[FakeSocket.all.length - 1];
    for (let i = 1; i <= 8; i++) {
      latest().send(expired);
      vi.advanceTimersByTime(5000);
      expect(relist).toHaveBeenCalledTimes(i);
    }
    vi.useRealTimers();
  });
});
