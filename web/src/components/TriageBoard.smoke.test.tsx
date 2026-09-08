import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { TriageBoard } from "./TriageBoard";
import type { Board, Card, Provider } from "../providers/types";
import { addDays, mondayOf, todayIso } from "../date";

// Triage reads down a person's column: every card is a plain box of one week,
// they stand one under the next at the full column width, and the week grows
// to hold them. The first row is NOW, and it carries everything the team has
// — this week's work and everything nobody has given a week. This is the one
// test that proves the tree comes out that way: markup only, so what it pins
// is the shape, not the gestures.

// Every date here counts from THIS week's Monday, because the grid opens on
// the week holding today and draws no week before it. Written as calendar
// dates, the fixture worked until the calendar left it behind: it named
// 2026-08-31, and on the Monday after, four of these tests failed — the cards
// sat in a week the board no longer draws, so a card of two weeks came out one
// week long. A test of the shape must not expire.
const store = new Map<string, string>();
globalThis.localStorage = {
  getItem: (k: string) => store.get(k) ?? null,
  setItem: (k: string, v: string) => void store.set(k, v),
  removeItem: (k: string) => void store.delete(k),
  clear: () => store.clear(),
  key: () => null,
  get length() {
    return store.size;
  },
} as Storage;

const week = mondayOf(todayIso());
// Friday of this week: a card of ONE week.
const thisFriday = addDays(week, 4);
// Wednesday and Friday of the NEXT one: a card reaching into a second week.
const nextWednesday = addDays(week, 9);
const nextFriday = addDays(week, 11);
// The two Mondays after this one — turns still to come, and deadlines.
const nextWeek = addDays(week, 7);
const weekAfter = addDays(week, 14);

const card = (over: Partial<Card> = {}): Card =>
  ({
    itemId: "c1",
    title: "A card of one week",
    assignees: ["lexfrei"],
    stage: "",
    team: "core",
    week,
    ...over,
  }) as Card;

const board = (cards: Card[], deadlines: { week: string; project: string }[] = []): Board =>
  ({
    title: "test",
    url: "",
    cards,
    teams: ["core"],
    projects: [],
    epics: [],
    deadlines,
    processes: [],
    members: [],
    domains: [],
    sprintStates: {},
  }) as unknown as Board;

function draw(cards: Card[], deadlines: { week: string; project: string }[] = []) {
  return renderToStaticMarkup(
    <TriageBoard
      board={board(cards, deadlines)}
      provider={{} as Provider}
      roster={["core"]}
      teamFilter={["core"]}
      onSetFilter={vi.fn()}
      avatars={{}}
      names={{}}
      patchCard={vi.fn()}
      addCard={vi.fn()}
      replaceCard={vi.fn()}
      removeCard={vi.fn()}
      reorderCards={vi.fn()}
      reload={vi.fn()}
      onOpen={vi.fn()}
      onError={vi.fn()}
    />,
  );
}

