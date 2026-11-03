import { describe, expect, it } from "vitest";

import {
  attachTargets,
  canBeFiledIntoAColumn,
  canCreateInColumn,
  columnsOf,
  markOf,
  drawnAsSlot,
  drawnOnProjectBoard,
  hasOriginToShow,
  makeCardPlacements,
  movingSlot,
  placementTargets,
  projectsAColumnCanJoin,
  projectsWithColumns,
  rosterOf,
  slotDragPlan,
  teamlessIsLawful,
  teamsACardCanTake,
  type CardPlacements,
} from "./placements";
import type { Card } from "./providers/types";
import { parkedLocally } from "./backlog";

// The picker offers only columns the server would accept: projects of the
// card's own repository, each with its scopes — a pair the server refuses
// has no business being clickable.
/** card builds the slice of a card these rules read, TYPED. A cast through
 *  `unknown` asserts nothing about the shape it hides, and these cases are
 *  the rules' documentation. */
const mk = (c: Partial<Card>): Card => c as Card;

describe("attach targets", () => {
  const projects = ["engineering", "freedom", "strategy"];
  // The repository rides on the COLUMN, the way the server records it: a
  // column declared in the closed repository carries it, whatever its
  // project is called.
  const scopes = [
    { name: "Cozystack", project: "engineering" },
    { name: "Ingress", project: "engineering" },
    { name: "Launch", project: "freedom" },
    { name: "Fundraising", project: "strategy", domain: "founders" },
  ];

  // The PROJECT carries the card into its repository and the COLUMN has to
  // be in that same one, so both stamps are part of the question — the
  // roster's for the project, the column's own for the column.
  const roster = {
    projectDomains: { strategy: "founders" },
    primary: "shared",
    single: false,
  };

  it("offers the projects of the card's repository, with their scopes", () => {
    const got = attachTargets(projects, scopes, "", true, roster);
    expect(got).toEqual([
      { name: "engineering", scopes: ["Cozystack", "Ingress"] },
      { name: "freedom", scopes: ["Launch"] },
    ]);
    // A card living in the founders repository sees only founders projects.
    expect(attachTargets(projects, scopes, "founders", true, roster)).toEqual([
      { name: "strategy", scopes: ["Fundraising"] },
    ]);
  });

  it("offers everything on a board that names no domains", () => {
    const single = scopes.map((e) => ({ name: e.name, project: e.project }));
    expect(attachTargets(projects, single, "").map((p) => p.name)).toEqual(projects);
  });

});

// The Project board's × means two different things, and the UI must know
// which before it asks anything: an orphan survives, and only the delete is
// worth a question.

// A card in a column is a PROJECT CARD and stands there alone (H4): the
// picker offers a column only to a card nothing holds — no team, no owner,
// no parent, no review — because the server refuses to file the rest.
// Mirrors boardservice.projectCardCannotCarry.
describe("canBeFiledIntoAColumn", () => {
  it("offers a column to a card nothing holds, and to no other", () => {
    expect(canBeFiledIntoAColumn(mk({}))).toBe(true);
    expect(canBeFiledIntoAColumn(mk({ assignees: [] }))).toBe(true);
    expect(canBeFiledIntoAColumn(mk({ team: "platform" }))).toBe(false);
    expect(canBeFiledIntoAColumn(mk({ assignees: ["kvaps"] }))).toBe(false);
    expect(canBeFiledIntoAColumn(mk({ parent: "p1" }))).toBe(false);
    expect(canBeFiledIntoAColumn(mk({ reviewOf: "c1" }))).toBe(false);
  });

  it("is what placementTargets asks first", () => {
    const board = {
      projects: ["engineering"],
      scopes: [{ name: "Cozystack", project: "engineering", domain: "" }],
    };
    expect(placementTargets(mk({ team: "platform" }), board).attach).toBeUndefined();
    expect(placementTargets(mk({ assignees: ["kvaps"] }), board).attach).toBeUndefined();
    expect(placementTargets(mk({}), board).attach).toEqual([
      { name: "engineering", scopes: ["Cozystack"] },
    ]);
  });

  // A project card has no subtasks (H4): a task that has pieces is not
  // offered a column, or the pieces would be subtasks of a card no day board
  // draws. Mirrors projectCardCannotCarry's subtask clause.
  it("offers no column to a task that has subtasks", () => {
    expect(canBeFiledIntoAColumn(mk({}), true)).toBe(false);
    const board = {
      projects: ["engineering"],
      scopes: [{ name: "Cozystack", project: "engineering", domain: "" }],
      cards: [mk({ itemId: "piece", parent: "parent", lineage: [{ uid: "parent", title: "a task" }] })],
    };
    expect(placementTargets(mk({ itemId: "parent" }), board).attach).toBeUndefined();
    expect(placementTargets(mk({ itemId: "lone" }), board).attach).toEqual([
      { name: "engineering", scopes: ["Cozystack"] },
    ]);
  });
});

