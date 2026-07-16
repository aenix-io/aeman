import type { Note } from "./providers/types";

/** mergeNotes overlays a server note list with local optimistic notes the
 *  response predates: a tmp- note not yet visible upstream (matched by body
 *  and author) survives until its own request's response carries it back. */
export function mergeNotes(server: Note[], local: Note[] | undefined): Note[] {
  const extras = (local ?? []).filter(
    (n) =>
      n.id.startsWith("tmp-") &&
      !server.some(
        (s) => s.body === n.body && (s.author ?? "") === (n.author ?? ""),
      ),
  );
  return extras.length ? [...server, ...extras] : server;
}

/** sameNotes reports whether two note lists are content-identical (same ids
 *  and bodies in the same order) — used to skip no-op state updates. */
export function sameNotes(a: Note[] | undefined, b: Note[]): boolean {
  const x = a ?? [];
  return (
    x.length === b.length &&
    x.every((n, i) => n.id === b[i].id && n.body === b[i].body)
  );
}
