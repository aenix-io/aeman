import { describe, expect, it } from "vitest";

import { slotMenuItems } from "./projectmenu";

// The project card's "…" menu: what it offers and what it does not.

describe("a project card's menu", () => {
  it("opens, attaches, marks done and deletes — and nothing takes the card out of its project", () => {
    const keys = slotMenuItems({ progress: 0 }).map((i) => i.key);
    expect(keys).toEqual(["open", "attach", "done", "delete"]);
    expect(keys).not.toContain("remove");
    expect(slotMenuItems({ progress: 0 }).map((i) => i.label)).not.toContain("Remove from project");
  });

  it("deletes at once from the menu: the pick is the confirmation", () => {
    const del = slotMenuItems({ progress: 40 }).find((i) => i.key === "delete")!;
    expect(del).toEqual({ key: "delete", label: "Delete", asks: false });
  });

  it("offers the way back on a finished card", () => {
    expect(slotMenuItems({ stage: "done" }).find((i) => i.key === "done")).toEqual({ key: "done", label: "Reopen", reopen: true });
    expect(slotMenuItems({ progress: 10 }).find((i) => i.key === "done")).toEqual({ key: "done", label: "Mark as done", reopen: false });
  });
});