// placementTargets is the dispatcher the boards call: which section a card
// gets is decided by what it is, not where it is rendered.
describe("placementTargets", () => {
  const board = {
    domains: [
      { name: "shared", writable: true, members: [] },
      { name: "closed", writable: true, members: [] },
    ],
    projects: ["engineering"],
    scopes: [{ name: "Cozystack", project: "engineering" }],
    projectDomains: undefined,
  };

  it("offers a card already in a column nothing: it is re-filed on the Project board", () => {
    const got = placementTargets(mk({ project: "engineering", scope: "Cozystack" }), board);
    expect(got.attach).toBeUndefined();
    // A no-project column is a column like any other.
    expect(placementTargets(mk({ scope: "Inbox" }), board).attach).toBeUndefined();
  });

  it("offers a SUBTASK nothing", () => {
    // A card in a column is nobody's subtask (H4): a subtask rides its
    // parent's team and person, and a project card has neither. The server
    // refuses the filing, so the picker does not open the door.
    expect(placementTargets(mk({ parent: "p1", stage: "recurrent" }), board).attach).toBeUndefined();
  });

  // A recurrent card is a card like any other here: it comes round by
  // carry-over, and a column is where it may be attached. (It used to be
  // offered a process tie instead; the Process feature is gone, H8.)
  it("offers projects to a recurrent card", () => {
    const got = placementTargets(mk({ stage: "recurrent" }), board);
    expect(got.attach).toEqual([{ name: "engineering", scopes: ["Cozystack"] }]);
  });

  it("offers projects to everything else", () => {
    const got = placementTargets(mk({}), board);
    expect(got.attach).toEqual([{ name: "engineering", scopes: ["Cozystack"] }]);
  });
});

// Dragging a slot: a vertical move inside its column is a date change and
// nothing else; a drop on another column re-files the card there.
describe("slotDragPlan", () => {
  const home = { project: "engineering", scope: "Cozystack" };
  const other = { project: "freedom", scope: "Launch" };

  it("a vertical move is a date change only", () => {
    expect(slotDragPlan(home, home)).toEqual({ kind: "dates" });
  });

  it("dragging into another column re-files the card", () => {
    expect(slotDragPlan(home, other)).toEqual({ kind: "refileHome" });
  });
});

// While a slot is dragged, only THAT slot dims: the question is asked of
// the slot's column as well as its card.
describe("movingSlot", () => {
  const move = {
    card: { itemId: "c1" },
    grabbed: { project: "freedom", scope: "Launch" },
  };
  it("dims exactly the grabbed placement", () => {
    expect(movingSlot(move, "c1", { project: "freedom", scope: "Launch" })).toBe(true);
    expect(movingSlot(move, "c1", { project: "engineering", scope: "Cozystack" })).toBe(false);
    expect(movingSlot(move, "c2", { project: "freedom", scope: "Launch" })).toBe(false);
    expect(movingSlot(null, "c1", { project: "freedom", scope: "Launch" })).toBe(false);
  });
});

