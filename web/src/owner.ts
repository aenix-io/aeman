// Whose a task is: the owner picker's rule, apart from the menu that draws
// it. An owner is a TEAM and, within it, a PERSON or nobody — the server's
// `own` gesture takes the pair (docs/design/hierarchy.md, "Owner") and
// files loose work where the pair says: a person's card lands on their
// board today, a team's alone on that team's shelf, nobody's goes back to
// the strip. The picker is the Team board's two-column menu with the
// difference that a team is not an answer by itself: picking one keeps the
// menu open for the person, a person cannot be picked without a team, and
// "no person" is an answer of its own.
import type { Card } from "./providers/types";

/** Owner is the pair the picker ends with: "" for no team, "" for nobody. */
export interface Owner {
  team: string;
  person: string;
}

/** ownerOf reads a card's owner: its team and its first assignee. */
export function ownerOf(c: Pick<Card, "team" | "assignees">): Owner {
  return { team: c.team ?? "", person: c.assignees[0] ?? "" };
}

/** OwnerPick is one press in the menu. */
export type OwnerPick =
  | { kind: "team"; team: string }
  | { kind: "person"; person: string }
  | { kind: "no-person" }
  | { kind: "no-owner" };

/** OwnerStep is what a press leaves: the team chosen so far, and — when
 *  the press was an ANSWER — the pair to write. A team alone is not an
 *  answer (the menu stays open on it); a person without a team is refused;
 *  a team with no person, and no owner at all, are answers. */
export interface OwnerStep {
  team: string;
  answer: Owner | null;
  /** The press could not be taken: a person before a team. */
  refused?: string;
}

export function ownerStep(team: string, pick: OwnerPick): OwnerStep {
  switch (pick.kind) {
    case "team":
      return { team: pick.team, answer: null };
    case "person":
      if (!team) {
        return { team, answer: null, refused: "Pick a team first: a person is somebody on a team." };
      }
      return { team, answer: { team, person: pick.person } };
    case "no-person":
      if (!team) {
        return { team, answer: null, refused: "Pick a team first." };
      }
      return { team, answer: { team, person: "" } };
    default:
      return { team: "", answer: { team: "", person: "" } };
  }
}

/** peopleOfTeam orders a people list for a team: those with open work on
 *  that team first, in the order given, then everyone else — a board has
 *  no roster of who is on which team, so the work is the best reading. */
export function peopleOfTeam(
  people: readonly string[],
  team: string,
  cards: readonly Pick<Card, "team" | "assignees" | "stage" | "progress">[],
): string[] {
  const on = new Set<string>();
  for (const c of cards) {
    if (c.team === team) {
      for (const a of c.assignees) {
        on.add(a);
      }
    }
  }
  return [...people.filter((p) => on.has(p)), ...people.filter((p) => !on.has(p))];
}

/** ownerLabel is the owner as a row shows it: "team · person", the team
 *  alone, or a dash for nobody's. */
export function ownerLabel(o: Owner, name: (login: string) => string = (l) => l): string {
  if (!o.team && !o.person) {
    return "—";
  }
  return o.person ? `${o.team} · ${name(o.person)}` : o.team;
}