describe("the Triage board", () => {
  it("opens on this week — what was owed before is owed now", () => {
    const html = draw([card()]);
    // No past rows at all, and no button offering any.
    expect(html).toContain("grid-row:2;grid-column:1");
    expect(html).toContain(">now<");
    expect(html).not.toContain("earlier weeks");
  });

  it("lets a week's rows grow, so the cards in it need not be sliced", () => {
    const html = draw([card()]);
    expect(html).toContain("minmax(28px, auto)");
  });

  it("stands a week's cards one under the next, each the same height", () => {
    const html = draw([
      card(),
      card({ itemId: "c2", title: "Under it" }),
      card({ itemId: "c3", title: "And under that" }),
    ]);
    expect(html).toContain("grid-row:2;height:24px;margin-top:0px");
    expect(html).toContain("grid-row:2;height:24px;margin-top:28px");
    expect(html).toContain("grid-row:2;height:24px;margin-top:56px");
    // The column is never sliced: there is nothing to stand beside.
    expect(html).not.toContain("margin-left");
  });

  it("draws a card of two weeks as two cards, each saying which it is", () => {
    const html = draw([card({ title: "Two weeks of work", day: nextFriday })]);
    expect(html).toContain("(1/2)");
    expect(html).toContain("(2/2)");
    // One in this week's row and one in the next, each a box of one row: no
    // card on this board is ever a rectangle across several of them.
    expect(html).toContain("grid-row:2;height:24px;margin-top:0px");
    expect(html).toContain("grid-row:3;height:24px;margin-top:0px");
    expect(html).not.toMatch(/class="project-slot[^"]*" style="[^"]*span/);
  });

  it("stripes a card down its left in the colour of the zone it stands in", () => {
    expect(draw([card({ zone: "red" })])).toContain("triage-slot-zone-red");
    expect(draw([card({ zone: "green" })])).toContain("triage-slot-zone-green");
  });

  it("stripes a project card with the project mark, not a zone", () => {
    // A slot is a commitment made on the Project board and only passing
    // through this one: its week is that board's to say, so a zone stripe
    // would credit the decision to somebody who did not make it.
    const html = draw([card({ epic: "Storage", day: thisFriday, zone: "red" })]);
    expect(html).toContain("triage-slot-project");
    expect(html).not.toContain("triage-slot-zone-red");
  });

  it("marks every week a stretched slot passes through", () => {
    // Split across its weeks, each box is the same commitment and wears the
    // same mark — one part carrying a zone stripe would read as two cards.
    const html = draw([card({ epic: "Storage", day: nextWednesday })]);
    expect(html.match(/triage-slot-project/g)).toHaveLength(2);
  });

  it("stacks a week in the order somebody triaging wants to meet it", () => {
    // Debts first, then the project's own work, then the zones.
    const html = draw([
      card({ itemId: "a", title: "If time left", zone: "green" }),
      card({ itemId: "b", title: "Planned", zone: "gray" }),
      card({ itemId: "c", title: "A slot", epic: "Storage" }),
      card({ itemId: "d", title: "Urgent", zone: "red" }),
      card({ itemId: "e", title: "A debt", zone: "green", overdue: true }),
    ]);
    const order = ["A debt", "A slot", "Urgent", "Planned", "If time left"];
    expect(order.map((t) => html.indexOf(t))).toEqual(
      [...order.map((t) => html.indexOf(t))].sort((x, y) => x - y),
    );
    // …and they stand under one another, in that order.
    expect(html).toContain("margin-top:0px");
    expect(html).toContain("margin-top:112px");
  });

  it("colours the progress bar by stage, as Team and Me do", () => {
    // A card locked or in review says so by its bar on the other boards; it
    // must not say something else here.
    expect(draw([card({ stage: "locked", progress: 30 })])).toContain(
      "background:var(--stage-locked)",
    );
    expect(draw([card({ stage: "review", progress: 85 })])).toContain(
      "background:var(--stage-review)",
    );
    expect(draw([card({ progress: 40 })])).toContain("background:var(--bar-default)");
  });

  it("leaves a card of no zone the plain edge", () => {
    expect(draw([card()])).not.toContain("triage-slot-zone");
  });

  it("keeps the debt's own red over the zone's", () => {
    // Urgent wears the lighter red and a debt the strong one; a card that is
    // both must read as the debt, so the late class comes last in the sheet.
    const html = draw([card({ zone: "red", overdue: true })]);
    expect(html).toContain("project-slot-late");
    expect(html).toContain("triage-slot-zone-red");
  });

  it("fills in nothing while nothing is being carried", () => {
    // The gap between the boxes of one card is filled in only while the card
    // is in the reader's hand; a board at rest has nothing to say about it.
    // And the cell under a carried card is never tinted at all: the card is
    // already drawn where it would land, which says it once.
    const html = draw([card({ title: "Two weeks of work", day: nextFriday })]);
    expect(html).not.toContain("triage-span");
    expect(html).not.toContain("project-cell-drag");
  });

  it("counts a card of two weeks against both of them", () => {
    const html = draw([card({ day: nextFriday })]);
    // A week says what it CARRIES, in points, beside its date — two weeks of
    // work is two weeks' worth, not one card filed early. The card here is
    // unsized, so it weighs the default (M = 2) in each of the first two rows.
    expect(html.match(/class="triage-points[^"]*">2</g)?.length).toBe(2);
  });

  it("says nothing of parts for a card that takes a single week", () => {
    expect(draw([card()])).not.toContain("triage-slot-part");
  });

  it("draws a card just placed, whatever start date it carried in", () => {
    // A card waiting in the strip usually carries dates from the day board.
    // The row here is the week TRIAGE gave it and nothing else: letting an
    // old start date win drew the card nowhere at all, which is what a card
    // dragged out of the strip looked like.
    const html = draw([card({ startDate: "2026-06-01", day: "2026-06-05" })]);
    expect(html).toContain("A card of one week");
    expect(html).toContain("grid-row:2;height:24px;margin-top:0px");
  });

  it("does not drag the window back to a start date nobody is looking at", () => {
    const html = draw([card({ startDate: "2026-06-01" })]);
    // Still this week first, and the nine rows this board always opens with.
    expect(html).toContain(">now<");
    expect(html.match(/project-week[ "]/g)?.length).toBe(9);
  });

  it("stands what nobody has given a week in the first row, with this week's work", () => {
    const html = draw([
      card({ title: "Planned for this week" }),
      card({ itemId: "c2", week: undefined, title: "Nobody has dated it" }),
    ]);
    // Both in row 2 — the grid's first row, which is now — one under the
    // other, and drawn exactly alike: the row is what the team is carrying.
    expect(html).toContain("grid-row:2;height:24px;margin-top:0px");
    expect(html).toContain("grid-row:2;height:24px;margin-top:28px");
    expect(html).toContain("Nobody has dated it");
    // No pile beside the grid any more.
    expect(html).not.toContain("triage-strip");
  });


  it("stands a card of no week in its owner's column", () => {
    const html = draw([card({ week: undefined, assignees: ["lexfrei"] })]);
    // Column 3: the week labels, then Unassigned, then lexfrei.
    expect(html).toContain("grid-column:3");
  });

  it("draws no review of its own — it follows the card it reviews", () => {
    const html = draw([card({ week: undefined, title: "Review of it", reviewOf: "c9" })]);
    expect(html).not.toContain("Review of it");
    expect(html).not.toContain("project-slot triage-slot");
  });

  it("says on a line under a cell's cards how many reviews stand in it", () => {
    // A review is not triage work — nobody is waiting on a week for it — so
    // it is not among the cards. But it IS work in the reviewer's hands, it
    // counts in the number over their name and in the week's points, and one
    // whose dates ran out is on no other board at all. So its cell says so,
    // and opens them where they belong: that person, that week. (What they
    // weigh and where they stand: size.test.ts and triage.test.ts.)
    const html = draw([
      card({ title: "Real work", assignees: ["lexfrei"] }),
      card({ title: "Review of it", reviewOf: "c9", assignees: ["lexfrei"] }),
    ]);
    expect(html).toContain("Real work");
    expect(html).not.toContain("Review of it");
    expect(html).toContain('class="triage-reviews-line"');
    expect(html).toContain("+1 review<");
    // Plain text, like the add control on the day boards: it is not a card
    // and must not be drawn as one.
    expect(html).not.toContain("triage-reviews-line project-slot");
    // The week counts what it costs whether or not the line is open: one
    // unsized card at M and one unsized review at S.
    expect(html).toContain(">3</span>");
  });

  it("counts several reviews in one line, and none where there are none", () => {
    const two = draw([
      card({ title: "Review A", reviewOf: "c9", assignees: ["lexfrei"] }),
      card({ title: "Review B", reviewOf: "c8", assignees: ["lexfrei"] }),
    ]);
    expect(two).toContain("+2 reviews<");
    expect(draw([card({ assignees: ["lexfrei"] })])).not.toContain("triage-reviews-line");
  });

  it("draws no card sent to review — it waits on a reviewer, not on a week", () => {
    const html = draw([
      card({ week: undefined, title: "Sent to review", stage: "review", progress: 85 }),
    ]);
    expect(html).not.toContain("Sent to review");
    expect(html).not.toContain("project-slot triage-slot");
  });

  it("keeps a card sent to review on the board once it has a week", () => {
    const html = draw([card({ title: "Sent to review", stage: "review", progress: 85 })]);
    expect(html).toContain("Sent to review");
  });

  it("draws the turns a process is going to file in the weeks they fall in", () => {
    // Not cards yet, but the weeks are already carrying them: a week spoken
    // for by a process is not a week the team is free in.
    const html = renderToStaticMarkup(
      <TriageBoard
        board={
          {
            ...board([card()]),
            processes: [
              {
                name: "Duty",
                project: "",
                tasks: [
                  {
                    uid: "t1",
                    title: "Rotate the keys",
                    recurrence: "week",
                    team: "core",
                    assignee: "lexfrei",
                    history: [],
                    due: [nextWeek, weekAfter],
                  },
                ],
              },
            ],
          } as unknown as Board
        }
        provider={{} as Provider}
        roster={["core"]}
        teamFilter={["core"]}
        onSetFilter={vi.fn()}
        avatars={{}}
        names={{}}
        patchCard={vi.fn()}
        addCard={vi.fn()}
        replaceCard={vi.fn()}
        removeCard={vi.fn()}
        reorderCards={vi.fn()}
        reload={vi.fn()}
        onOpen={vi.fn()}
        onError={vi.fn()}
      />,
    );
    // One in each week it is due in, in the column of whoever holds the task,
    // drawn as what it is — an outline, with nothing to press.
    expect(html.match(/triage-slot-coming/g)?.length).toBe(2);
    expect(html).toContain("Rotate the keys");
    expect(html).toContain("grid-row:3");
    expect(html).toContain("grid-row:4");
    // …and both weeks count it: a turn nobody has filed yet is work those
    // weeks are already spoken for, so it weighs like any unsized card (2)
    // beside their dates. The points and the card count must describe the
    // same set of boxes, or a week reads "1 card, 0 points".
    expect(html.match(/class="triage-points[^"]*">2</g)?.length).toBeGreaterThanOrEqual(2);
  });

  it("offers no grip to stretch anything by, with the catch closed", () => {
    // Only a PROJECT card has a span to pull — the weeks it takes are its row
    // on the Project board — and only with the catch lifted, since the grip
    // changes that board's own dates. Everything else here is one week's
    // work, and a grip would say something about it nothing else would.
    expect(draw([card({ epic: "Storage", day: thisFriday })])).not.toContain(
      "triage-slot-resize",
    );
    expect(draw([card()])).not.toContain("triage-slot-resize");
  });

  it("keeps the × off a project card while the catch is closed", () => {
    // What the × does to one is the Project board's business too.
    expect(draw([card({ epic: "Storage", day: thisFriday })])).not.toContain(
      "card-action-delete",
    );
    expect(draw([card()])).toContain("card-action-delete");
  });

  it("offers a catch to lift, and starts with it closed", () => {
    // Never remembered: a guard that stays open is not a guard, so every
    // visit begins with a project card's weeks held still.
    const html = draw([card({ epic: "Storage", day: thisFriday })]);
    expect(html).toContain('class="triage-lock"');
    expect(html).toContain('aria-pressed="false"');
    expect(html).not.toContain("triage-lock-open");
  });

  it("says beside a name what that person is carrying, in points and nothing else", () => {
    // The board is read through a team filter and a person is not: the
    // number is theirs across every team, and the server counts it. It is
    // said ONCE, in points — a card count beside it answered "how many
    // things", which is not the question a week that does not fit asks.
    const html = renderToStaticMarkup(
      <TriageBoard
        board={
          {
            ...board([card()]),
            members: [{ login: "lexfrei", carrying: 11, load: 21, capacity: 40 }],
          } as unknown as Board
        }
        provider={{} as Provider}
        roster={["core"]}
        teamFilter={["core"]}
        onSetFilter={vi.fn()}
        avatars={{}}
        names={{}}
        patchCard={vi.fn()}
        addCard={vi.fn()}
        replaceCard={vi.fn()}
        removeCard={vi.fn()}
        reorderCards={vi.fn()}
        reload={vi.fn()}
        onOpen={vi.fn()}
        onError={vi.fn()}
      />,
    );
    expect(html).toContain('class="person-load person-load-ok"');
    expect(html).toContain(">21/40<");
    expect(html).not.toContain("triage-person-load");
  });

  it("shows a person's load alone when nobody has set a capacity", () => {
    // Nothing is derived from the record any more, so most people have no
    // number — and "21/0" would read as a person with no room at all.
    const html = renderToStaticMarkup(
      <TriageBoard
        board={
          {
            ...board([card()]),
            members: [{ login: "lexfrei", carrying: 11, load: 21 }],
          } as unknown as Board
        }
        provider={{} as Provider}
        roster={["core"]}
        teamFilter={["core"]}
        onSetFilter={vi.fn()}
        avatars={{}}
        names={{}}
        patchCard={vi.fn()}
        addCard={vi.fn()}
        replaceCard={vi.fn()}
        removeCard={vi.fn()}
        reorderCards={vi.fn()}
        reload={vi.fn()}
        onOpen={vi.fn()}
        onError={vi.fn()}
      />,
    );
    expect(html).toContain('class="person-load person-load-unknown"');
    expect(html).toContain(">21<");
    expect(html).not.toContain("21/0");
  });

  it("wears the size on every box — the one it is COUNTED as while nobody has said", () => {
    // The board is where a week is read, so it is where the answer to "that
    // does not fit" is given: the letter is always on show and one click from
    // being changed. An unsized box wears the size it is WEIGHED as rather
    // than a dash — that letter is what the number over the person is made
    // of — in an empty dashed chip, so it never reads as somebody's word.
    const bare = draw([card()]);
    expect(bare).toContain("triage-slot-size triage-slot-size-unset");
    expect(bare).toContain(">M</button>");
    expect(bare).toContain("Nobody has sized this: counted as M");

    const sized = draw([{ ...card(), size: "L" } as never]);
    expect(sized).toContain('class="triage-slot-size"');
    expect(sized).toContain(">L</button>");
    expect(sized).not.toContain("triage-slot-size-unset");
  });

  it("says a week's points alone when there is no capacity to measure them", () => {
    // "2/0" would read as a week with no room at all; the number stands on
    // its own until somebody has set the people's capacities.
    const html = draw([card()]);
    expect(html).toContain(">2</span>");
    expect(html).not.toContain("2/0");
  });

  it("keeps half a card of room under the last one, to press on", () => {
    // There is always somewhere to start a card, however full the week.
    const html = draw([card()]);
    expect(html).toContain("grid-row:2;grid-column:3;min-height:42px");
    expect(html).toContain("grid-row:3;grid-column:3;min-height:14px");
  });

  it("says nothing beside a name with nothing to say", () => {
    expect(draw([card()])).not.toContain("triage-person-load");
  });

  it("offers every column to be dragged into another place", () => {
    expect(draw([card()])).toContain("project-epic-head-movable");
  });

  it("puts the columns in the order the reader dragged them into", () => {
    store.set("aeman.triage.people", JSON.stringify(["lexfrei", " nobody"]));
    const html = draw([card(), card({ itemId: "c2", assignees: [] })]);
    store.delete("aeman.triage.people");
    expect(html.indexOf("lexfrei")).toBeLessThan(html.indexOf("Unassigned"));
  });

  it("joins somebody the reader has never seen to the end, disturbing nothing", () => {
    store.set("aeman.triage.people", JSON.stringify(["lexfrei", " nobody"]));
    const html = draw([
      card(),
      card({ itemId: "c2", assignees: [] }),
      card({ itemId: "c3", assignees: ["newcomer"] }),
    ]);
    store.delete("aeman.triage.people");
    expect(html.indexOf("lexfrei")).toBeLessThan(html.indexOf("Unassigned"));
    expect(html.indexOf("Unassigned")).toBeLessThan(html.indexOf("newcomer"));
  });

  it("gives a column to every person, with the unassigned first", () => {
    const html = draw([card(), card({ itemId: "c2", assignees: [] })]);
    expect(html).toContain("Unassigned");
    expect(html.match(/project-epic-head triage-person/g)?.length).toBe(2);
    // Unassigned stands in the first column.
    expect(html.indexOf("Unassigned")).toBeLessThan(html.indexOf("lexfrei"));
  });

  // Every project of the team lands on this one board, so a bare red line
  // said only "something is due" and left the reader to work out whose. The
  // line carries its project's NAME in the week column, in that project's own
  // colour — the same colour the Project board gives it when several plans
  // share a screen.
  it("says whose deadline the line is", () => {
    const html = draw([card()], [
      { week: nextWeek, project: "cozystack" },
      { week: weekAfter, project: "freedom" },
    ]);
    expect(html).toContain("triage-deadline-label");
    expect(html).toContain("cozystack");
    expect(html).toContain("freedom");
    // The label stands in the week column; the line still crosses the cards.
    expect(html).toContain("grid-row:3;grid-column:1");
    expect(html).toContain("grid-row:3;grid-column:2 / -2");
    // Two projects, two colours: neither line is the plain danger red that
    // would make them indistinguishable.
    const colours = [...html.matchAll(/border-top-color:([^;"]+)/g)].map((m) => m[1]);
    expect(new Set(colours).size).toBeGreaterThan(1);
  });

  // A PROCESS TURN passes through this board too: its week is its process's
  // to say, so it wears the process mark rather than a zone nobody set here
  // — the same mark the day boards draw down its left edge.
  it("stripes a process turn with the process mark, not a zone", () => {
    const html = draw([card({ task: "t1", zone: "red" })]);
    expect(html).toContain("triage-slot-process");
    expect(html).not.toContain("triage-slot-zone-red");
  });
});