// The attach patch is the column and nothing else: a task has no week whose
// slot it could take (K7), and a chosen schedule survives the move.
describe("makeCardPlacements onAttachProject", () => {
  const board = {
    domains: [
      { name: "shared", writable: true, members: [] },
      { name: "closed", writable: true, members: [] },
    ],
    projects: ["engineering", "freedom"],
    scopes: [
      { name: "Cozystack", project: "engineering" },
      { name: "Launch", project: "freedom" },
    ],
    projectDomains: undefined,
  };
  const run = (card: Card, act?: (p: CardPlacements) => void) => {
    const patches: Partial<Card>[] = [];
    const deps = {
      provider: {
        patchCard: () => Promise.resolve(undefined),
      },
      patchCard: (_: string, patch: Partial<Card>) => {
        patches.push(patch);
      },
      reload: () => {},
      onError: () => {},
      errMessage: () => "",
    };
    const placements = makeCardPlacements(card, board, deps);
    if (act) {
      act(placements);
    } else {
      placements.onAttachProject("engineering", "Cozystack");
    }
    return patches[0];
  };

  it("takes no dates from a week an older repository left on the card (K7)", () => {
    const patch = run({ ...mk({ itemId: "c1" }), week: "2026-08-24" } as Card);
    expect(patch).toEqual({ project: "engineering", scope: "Cozystack" });
  });

  it("keeps a dated card's chosen schedule", () => {
    const patch = run(mk({ itemId: "c1", startDate: "2026-09-07", day: "2026-09-09" }));
    expect(patch.startDate).toBeUndefined();
    expect(patch.day).toBeUndefined();
  });

  // Parking from the assign menu is the same door as every other into the
  // backlog: out of work — nobody, no day, no sprint — and planned (K1, K3).
  it("parks the card out of work, the way every door into the backlog does", () => {
    const patch = run(mk({ itemId: "c1", assignees: ["kvaps"], startDate: "2026-09-07", sprintStart: "2026-09-07" }), (p) =>
      p.onPark?.(),
    );
    expect(patch).toEqual(parkedLocally());
  });
});

// The Project board's own filter, as a rule rather than a line inside a
// component: which cards a column grid draws.
describe("drawnOnProjectBoard", () => {
  const shown = new Set(["engineering\u0000Cozystack", "freedom\u0000Launch"]);

  it("draws a card filed under a shown column", () => {
    expect(
      drawnOnProjectBoard(mk({ project: "engineering", scope: "Cozystack" }), shown),
    ).toBe(true);
  });

  it("draws a SUBTASK that carries its own column", () => {
    expect(
      drawnOnProjectBoard(
        mk({ project: "engineering", scope: "Cozystack", parent: "p1" }),
        shown,
      ),
    ).toBe(true);
  });

  it("draws nothing for a card with no column of its own", () => {
    expect(drawnOnProjectBoard(mk({ parent: "p1" }), shown)).toBe(false);
    expect(drawnOnProjectBoard(mk({ project: "engineering" }), shown)).toBe(false);
  });

});

// the progress bars count what the grid DRAWS. A card in a column with no
// start has no slot — its start is its row, and a stored week no longer
// stands in for one (nothing writes a week on a project card, H4).
describe("drawnAsSlot", () => {
  it("needs a start date to be a slot", () => {
    expect(drawnAsSlot(mk({ scope: "Cozystack", startDate: "2026-08-24" }))).toBe(true);
    expect(drawnAsSlot({ ...mk({ scope: "Cozystack" }), week: "2026-08-24" } as Card)).toBe(false);
    expect(drawnAsSlot(mk({ scope: "Cozystack" }))).toBe(false);
  });
});


// A project NAME can be declared in two repositories, its columns merged
// under one entry (G13) while each column keeps its own. The picker must
// ask the COLUMN — filtering by the project offered a column the server
// refuses, which is exactly what these targets promise never to do.
describe("targets on an alias project", () => {
  const board = {
    domains: [
      { name: "shared", writable: true, members: [] },
      { name: "closed", writable: true, members: [] },
    ],
    projects: ["portal"],
    scopes: [
      { name: "Bugs", project: "portal" },
      { name: "Docs", project: "portal", domain: "closed" },
    ],
    projectDomains: undefined,
  };

  it("offers a team's card nothing", () => {
    // A card in a column has no team (H4): the server refuses the filing,
    // whichever repository the column is in.
    expect(placementTargets(mk({ team: "platform" }), board).attach).toBeUndefined();
    expect(placementTargets(mk({ domain: "closed", team: "board" }), board).attach).toBeUndefined();
  });

  it("offers a card nothing holds only the columns its new project can hold", () => {
    // No team to keep it anywhere, so the project carries it — but only to
    // the repository the project itself is in, which the OTHER half of the
    // alias is not.
    const got = placementTargets(mk({}), board);
    expect(got.attach).toEqual([{ name: "portal", scopes: ["Bugs"] }]);
  });

});

