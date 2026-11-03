import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { MeBoard } from "./MeBoard";
import { TeamBoard } from "./TeamBoard";
import type { Board, Card, Provider } from "../providers/types";
import { todayIso } from "../date";

// The day boards' insert "+": no add box at the foot of a band any more —
// every gap of a cell (Team: a person's column × a band; Me: the band) has an
// insert "+" that types a card in right there (insertgap.ts), and an empty
// cell has one shown on hover. The Me board adds in ONE band, the unplanned
// one, so only that band has gaps there. Markup only.

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
globalThis.window ??= { matchMedia: () => ({ matches: false, addEventListener: () => {}, removeEventListener: () => {} }) } as never;

const today = todayIso();
const card = (over: Partial<Card>): Card =>
  ({ itemId: "c", title: "a card", assignees: ["bob"], progress: 0, team: "alpha", startDate: today, day: today, sprintStart: today, ...over }) as Card;

const board = (cards: Card[]): Board =>
  ({
    title: "test",
    url: "",
    cards,
    teams: ["alpha"],
    projects: [],
    scopes: [],
    deadlines: [],
    members: [{ login: "bob" }],
    domains: [],
    sprintStates: { alpha: { current: today } },
  }) as unknown as Board;

const common = {
  provider: {} as Provider,
  me: "bob",
  selectedDate: today,
  avatars: {},
  names: {},
  teamFilter: null,
  onSetFilter: vi.fn(),
  onAddTeam: vi.fn(),
  onRemoveTeam: vi.fn(),
  onRenameTeam: vi.fn(),
  patchCard: vi.fn(),
  addCard: vi.fn(),
  replaceCard: vi.fn(),
  removeCard: vi.fn(),
  reorderCards: vi.fn(),
  reload: vi.fn(),
  onError: vi.fn(),
  onOpen: vi.fn(),
};

