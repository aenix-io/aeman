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