// The origin block draws what the card IS, not what the menu can offer.
// Asking the offer instead left a card with an empty block: a stray
// divider line, the artefact the origin rule exists to prevent.
describe("hasOriginToShow", () => {
  it("is what the block asks — the menus are its siblings, not its children", () => {
    // A fresh unattached card has attach targets and no origin: asking the
    // OFFER drew the block empty, which is a bare divider bar with nothing
    // above it (the placement menus render outside this block and hide
    // themselves when they have nothing to list).
    expect(hasOriginToShow(mk({}))).toBe(false);
  });

  it("counts a column of no project", () => {
    expect(hasOriginToShow(mk({ scope: "Inbox" }))).toBe(true);
  });

  it("is false for a card that came from nowhere", () => {
    expect(hasOriginToShow(mk({}))).toBe(false);
  });
});

// The pickers narrow themselves to what the server accepts — the promise
// this module opens with. Two were left offering refusals after the column
// rules changed: a column cannot change repository at all, and a card in a
// no-project column takes its repository from its TEAM, so only that
// repository's teams can hold it.
describe("pickers that must not offer a refusal", () => {
  // With the roster, as the boards pass it: the stamps are read in the
  // board's one namespace, so an unstamped entry is the primary rather
  // than a repository of its own.
  const roster = { primary: "aeman-db", single: false };

  it("offers a column only the projects of its own repository", () => {
    const projects = ["engineering", "strategy"];
    const domains = { strategy: "founders" };
    expect(projectsAColumnCanJoin("aeman-db", projects, domains, "", roster)).toEqual([
      "engineering",
    ]);
    expect(projectsAColumnCanJoin("founders", projects, domains, "", roster)).toEqual([
      "strategy",
    ]);
    // And without a roster to read them in — a board that draws no
    // boundaries — nothing is refused.
    expect(projectsAColumnCanJoin("founders", projects, domains, "", { single: true })).toEqual(
      projects,
    );
  });

  it("offers a card in a closed column only that repository's teams", () => {
    const teams = ["platform", "board"];
    const domains = { board: "founders" };
    expect(teamsACardCanTake("founders", teams, domains, "", roster)).toEqual(["board"]);
    // What the card already carries stays on the list, so a pair written
    // before the rule can be seen and changed.
    expect(teamsACardCanTake("founders", teams, domains, "platform", roster)).toEqual([
      "platform",
      "board",
    ]);
  });
});

// The exported mirrors of the server's rules carry its refusals too — an
// outcome that says "delete" where the server says "no such column" is a
// deletion nobody sanctioned, which is the shape the server's own guard
// was written against.

// Attaching asks where the card will BE. A card whose project decides is
// carried along by the new one, so a column of another repository is a
// lawful destination — the server accepts it, and hiding it kept the
// no-project bucket unreachable. A card its TEAM holds stays put (G46).
describe("attachTargets and the card's binding", () => {
  const projects = ["engineering", "strategy"];
  const scopes = [
    { name: "Cozystack", project: "engineering" },
    { name: "Fundraising", project: "strategy", domain: "founders" },
  ];
  // As the server sends it: the project of the closed column is stamped
  // too, and a column agrees with the project that owns it.
  const projectDomains = { strategy: "founders" };
  // Two repositories, the primary named: the shape a board with a boundary
  // to police is served in. Without a second one there is no boundary at
  // all and every domain rule answers yes (rosterOf().single).
  const domains = [
    { name: "shared", writable: true, members: [] },
    { name: "closed", writable: true, members: [] },
  ];


  it("offers every column to a card nothing holds", () => {
    const got = placementTargets(mk({}), {
      projects,
      scopes,
      projectDomains,
      domains,
    });
    expect(got.attach?.map((p) => p.name)).toEqual(["engineering", "strategy"]);
  });

  it("offers a team's card nothing at all", () => {
    // A card in a column has no team (H4), so the question of which
    // repository a team holds the card in no longer reaches the picker.
    const got = placementTargets(mk({ team: "platform" }), {
      projects,
      scopes,
      projectDomains,
      domains,
    });
    expect(got.attach).toBeUndefined();
  });

  // The relaxation is the PROJECT's doing, and the no-project bucket is not
  // a project: a card attached there is placed by nothing, so it stays in
  // the repository it was in. A foreign bucket column is a refusal with a
  // friendly label.
  it("never offers a foreign no-project bucket", () => {
    const withBucket = [...scopes, { name: "Inbox", project: "", domain: "founders" }];
    const got = placementTargets(mk({}), {
      projects,
      scopes: withBucket,
      projectDomains,
      domains,
    });
    expect(got.attach?.find((p) => p.name === "")).toBeUndefined();
  });

  it("offers the bucket of the card's own repository", () => {
    const withBucket = [...scopes, { name: "Inbox", project: "" }];
    const got = placementTargets(mk({}), {
      projects,
      scopes: withBucket,
      projectDomains,
      domains,
    });
    expect(got.attach?.find((p) => p.name === "")?.scopes).toEqual(["Inbox"]);
  });

  // A column of an ALIAS project (G13) is declared in the other half of the
  // merged entry: the project would carry the card to ITS repository, and
  // the column is not in it. The server compares exactly those two.
  it("never offers a column its own project could not hold the card in", () => {
    expect(
      attachTargets(
        ["freedom"],
        [{ name: "Launch", project: "freedom", domain: "closed" }],
        "shared",
        false,
      ),
    ).toEqual([]);
  });
});