describe("the Team grid's insert +", () => {
  const draw = (cards: Card[]) =>
    renderToStaticMarkup(
      <TeamBoard
        {...common}
        board={board(cards)}
        onSelectDate={vi.fn()}
        liveSprints={{ alpha: { current: today } } as never}
        roster={["alpha"]}
        onReorderTeams={vi.fn()}
        track={(p) => p}
      />,
    );

  it("has no add box at the foot of a band, and a + in every gap of a cell", () => {
    const html = draw([card({ itemId: "a", zone: "gray" }), card({ itemId: "b", zone: "gray" })]);
    expect(html).not.toContain(">+ add<");
    // bob's planned cell: above the first card, between the two, below the last.
    const planned = html.slice(html.indexOf(">PLANNED<"), html.indexOf(">NICE TO HAVE<"));
    expect(planned.match(/data-insert-gap="/g)?.length).toBe(3);
    expect(planned.indexOf('data-insert-gap="1"')).toBeGreaterThan(planned.indexOf("sortable-card"));
  });

  it("gives an empty cell one +, shown on hover of the cell", () => {
    const html = draw([card({ itemId: "a", zone: "gray" })]);
    expect(html).toContain("insert-gap insert-gap-empty");
  });
});

describe("the Me board's insert +", () => {
  const draw = (cards: Card[]) =>
    renderToStaticMarkup(
      <MeBoard
        {...common}
        board={board(cards)}
        viewAs={null}
        onViewAs={vi.fn()}
        connectHint=""
        teams={["alpha"]}
      />,
    );

  it("has no add box, and gaps only in the unplanned band — the one it adds in", () => {
    const html = draw([card({ itemId: "u", zone: "yellow" }), card({ itemId: "p", zone: "gray" }), card({ itemId: "r", zone: "red" })]);
    expect(html).not.toContain(">+ add<");
    const total = html.match(/data-insert-gap="/g)?.length ?? 0;
    expect(total).toBe(2); // above and below the one unplanned card
  });
});

// A band's vertical label is CENTRED in its band, whatever the band's height
// — the Team grid's cells and the Me board's bands alike (both draw
// .zone-spine inside .zone-area). Stretched to the band's height, the label
// leaned on text-align to centre itself, and in a tall band it drifted to
// the top; centred as an item, it cannot.
describe("a band's label", () => {
  const css = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");
  const spine = css.match(/\.zone-spine \{[^}]*\}/)?.[0] ?? "";

  it("is centred in its band, never stretched, sticky or pinned to the top", () => {
    expect(spine).toMatch(/align-self: center;/);
    expect(spine).not.toMatch(/position:\s*sticky/);
    expect(spine).not.toMatch(/align-self:\s*(flex-start|start|stretch)/);
  });

  it("is drawn by both day boards inside their bands", () => {
    const team = renderToStaticMarkup(
      <TeamBoard
        {...common}
        board={board([])}
        onSelectDate={vi.fn()}
        liveSprints={{ alpha: { current: today } } as never}
        roster={["alpha"]}
        onReorderTeams={vi.fn()}
        track={(p) => p}
      />,
    );
    const me = renderToStaticMarkup(
      <MeBoard {...common} board={board([])} viewAs={null} onViewAs={vi.fn()} connectHint="" teams={["alpha"]} />,
    );
    for (const html of [team, me]) {
      expect(html).toMatch(/class="zone-area"[^>]*><span class="zone-spine">/);
    }
  });
});

// The insert "+" ADDS NO HEIGHT: the cards of a band stand exactly as far
// apart as they did before it (origin/main: .zone-cards gap 4px, padding-top
// 4px). A gap is a zero-height line whose negative margin cancels the flex
// gap it adds, and its hover zone and its "+" are laid over the existing
// gap — the "+" out in the band's left field, where the spine is.
describe("the insert + and the spacing of a band", () => {
  const css = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");
  const rule = (sel: string) => css.match(new RegExp(`(^|\\n)${sel.replace(/[.]/g, "\\.")} \\{[^}]*\\}`))?.[0] ?? "";

  it("keeps the cards of a band 4px apart, as before", () => {
    const cards = rule(".zone-cards");
    expect(cards).toMatch(/gap: 4px;/);
    expect(cards).toMatch(/padding-top: 4px;/);
  });

  // The end of a band mirrors its start: as much space below the last card
  // as above the first, and no trailing room left by the old add box.
  it("leaves as much below the last card as above the first", () => {
    const cards = rule(".zone-cards");
    const top = /padding-top: (\d+)px;/.exec(cards)?.[1];
    const bottom = /padding-bottom: (\d+)px;/.exec(cards)?.[1];
    expect(top).toBe("4");
    expect(bottom).toBe(top);
    expect(cards).not.toMatch(/padding: /);
  });

  it("is a zero-height line that cancels the flex gap it adds", () => {
    const gap = rule(".insert-gap");
    expect(gap).toMatch(/height: 0;/);
    expect(gap).toMatch(/margin-top: calc\(-1 \* var\(--insert-gap-gap, 4px\)\);/);
    expect(gap).toMatch(/position: relative;/);
  });

  // Right next to the card's left edge: centred 10–12px left of it, in the
  // band's left field, never over the card.
  it("puts the + right beside the card's left edge, not over it", () => {
    const btn = rule(".insert-gap-btn");
    expect(btn).toMatch(/position: absolute;/);
    const left = Number(/left: var\(--insert-gap-left, (-?\d+)px\);/.exec(btn)?.[1]);
    const width = Number(/width: (\d+)px;/.exec(btn)?.[1]);
    const centre = left + width / 2;
    expect(centre).toBeGreaterThanOrEqual(-12);
    expect(centre).toBeLessThanOrEqual(-10);
    expect(left + width).toBeLessThanOrEqual(0);
  });
});

// #290: the Me board draws who else has a card selected — a lead selecting
// it on the Team board — the way the Team board does.
describe("the Me board's presence marks", () => {
  it("draws the lead's selection on the person's card", () => {
    const html = renderToStaticMarkup(
      <MeBoard
        {...common}
        board={board([card({ itemId: "u", zone: "yellow" })])}
        viewAs={null}
        onViewAs={vi.fn()}
        connectHint=""
        teams={["alpha"]}
        presence={{ lead: "u" }}
      />,
    );
    expect(html).toContain("card-peer-selected");
    expect(html).toContain('title="Selected by lead"');
  });
});
