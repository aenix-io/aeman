import { describe, expect, it } from "vitest";

import {
  attachSlotDates,
  attachTargets,
  mirrorTargets,
  removeFromProjectOutcome,
} from "./placements";
import type { Card } from "./providers/types";

// The picker offers only columns the server would accept: projects of the
// card's own repository, each with its epics — a pair the server refuses
// has no business being clickable.
describe("attach and mirror targets", () => {
  const projects = ["engineering", "freedom", "strategy"];
  const epics = [
    { name: "Cozystack", project: "engineering" },
    { name: "Ingress", project: "engineering" },
    { name: "Launch", project: "freedom" },
    { name: "Fundraising", project: "strategy" },
  ];
  const projectDomains = { strategy: "founders" };

  it("offers the projects of the card's repository, with their epics", () => {
    const got = attachTargets(projects, epics, projectDomains, "");
    expect(got).toEqual([
      { name: "engineering", epics: ["Cozystack", "Ingress"] },
      { name: "freedom", epics: ["Launch"] },
    ]);
    // A card living in the founders repository sees only founders projects.
    expect(attachTargets(projects, epics, projectDomains, "founders")).toEqual([
      { name: "strategy", epics: ["Fundraising"] },
    ]);
  });

  it("offers everything on a board that names no domains", () => {
    expect(attachTargets(projects, epics, undefined, "").map((p) => p.name)).toEqual(projects);
  });

  it("mirror targets follow the HOME project's repository and skip where the card stands", () => {
    const card = {
      project: "engineering",
      epic: "Cozystack",
      mirrors: [{ project: "freedom", epic: "Launch" }],
    } as Card;
    const got = mirrorTargets(card, projects, epics, projectDomains);
    // Cozystack is the home, Launch is already mirrored: only Ingress is left.
    expect(got).toEqual([{ name: "engineering", epics: ["Ingress"] }]);
  });
});

// Attaching a weekly-plan card gives it the slot of the week it was taken
// from: start on its Monday, end on its band's day — the same dates the
// server writes, so the optimistic card does not jump on the re-list.
describe("attachSlotDates", () => {
  it("spans to Friday for a by-Friday card and Wednesday for a by-Wednesday one", () => {
    expect(attachSlotDates("fri", "2026-08-24")).toEqual({
      startDate: "2026-08-24",
      day: "2026-08-28",
    });
    expect(attachSlotDates("wed", "2026-08-24")).toEqual({
      startDate: "2026-08-24",
      day: "2026-08-26",
    });
  });
});

// The Project board's × means four different things, and the UI must know
// which before it asks anything: a mirror goes silently, the home hands
// over silently, an orphan survives, and only the delete is worth a
// question.
describe("removeFromProjectOutcome", () => {
  const base = { project: "engineering", epic: "Cozystack" } as Card;

  it("is unmirror on a mirror placement", () => {
    const c = { ...base, mirrors: [{ project: "freedom", epic: "Launch" }] } as Card;
    expect(removeFromProjectOutcome(c, "freedom", "Launch")).toBe("unmirror");
  });

  it("is promote on the home while mirrors remain", () => {
    const c = { ...base, mirrors: [{ project: "freedom", epic: "Launch" }] } as Card;
    expect(removeFromProjectOutcome(c, "engineering", "Cozystack")).toBe("promote");
  });

  it("is orphan on the last column of a worked card, delete otherwise", () => {
    const worked = { ...base, assignees: ["kvaps"], progress: 40 } as Card;
    expect(removeFromProjectOutcome(worked, "engineering", "Cozystack")).toBe("orphan");
    const idle = { ...base, assignees: [] } as Card;
    expect(removeFromProjectOutcome(idle, "engineering", "Cozystack")).toBe("delete");
    // Progress without a person, or a person without progress, is not
    // "worked" — the server deletes it, and the UI must ask first.
    expect(
      removeFromProjectOutcome({ ...base, assignees: [], progress: 40 } as Card, "engineering", "Cozystack"),
    ).toBe("delete");
  });
});