// A column's bar counts exactly what the column DRAWS: the card's own pair,
// and nothing for a card outside every column.
describe("columnsOf", () => {
  it("lists nothing for a card in no column", () => {
    expect(columnsOf(mk({}))).toEqual([]);
  });
});

// The controls that create and un-team a card must not offer a refusal
// either — a card with no team is held by the PRIMARY repository, so a
// column of another one cannot show it.
describe("controls that must match the server", () => {
  // A board of TWO repositories: the primary is named, and so is every
  // entry — the shape the server serves whenever there is a boundary to
  // police.
  const db = { primary: "aeman-db", single: false };

  it("offers No team only where a teamless card can stand", () => {
    expect(teamlessIsLawful("aeman-db", db)).toBe(true);
    expect(teamlessIsLawful("", db)).toBe(true);
    expect(teamlessIsLawful("founders", db)).toBe(false);
    // ...unless a PROJECT holds the card: it stays in that project's
    // repository with no team at all, so the entry is lawful there.
    expect(teamlessIsLawful("founders", db, "strategy")).toBe(true);
  });

  it("opens a new card only where one can be created", () => {
    // The card carries no team, so its PROJECT puts it in its own
    // repository — and the column has to be there too. A project of the
    // closed repository with its own column: lawful.
    const closed = { ...db, projectDomains: { engineering: "founders" } };
    expect(
      canCreateInColumn({ project: "engineering", domain: "founders" }, closed),
    ).toBe(true);
    // The same column under a project of the PRIMARY — the alias case
    // (G13), where one project name holds columns of two repositories:
    // the project would put the card in the primary, and the column is
    // not there. The server returns 422; the "+" does not open.
    expect(canCreateInColumn({ project: "engineering", domain: "founders" }, db)).toBe(
      false,
    );
    // A bucket column cannot hold a teamless card unless it is the
    // primary's own: nothing else places such a card.
    expect(canCreateInColumn({ project: "", domain: "aeman-db" }, db)).toBe(true);
    expect(canCreateInColumn({ project: "", domain: "founders" }, db)).toBe(false);
  });
});

// The no-project bucket is a home the server accepts; a picker that walks
// only the roster's projects could never name it, so the gesture the
// codec, the store and the service were all changed for had no way in.
describe("projectsWithColumns", () => {
  it("adds the bucket when it holds columns", () => {
    expect(
      projectsWithColumns(["engineering"], [
        { name: "Cozystack", project: "engineering" },
        { name: "Inbox", project: "" },
      ]),
    ).toEqual(["engineering", ""]);
  });

  it("leaves it out when nothing is filed there", () => {
    expect(
      projectsWithColumns(["engineering"], [{ name: "Cozystack", project: "engineering" }]),
    ).toEqual(["engineering"]);
  });
});

