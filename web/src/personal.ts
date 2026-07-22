// The personal board: a second GitHub Project (the user's own, private)
// attached beside the work board. The pointer to it is a per-login client
// preference — it never reaches the server or other users; the project's
// privacy itself is enforced by GitHub (every API call runs under the
// requesting user's token, and the server's board cache is gated per login).
// See docs/design/personal-board.md.

export interface PersonalPointer {
  owner: string;
  number: number;
}

export type MePane = "work" | "personal";

const KEY_PREFIX = "aeman.personal.";

// Storage is passed in (not imported) so tests run against a plain fake.
type StorageLike = Pick<Storage, "getItem" | "setItem" | "removeItem">;

const key = (login: string) => `${KEY_PREFIX}${login}`;

export function readPersonalBoard(
  storage: StorageLike,
  login: string,
): PersonalPointer | null {
  if (!login) {
    return null;
  }
  const raw = storage.getItem(key(login));
  if (!raw) {
    return null;
  }
  try {
    const parsed = JSON.parse(raw) as unknown;
    if (
      typeof parsed === "object" &&
      parsed !== null &&
      typeof (parsed as PersonalPointer).owner === "string" &&
      (parsed as PersonalPointer).owner !== "" &&
      typeof (parsed as PersonalPointer).number === "number" &&
      Number.isInteger((parsed as PersonalPointer).number) &&
      (parsed as PersonalPointer).number > 0
    ) {
      const p = parsed as PersonalPointer;
      return { owner: p.owner, number: p.number };
    }
    return null;
  } catch {
    return null;
  }
}

export function savePersonalBoard(
  storage: StorageLike,
  login: string,
  ptr: PersonalPointer,
): void {
  if (!login) {
    return;
  }
  storage.setItem(key(login), JSON.stringify(ptr));
}

export function clearPersonalBoard(storage: StorageLike, login: string): void {
  if (!login) {
    return;
  }
  storage.removeItem(key(login));
}

// samePointer guards the setup dialog: attaching the WORK board as the
// personal board would double every card and route "personal" writes into
// the shared project. Owner comparison is case-insensitive (GitHub logins
// and org slugs are).
export function samePointer(
  a: { owner: string; number: number },
  b: { owner: string; number: number },
): boolean {
  return (
    a.owner.trim().toLowerCase() === b.owner.trim().toLowerCase() &&
    a.number === b.number
  );
}

// personalPaneVisible is the single gate for everything personal in the UI
// (the Me pane, the virtual Team chip): only for the owner themselves (never
// while impersonating), only when a pointer is attached, only when the board
// actually loaded — and never in lock-board mode, where the server pins every
// request to the locked project and would silently serve the WORK board in
// the personal slot (see docs/design/personal-board.md, "lock-board mode").
export function personalPaneVisible(args: {
  lockBoard: boolean;
  viewAs: string | null;
  pointer: PersonalPointer | null;
  boardLoaded: boolean;
}): boolean {
  return (
    !args.lockBoard && !args.viewAs && args.pointer !== null && args.boardLoaded
  );
}

// nextPane / prevPane implement the narrow-screen ‹ › switcher: two panes,
// arrows clamp at the edges (no wrap — matching the day-nav arrows' feel of
// a bounded strip rather than a carousel).
export function nextPane(cur: MePane): MePane {
  return cur === "work" ? "personal" : "personal";
}

export function prevPane(cur: MePane): MePane {
  return cur === "personal" ? "work" : "work";
}
