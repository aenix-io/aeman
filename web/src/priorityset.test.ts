import { describe, expect, it, vi } from "vitest";

import { latestWins } from "./roadmap";
import { pressPriority } from "./priorityset";
import type { Card } from "./providers/types";

// Setting a card's OWN priority (K15) from wherever it is offered — the
// Roadmap badge and the card drawer share it: shown at once on the card and
// on everything that counts under it, settled by the server's answer, put
// back on a refusal, and never overwritten by an older answer.

const card = (over: Partial<Card>): Card => ({ itemId: "c", title: "a card", assignees: [], progress: 0, ...over }) as Card;
const e = card({ itemId: "e", type: "epic", title: "Billing" });
const t = card({ itemId: "t", parent: "e" });
const r = card({ itemId: "r", reviewOf: "t" });

function deps(setPriority: (uid: string, value: string) => Promise<Card>) {
  const patches: [string, Partial<Card>][] = [];
  return {
    patches,
    d: {
      cards: [e, t, r],
      patchCard: (id: string, p: Partial<Card>) => patches.push([id, p]),
      provider: { setPriority: vi.fn(setPriority) },
      seq: latestWins(),
      onError: vi.fn(),
      reload: vi.fn(),
    },
  };
}

describe("pressing a priority", () => {
  it("shows it at once on the card, what is under it and their reviews, then takes the server's answer", async () => {
    const { patches, d } = deps(async () => card({ itemId: "e", type: "epic", priority: "P1", effectivePriority: "P1" }));
    await pressPriority(d, e, "P1");
    expect(patches[0]).toEqual(["e", { priority: "P1" }]);
    const shown = new Map(patches.slice(1, -1));
    expect(shown.get("t")).toEqual({ effectivePriority: "P1", effectivePriorityFrom: { uid: "e", type: "epic", title: "Billing" } });
    expect(shown.get("r")?.effectivePriority).toBe("P1");
    expect(patches.at(-1)).toEqual(["e", { priority: "P1", effectivePriority: "P1", effectivePriorityFrom: undefined }]);
  });

  it("puts everything back and reloads on a refusal", async () => {
    const { patches, d } = deps(async () => {
      throw new Error("refused");
    });
    await pressPriority(d, e, "P0");
    expect(d.onError).toHaveBeenCalledWith("refused");
    expect(d.reload).toHaveBeenCalled();
    const back = new Map(patches.slice(-3));
    expect(back.get("e")).toEqual({ priority: undefined, effectivePriority: undefined, effectivePriorityFrom: undefined });
  });

  it("ignores an older answer that arrives after a newer press", async () => {
    let release: (c: Card) => void = () => {};
    const slow = new Promise<Card>((res) => (release = res));
    const { patches, d } = deps(() => slow);
    const first = pressPriority(d, e, "P1");
    d.provider.setPriority.mockImplementation(async () => card({ itemId: "e", priority: "P2", effectivePriority: "P2" }));
    await pressPriority(d, e, "P2");
    release(card({ itemId: "e", priority: "P1", effectivePriority: "P1" }));
    await first;
    expect(patches.at(-1)).toEqual(["e", { priority: "P2", effectivePriority: "P2", effectivePriorityFrom: undefined }]);
  });
});
