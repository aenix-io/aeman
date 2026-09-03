import { describe, expect, it } from "vitest";

import {
  ME_ONLY_STAGES,
  STAGES,
  STAGE_ORDER,
  barColor,
  clampProgress,
  clampsProgress,
  isComplete,
  isInProgress,
  isWorkable,
} from "./stages";
import type { StageKey } from "./providers/types";

// The stages are mirrored from pkg/board/stages.go and status.go, and until
// now the Go side had all the tests: ClampProgress on both edges, Workable,
// Complete, ApplyStage, StageFromName. The TS copies had none — and it is the
// TS copies the board draws with, so a rule added to one and not the other
// shows as a bar in the wrong place or a handle that snaps back.

describe("the stages the board has", () => {
  it("names every stage it orders", () => {
    for (const stage of STAGE_ORDER) {
      expect(STAGES[stage]?.key).toBe(stage);
    }
  });

  // Mirrors board.StageOrder. A stage in one list and not the other is how a
  // card comes back from the server wearing something this board cannot draw.
  it("holds exactly the five the domain has", () => {
    expect(STAGE_ORDER).toEqual(["locked", "review", "recurrent", "refuse", "done"]);
  });

  it("gives every stage a colour, and unstaged work the default", () => {
    for (const stage of STAGE_ORDER) {
      expect(barColor(stage)).toBeTruthy();
    }
    expect(barColor(undefined)).toBe(barColor(undefined));
    expect(barColor("refuse")).not.toBe(barColor("done"));
  });
});

// Work parked on somebody ELSE's answer — a reviewer's, whoever locked it, the
// lead's — is neither untouched nor finished, and a bar at either end would
// read as one of those two. The three stages that wait on someone hold the
// band; the rest span the full range.
describe("the stages that hold the 10–90 band", () => {
  const clamped: StageKey[] = ["locked", "review", "refuse"];
  const free: (StageKey | undefined)[] = ["recurrent", "done", undefined];

  it("clamps the three that wait on somebody", () => {
    for (const stage of clamped) {
      expect(clampsProgress(stage)).toBe(true);
    }
  });

  it("leaves the rest alone", () => {
    for (const stage of free) {
      expect(clampsProgress(stage)).toBe(false);
    }
  });

  // Both edges, which is the whole of the rule: a hand-written copy that knew
  // only the top edge is what let a 0% card sent to review show a 0 the
  // server does not have.
  it("holds both edges, not just the top", () => {
    for (const stage of clamped) {
      expect(clampProgress(stage, 0)).toBe(10);
      expect(clampProgress(stage, 100)).toBe(90);
      // Inside the band nothing moves.
      expect(clampProgress(stage, 40)).toBe(40);
      expect(clampProgress(stage, 10)).toBe(10);
      expect(clampProgress(stage, 90)).toBe(90);
    }
  });

  it("touches nothing on a stage that does not hold the band", () => {
    expect(clampProgress("recurrent", 0)).toBe(0);
    expect(clampProgress("recurrent", 100)).toBe(100);
    expect(clampProgress(undefined, 0)).toBe(0);
    expect(clampProgress(undefined, 100)).toBe(100);
  });
});

// REFUSE is the answer of the person the card is on, and of nobody else, so
// it is the one stage a board offers only on that person's own view.
describe("the stage only its owner may set", () => {
  it("is refuse, and only refuse", () => {
    expect(ME_ONLY_STAGES).toEqual(["refuse"]);
  });
});

// Done is DERIVED, never stored: a finished card is 100% with no stage.
describe("what counts as finished, and as being worked on", () => {
  it("calls a stageless 100 done", () => {
    expect(isComplete({ stage: undefined, progress: 100 })).toBe(true);
  });

  it("does not call a card on a stage done, whatever its progress says", () => {
    // A card cannot reach 100 on these through the service — the band stops
    // it — but a direct repository write can, and it is still not finished:
    // it is waiting on somebody.
    expect(isComplete({ stage: "review", progress: 100 })).toBe(false);
    expect(isComplete({ stage: "locked", progress: 100 })).toBe(false);
    expect(isComplete({ stage: "refuse", progress: 100 })).toBe(false);
  });

  it("is in progress between the edges, with no stage at all", () => {
    expect(isInProgress({ stage: undefined, progress: 10 })).toBe(true);
    expect(isInProgress({ stage: undefined, progress: 90 })).toBe(true);
    expect(isInProgress({ stage: undefined, progress: 9 })).toBe(false);
    expect(isInProgress({ stage: undefined, progress: 91 })).toBe(false);
    // A stage of its own says what the card is doing; this status is the
    // absence of one.
    expect(isInProgress({ stage: "review", progress: 40 })).toBe(false);
  });
});

// Workable is what somebody can pick up now. A refused card is not: it waits
// on the lead to answer it, which is the whole point of refusing rather than
// deleting.
describe("what work can be picked up", () => {
  it("excludes the stages that wait on somebody else", () => {
    expect(isWorkable({ stage: "review", progress: 40 })).toBe(false);
    expect(isWorkable({ stage: "locked", progress: 40 })).toBe(false);
    expect(isWorkable({ stage: "refuse", progress: 40 })).toBe(false);
  });

  it("includes ordinary work, finished or not", () => {
    expect(isWorkable({ stage: undefined, progress: 40 })).toBe(true);
  });
});
