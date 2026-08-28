import { describe, expect, it } from "vitest";

import { dayFeedUpdates, splitDayLogs } from "./daylog";

describe("splitDayLogs", () => {
  it("splits a card's wire entries into notes and events, mapping the note fields", () => {
    expect(
      splitDayLogs({
        c1: [
          {
            type: "event",
            id: "e1",
            at: "2026-08-28T09:00:00Z",
            actor: "ann",
            kind: "progress",
            from: "10",
            to: "40",
          },
          {
            type: "note",
            id: "n1",
            at: "2026-08-28T10:00:00Z",
            actor: "ann",
            text: "shipped the draft",
          },
        ],
      }),
    ).toEqual({
      c1: {
        notes: [
          {
            id: "n1",
            body: "shipped the draft",
            createdAt: "2026-08-28T10:00:00Z",
            author: "ann",
            source: "comment",
          },
        ],
        events: [
          {
            id: "e1",
            kind: "progress",
            actor: "ann",
            from: "10",
            to: "40",
            at: "2026-08-28T09:00:00Z",
          },
        ],
      },
    });
  });

  it("keeps the server's (oldest-first) order within notes and within events", () => {
    const { c1 } = splitDayLogs({
      c1: [
        { type: "note", id: "n1", at: "t1", text: "first" },
        { type: "event", id: "e1", at: "t2", kind: "created" },
        { type: "note", id: "n2", at: "t3", text: "second" },
        { type: "event", id: "e2", at: "t4", kind: "progress" },
      ],
    });
    expect(c1.notes.map((n) => n.id)).toEqual(["n1", "n2"]);
    expect(c1.events.map((e) => e.id)).toEqual(["e1", "e2"]);
  });

  it("ignores an entry of an unknown type — a newer server may add kinds this build does not know", () => {
    expect(
      splitDayLogs({
        c1: [
          { type: "hologram", id: "x1", at: "t" },
          { type: "note", id: "n1", at: "t", text: "kept" },
        ],
      }),
    ).toEqual({
      c1: {
        notes: [{ id: "n1", body: "kept", createdAt: "t", author: undefined, source: "comment" }],
        events: [],
      },
    });
  });

  it("keeps a quiet card as an empty entry — 'nothing that day' is an answer, not a gap", () => {
    expect(splitDayLogs({ c1: [] })).toEqual({ c1: { notes: [], events: [] } });
    expect(splitDayLogs({ c1: null })).toEqual({ c1: { notes: [], events: [] } });
  });

  it("does not invent a card the response left out (one the visitor cannot see)", () => {
    expect(Object.keys(splitDayLogs({ c1: [] }))).toEqual(["c1"]);
  });

  it("answers empty for a missing or null map", () => {
    expect(splitDayLogs(null)).toEqual({});
    expect(splitDayLogs(undefined)).toEqual({});
  });

  it("fills the blanks of a sparse entry rather than passing undefined through", () => {
    expect(splitDayLogs({ c1: [{ type: "event", id: "e1" }, { type: "note", id: "n1" }] })).toEqual(
      {
        c1: {
          notes: [{ id: "n1", body: "", createdAt: "", author: undefined, source: "comment" }],
          events: [{ id: "e1", kind: "", actor: undefined, from: undefined, to: undefined, at: "" }],
        },
      },
    );
  });
});

describe("dayFeedUpdates", () => {
  const inFeed = new Set(["a", "b"]);

  it("coalesces a burst to one refresh per card, keeping first-seen order", () => {
    expect(
      dayFeedUpdates(
        [
          { uid: "a", deleted: false },
          { uid: "b", deleted: false },
          { uid: "a", deleted: false },
        ],
        inFeed,
      ),
    ).toEqual({ drop: [], refresh: ["a", "b"] });
  });

  it("ignores frames for cards outside the feed set", () => {
    expect(dayFeedUpdates([{ uid: "zzz", deleted: false }], inFeed)).toEqual({
      drop: [],
      refresh: [],
    });
  });

  it("turns a DELETED frame into a drop — even for a card already outside the set, so no stale entry stays", () => {
    expect(dayFeedUpdates([{ uid: "gone", deleted: true }], inFeed)).toEqual({
      drop: ["gone"],
      refresh: [],
    });
  });

  it("lets a card's last frame win: deleted-then-changed is a refresh, changed-then-deleted a drop", () => {
    expect(
      dayFeedUpdates(
        [
          { uid: "a", deleted: true },
          { uid: "a", deleted: false },
        ],
        inFeed,
      ),
    ).toEqual({ drop: [], refresh: ["a"] });
    expect(
      dayFeedUpdates(
        [
          { uid: "a", deleted: false },
          { uid: "a", deleted: true },
        ],
        inFeed,
      ),
    ).toEqual({ drop: ["a"], refresh: [] });
  });

  it("answers empty for an empty burst", () => {
    expect(dayFeedUpdates([], inFeed)).toEqual({ drop: [], refresh: [] });
  });
});