// The shape a SINGLE-REPOSITORY board is served in — the default
// deployment, and the one no test covered: the board names its one
// repository, and every entry in the payload carries that name, because
// the store stamps them all (G59). Read with an empty primary, those
// stamps make one repository look like two, and every rule here then
// refuses what the server accepts: the "+" in the no-project bucket, "No
// team", and the grid's × on a subtask, which the client would send as a
// hard delete while the server ungroups the card and keeps it.
describe("a board of one repository", () => {
  const roster = rosterOf({
    domains: [{ name: "aeman", writable: true, members: [] }],
    projectDomains: { engineering: "aeman" },
  });
  const scopes = [
    { name: "Inbox", project: "", domain: "aeman" },
    { name: "Cozystack", project: "engineering", domain: "aeman" },
  ];

  it("has no boundary to police", () => {
    expect(roster.primary).toBe("aeman");
    expect(roster.single).toBe(true);
  });

  it("opens the + in the no-project bucket", () => {
    expect(canCreateInColumn({ project: "", domain: "aeman" }, roster)).toBe(true);
    expect(canCreateInColumn({ project: "engineering", domain: "aeman" }, roster)).toBe(true);
  });

  it("offers No team in the no-project bucket", () => {
    expect(teamlessIsLawful("aeman", roster)).toBe(true);
  });

  it("offers every column to attach to", () => {
    expect(
      attachTargets(["engineering"], scopes, "aeman", true, roster).map((p) => p.name),
    ).toEqual(["engineering", ""]);
  });

  // And a board that names NO repository at all — nothing to compare
  // against, while the entries still carry a name — is the same case: no
  // boundary, so nothing is refused. Read literally, that payload puts
  // "aeman" beside an empty primary and calls one repository two.
  it("has no boundary when the board names no repository at all", () => {
    const unnamed = rosterOf({ projectDomains: { engineering: "aeman" } });
    expect(unnamed.single).toBe(true);
    expect(canCreateInColumn({ project: "", domain: "aeman" }, unnamed)).toBe(true);
    expect(teamlessIsLawful("aeman", unnamed)).toBe(true);
    expect(
      attachTargets(["engineering"], scopes, "aeman", true, unnamed).map((p) => p.name),
    ).toEqual(["engineering", ""]);
  });
});

// A LINK decides where a card lives ahead of its project (G14), so a
// subtask — or a review card, or an iteration — cannot be carried into
// another repository by a new project. Offering those columns was a
// refusal with a friendly label, exactly as it was for a teamed card.
describe("attach targets for a card a LINK holds", () => {
  const board = {
    projects: ["engineering", "strategy"],
    scopes: [
      { name: "Cozystack", project: "engineering", domain: "aeman-db" },
      { name: "Fundraising", project: "strategy", domain: "founders" },
    ],
    projectDomains: { engineering: "aeman-db", strategy: "founders" },
    domains: [
      { name: "aeman-db", writable: true, members: [] },
      { name: "founders", writable: true, members: [] },
    ],
  };

  it("offers a subtask and a review card nothing", () => {
    // A card in a column is nobody's subtask and never a review (H4): the
    // link-held card is refused the filing before the repository question.
    const kid = mk({ parent: "p", domain: "aeman-db" });
    expect(placementTargets(kid, board).attach).toBeUndefined();
    const rev = mk({ reviewOf: "orig", domain: "aeman-db" });
    expect(placementTargets(rev, board).attach).toBeUndefined();
  });

  it("still carries a card nothing holds", () => {
    const loose = mk({ domain: "aeman-db" });
    expect(placementTargets(loose, board).attach?.map((p) => p.name)).toEqual([
      "engineering",
      "strategy",
    ]);
  });
});

// The left stripe says where a card came FROM, and every board that draws one
// asks this — the day boards down the card's edge. Two boards deciding it
// apart is how one of them came to draw nothing at all
// after the weekly plan, whose band the stripe used to be, was taken out.
describe("markOf", () => {
  // A review is what arrived from elsewhere; a project card, the other kind
  // the stripe used to mark, is on no board that draws the stripe (H4).
  it("marks a review, and nothing else", () => {
    expect(markOf({ reviewOf: "c1" })).toBe("review");
    expect(markOf({})).toBe("");
  });
});
