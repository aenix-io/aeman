# Card dependencies ("depends on")

Status: decided 2026-11-08, owner-approved. Matrix rows N1–N8 in
`behavior-matrix.md`.

A card can say it is WAITING for other cards: "this cannot start before
that is done". The board derives a lock from it, shows who is waiting for
whom, and passes the customers of the waiting work on to the work it
waits for. Nothing is enforced: a locked card can still be planned, moved,
taken and finished — the lock is information, not a gate.

## The field

- `dependsOn` — a list of card uids, on a task or a plan item (goal,
  initiative, epic). Stored in the card file as the front-matter key
  `dependsOn: [<ulid>, …]` in the order given, each id once. The codec
  keeps unknown keys as before; a file without the key has no
  dependencies.
- Wire: `spec.dependsOn` (array of uids, omitted when empty) on every card
  resource. It is the card's OWN list as stored, minus ids the board does
  not hold (dangling) — see "Dangling" below.

### What may be a target

A dependency of card X on card T is refused (422 `dependencyTarget`)
when:

- T is not on the board (unknown uid);
- T is X itself;
- T is a review card, or X is a review card (a review follows the task it
  reviews; it neither waits nor is waited for);
- T is an ancestor of X or a descendant of X (a plan item is done when the
  work under it is done, so "the epic waits for its own task" or "the task
  waits for its own epic" is meaningless).

### Cycles

A dependency that would close a cycle is refused (422 `dependencyCycle`).
The graph the check walks has an edge X → Y when:

- Y is in X's `dependsOn`;
- Y is in the `dependsOn` of an ancestor of X (X inherits that wait);
- X is a plan item and Y is a card under it (X is done only when Y is).

Adding X → T is refused when X — or any card under X, which inherits the
wait being added — is reachable from T in that graph (direct A→B→A and
transitive A→B→C→A alike, A→epic B whose task waits for A, and epic E→T
when T already waits for a task under E).
The walk visits each card once, so it ends on any board, including one a
direct writer left cyclic.

## Satisfied, blocked, locked

- A dependency is SATISFIED when its target is DONE: a task when it is
  complete (`board.Complete`); a plan item when its roll-up is done (every
  task under it complete, `Index.HierarchyStatus` = done). A plan item with
  no tasks is not done, so a dependency on it is unsatisfied.
- A card's BLOCKERS are its own unsatisfied dependencies plus those of
  every ancestor (any level: goal, initiative, epic, a subtask's task) —
  the wait is inherited down the tree like the effective team and
  priority. Each blocker carries the card whose `dependsOn` names it
  (`via`): the card itself, or the ancestor.
- A card is LOCKED when it has at least one blocker. LOCKED is derived on
  every read and never written. It is independent of the manual
  `stage: locked`, which stays what it was (a person's stage).
- LOCKED is a badge, not a work status: the Roadmap's status filter (O14)
  keeps reading placement, the plan roll-up (`status.hierarchy`) counts a
  locked task by its placement, and every board still lets the card be
  planned, moved and taken.

### Wire

- `status.locked` (bool, omitted when false) — the derived lock.
- `status.blockedBy` — the blockers the visitor may read, board order of
  the target: `[{uid, title, type, via}]`, where `via` is the uid of the
  card whose `dependsOn` names the blocker, omitted when it is the card
  itself.
- `status.hiddenBlockers` (int, omitted when 0) — blockers the visitor may
  not read: the target is in a repository they cannot read, or the wait is
  inherited through an ancestor they cannot read. They count (the card is
  locked) but carry no uid or title.
- `status.blocks` — the cards that wait for THIS one directly (their
  `dependsOn` names it), readable ones only: `[{uid, title, type}]`, board
  order. Drives the drawer's read-only "Blocks" list.

A visitor's resource is computed as their listing computes it, on their
visible board (G17); the server records, while projecting, how many
blockers each card has on the whole board, and `hiddenBlockers` is the
difference. A watch frame is built on the whole board and then counted
again for the subscriber, giving the same answer as their listing.

## Customers follow the waiting work

A dependency TARGET is for the customers of the work that waits for it:

- `Index.EffectiveCustomers(T)` = its ancestors' and its own (O4) PLUS the
  effective customers of every card whose `dependsOn` names T or one of
  T's ancestors — transitively (A waits for B, B waits for C: C is for A's
  customers too), each card visited once (cycle-safe, ends on any board).
  Customers that flow down the tree (O4) include those an ancestor got
  this way: a plan item a customer's work waits for passes that customer
  to the tasks under it.
- Only readable cards pass customers on (the visible board, as O4's
  readable-ancestors rule): a hidden dependent passes nothing.
- The customer filters (Roadmap O7/O14, day boards O11) read
  `effectiveCustomers`, so they include these.
- Wire: `status.effectiveCustomers` includes them; `status.dependentCustomers`
  names the ones that arrive ONLY through a dependency, with where from:
  `[{customer, from: {uid, title, type}}]` — `from` is the nearest card that
  waits for this one (or for the ancestor that brought it). The drawer
  draws them greyed, "from «A»".

## Dangling

A uid in `dependsOn` the board does not hold (the target was deleted, or
lives in a repository nobody configured) is ignored on read: it is no
blocker and is not in `spec.dependsOn`. Deleting a card never rewrites the
cards that depend on it. The dangling id leaves the file the next time
that card's dependency list is written (an add, a remove, a PATCH of
`spec.dependsOn`); a write of another field leaves the list as it is (the
store's field writers do not read the board).

## Writing it

- REST: `PATCH /cards/{uid}` with `spec.dependsOn` — the full list,
  replacing the stored one; `[]` clears it. Every new id is checked
  (target, cycle) before anything is written; ids already on the list that
  have since become invalid are kept (they were valid when written).
  Refusals: 422 `dependencyTarget`, 422 `dependencyCycle`.
- The check runs on the WHOLE board, never on a visitor's: their board
  breaks a lineage at a card they cannot read and ends a cycle's walk
  there. The service checks first on the caller's board and names the id
  it refuses; the backend checks again on the whole board and refuses
  without naming anything, since the card that decides may be hidden. The
  server's store makes that check against its cache under the write lock,
  where writes are put in order, so two requests that each read the board
  before the other's write cannot close a cycle between them.
- A visitor's list is the readable part. A dependency on a card they
  cannot read stays on the card in the place it held; their ids fill the
  readable places in their order. The list is compared with the card's
  readable part as a SET: reordering, or leaving out what they cannot
  name, writes nothing and logs nothing. The event names only ids the
  caller's board holds.
- MCP: `update_card` takes `dependsOn` (array of uids, full list) with the
  same meaning; its description explains the derived lock and the customer
  inheritance. ADR 0002 parity: no separate tools.
- Service: `boardservice.Service.SetDependsOn(ctx, board, uid, ids)`; the
  backend half is the optional `boardservice.DependencyBackend`
  (`SetDependsOn`), as customers are `OwnershipBackend`'s.
- The change is one commit on the card's own file and is logged as a
  `depends-on` event (from → to, comma-joined uids the caller's board
  holds; a dangling id is no card and is not named).

## Watch

- Changing a card's `dependsOn` sends a frame for the card, for every card
  under it (their inherited lock and customers changed), and for every
  target added or removed and the cards under those (their `blocks` and
  inherited customers changed).
- A target becoming done or undone (progress, stage, a task under a plan
  item finishing) sends a frame for every card whose blockers include it:
  its direct dependents and everything under them.
- Frames are redacted per subscriber like the listing (`seen`):
  `blockedBy`, `hiddenBlockers`, `blocks`, `effectiveCustomers` and
  `dependentCustomers` are counted again over what the subscriber reads.

## External writers

A writer may set `dependsOn` in a card file (a list of ULIDs). It need not
check targets or cycles — the server ignores what it cannot honour on
read (dangling ids; a cycle only locks the cards in it) — but the server's
own writes refuse both, so a writer should too.
