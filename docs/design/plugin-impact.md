# Writing a board's repositories directly

A board is files in git, so a tool can drive one **without this server** — committing to the repositories itself. This page is the contract such a writer has to keep. The design behind it, and the reasoning for every shape here, is [git-backend.md](git-backend.md); the rules are pinned by [behavior-matrix.md](behavior-matrix.md) and the API surface is in [../api.md](../api.md). Its other reader is whoever CHANGES these rules: what is listed here is what a direct writer gets wrong if a rule moves and this page does not.

Everything else a writer needs is already written down elsewhere and is not repeated here: the dates, visibility, clamps, carry-over and review rules are in [../dates.md](../dates.md) and the matrix.

## The layout

Schema 3 (`board.yaml` says `schema: 3`; a newer number is refused by older servers). One repository is a **domain**; a board is one or more, the first being the primary.

```
board.yaml                         # schema, title (primary only)
teams/<ulid>.yaml                  # one team; teams/_.yaml is the no-team group (primary only)
cards/<a>/<b>/<ulid>.md            # a card; a, b = the id's LAST two characters, lower-cased
users/<login>.yaml                 # a person's roster entry: `capacity: N`, other keys kept (primary only)
.aeman/migration.yaml              # written once by aeman migrate
```

Schema 2 retired the Process feature (H8): a `processes/` tree is no longer part of the layout, and the server's start-up migration deletes it and strips the `process`, `task` and `accumulate` keys from cards in one `schema` commit. A writer meeting a schema-1 repository should not add to that tree — the next server start removes it; a card keeping one of the three keys is read all the same (they ride through as unknown keys) and loses them on the migration.

**The Project board is gone** (removed 2026-11-07, H15). `projects/` — its projects, their columns (`scopes/`, or `epics/` before schema 2) and deadline lines — and every card file whose front matter carries a non-empty `scope` (or `epic`, its old spelling) are no part of the model any more. This server leaves them in the repository exactly as they are and reads none of them: such a card is not on any board, in any listing or count, nor reachable by id, and a write addressed at it is refused as not found. A writer must not create them and must not expect them to show up; one that finds them may leave them be (this server never touches them). A `parent` naming a project card is a dangling link and is dropped on read.

The path never encodes mutable state: renaming, re-zoning, re-teaming or moving a card in the order keeps its path. Empty directories do not exist (git); a delete removes the file. A writer clones (or fetches) the repository, edits files, commits with the trailers below and pushes; the server picks pushed commits up on its fetch tick (15 s default) and re-applies its own unpushed commits over them field by field.

## File formats

### Card (`cards/<a>/<b>/<id>.md`)

YAML front-matter between `---` fences, then the description, then an optional `## Notes` section. Empty fields are omitted. Unknown keys are preserved by the server; do not rely on their order.

Front-matter keys, in the order the server writes them: `title, type, assignees, author, team, owner, customers, zone, size, stage, progress, doneFrom, doneAt, leftAt, start, day, sprint, parked, todo, parent, reviewOf, reviewRound, recurrence, link, github, movedFrom, movedAt, rank, created`.

`type` is the card's level (`docs/design/hierarchy.md`): absent for a task, else `goal`, `initiative` or `epic` — a PLANNING card. Written right after the title; a task writes no `type` line. A writer must not emit any other value (this server reads an unknown type as a task, the lowest level) and must not change a card's type after creating it.


Three keys are READ rather than written or preserved: `plan` (`wed | fri`), the weekly plan's band, retired in v0.20, `week` on a card, retired with schema 3 (`docs/design/backlog.md`, K7) — a task has no week, the server reads one as nothing on the card (the schema-3 bump and the importer read it to park a task that had nothing but a week), and the next write of the card drops the key — and `project` on a card that stands in no column, a label of the retired Project board (H15). An unknown key rides through untouched, so leaving it unknown would have every board re-commit a band nothing means; a writer should not emit it, and may leave an old one where it is — the next write to that card takes it off.

- `zone`: `gray | green | yellow | red` (empty = none). `stage`: `review | locked | recurrent | refuse` (empty = none) — the KEY, one letter from the word: a file written with `refused` carries a stage the board does not have, which this server now refuses to write and no board draws. `done` is not among them: a finished card is `progress: 100` with no stage. `size`: `S | M | L | XL` (empty = unsized) — the letter somebody said; the points (1/2/4/8) are derived and never written, a card with subtasks weighs them rather than itself, and a card with no `size` weighs M wherever a board sums points (B18).
- `progress`: 0–100; review/locked/refuse clamp to [10, 90] (S1, S7). `doneFrom` is written when progress reaches 100 (the value before) and cleared when it drops below (G3, G23); `Reopen` restores it. `doneAt` is written alongside — the board day (yyyy-mm-dd) the card reached 100 — and cleared the same way (P6). **A writer that closes a card must write it.** The day boards draw finished work by that day and no other (D2), and nothing is guessed from the card's dates, so a card closed without one stands on no day board at all. The exception is a day already gone: there the record is read from the history, and a card that the day OPENED with unfinished and CLOSED with done is taken as finished that day (G60) — which is also why a writer that leaves no `Aeman-Cards` trailer loses the card from the record of the day it closed it on: the trailer is what says which cards a day may have changed at all.
- Dates are `yyyy-mm-dd`: `start` (startDate), `day` (end of the visible range), `sprint` (sprintStart). A card carries no `week`.
- **`parked: true` puts a task in its team's BACKLOG** (`docs/design/backlog.md`). A task — no `type`, not a subtask, not a review card — is IN WORK (a `start`/`day` or a `sprint`) or PARKED, never both and never neither (K1). A writer parking a task clears its `assignees`, `start`, `day` and `sprint` (this server also moves it to the top of the backlog and sets `zone: gray`); a writer giving a parked task a date or a sprint must drop `parked`, or the card stands in its backlog and on no day board; a writer creating a task with no date and no sprint must write `parked: true`, or the task stands on no board at all (the Triage strip that used to draw it is gone). The backlog is ordered by `rank`, unranked cards last by `created`. Timestamps (`created`, `movedAt`) are RFC 3339 UTC.
- **`todo: true` marks a parked task READY** — the Roadmap's TO DO (`docs/design/backlog.md`, K15): the backlog reads TO DO tasks first, in the Roadmap's order among themselves. Written right after `parked`, omitted when unset; a writer taking a task into work (a date, a sprint or a person) or finishing it must drop the key, or a task reopened later comes back marked ready by a flag nobody saw. A backlog's order is the Roadmap's (K14): the path of places in the board's order from the root plan item down to the task — a writer reorders a task in its backlog by moving its `rank` among its siblings under the same parent.
- `assignees` is a YAML list of logins. `author` is the creator's login.
- `parent`, `reviewOf` are ULIDs of other files. `team` is a **name**, resolved against the roster on read.
- **`parent` is the ONE link of the hierarchy, and what it means is the parent's `type`** (H1, H2, `hierarchy.md`). Under a TASK the card is a SUBTASK: one level deep (a subtask's parent is not itself a subtask), and it takes its parent's `team`, first assignee and `sprint` — a writer grouping a card under a task must write those too, or the family splits across sprints and Me boards. Under a goal, an initiative or an epic the card is simply filed in the hierarchy and NOTHING else about it changes. A writer must keep the LEVEL RULE: the parent is of a higher level than the child (goal > initiative > epic > task; a task may skip levels), a goal has no parent, and a card has one parent at most — this server refuses the rest (422 `parentLevel`) and reads a cycle without hanging, but a reader of the repository is not so forgiving. A `parent` must be readable to every reader of the child: a planning parent in the PRIMARY repository (where every planning card lives), or any parent whose file stands in the child's own repository — anything else is refused (422 `crossDomain`); a dangling parent — a ULID that is not a card — is dropped when the board is read: the card stands as a root, where its own dates and team put it, and its next rewrite drops the key. A writer deleting a planning card should still re-parent what sat under it to that card's own parent (this server does), or the subtree falls out of the plan it belonged to.
- **A planning card** (a `type`) carries a `title`, `start`/`day` (its bar on the Roadmap), `parent`, `rank`, `created`, a body and notes, and MAY carry `team:` (the team that carries it out), `owner:` (one login: the person who answers for it, never inherited) and `customers:` (`docs/design/ownership.md`) — and NONE of `assignees`, `stage`, `progress`, `zone`, `size`, `parked`, `sprint`, `reviewOf`, `recurrence`. Its team places nothing, so it lives in the PRIMARY repository: a writer puts a goal, an initiative or an epic there and nowhere else, and an assignee written on one is ignored on read. This server refuses every write of those (422 `planningCard`) and derives the card's status from the tasks under it on every read; a writer must not emit them. The day boards do not draw a planning card at all; the Roadmap draws it and every card with one above it.
- `rank` orders the card in its list (see Ordering). `created` is the creation time.
- `movedFrom` / `movedAt` appear on a card that was moved between domains (see Moves).
- `mirrors` is no longer written (retired with schema 2, `hierarchy.md`). A file written before the migration may still carry the key; this server keeps it as an unknown key and the schema-2 migration strips it — a writer must not emit it.

Notes section:

```
## Notes

- <ulid> [<rfc3339 utc>] <author> — <text>
  <continuation lines are indented by two spaces>
```

Only the **last** `## Notes` heading in the file is the notes section; a description may contain the words freely. Note ids are ULIDs.

### Roster files (YAML)

- `teams/<id>.yaml`: `name`, `rank`, `created`, `sprint: {current, previous}` (dates) and `capacity: {points: N}` — the points a week somebody SET for the team; absent means nobody has, and no board derives one (B20). Unknown keys under `capacity:` are preserved. `teams/_.yaml` has no `name`.
- `customers/<id>.yaml`, in the PRIMARY repository only: `name`, `rank`, `created` — the board's customer roster (`docs/design/ownership.md`). A task with no `team:` of its own under a planning card that has one stands under that team on every day board and backlog and in that team's sprint (`docs/design/ownership.md`, O13): a writer leaves `team:` off to inherit it, writes it to override, and must not copy the plan item's team down the tree. Any card may name customers by id: `customers: [<id>, …]`, a flow list like `assignees`. A card's effective customers are its own plus every ancestor's; a writer never copies them down the tree. A writer deleting a customer must take its id off every card that names it in the same commit, and must not write a name where an id belongs: a rename is the roster file alone. An id the roster does not hold is read as no customer at all. `owner:` (a login) is read on a planning card only.

**A card never names a team the roster lacks** (G39): a write that gives a card a `team:` no `teams/<id>.yaml` declares must create that file (`name`, `rank`, `created`; no `sprint`) in the same commit — the server does so on every path (create, sprint-less create, assignment). A plugin writing `team:` onto a card must do the same, or the card sits on a team that is on no roster.

**Names are one namespace across every domain**: a team name may be declared once on the whole board, whichever repository it lands in — the server refuses a create or a rename into a taken name (G38) and refuses to start at all when the repositories it is given already collide, and a plugin writing files must check every domain before declaring one. Duplicate names that reach the files anyway resolve on read to the **oldest** `created` (ties by id); the others are aliases whose cards still count and which `GET /api/healthz` lists for a maintainer to rename (G13). A `teams/_.yaml` outside the primary is ignored. Renaming a team is one action: the team file's `name` changes and every card's `team:` follows in the same commit.

## Ordering

Every ordered list — cards, teams, customers — orders by `rank`, a LexoRank-style base-36 key that never ends in `0`, ties broken by id. To insert between two neighbours, pick any key strictly between them (the server uses the midpoint); to append, increment the last key. A key longer than 32 characters is exhausted: the server renumbers the whole list in one commit (G12). A plugin should do the same rather than write a longer key. On a board of several repositories the card order is ONE list across them: the top is a key below every repository's smallest card key, and a card inserted between two neighbours (K13, the insert "+") takes a key between theirs whichever repository each lives in.

**Review cards (K10).** A task with `stage: review` always has a review card — a file with `reviewOf: <the task's id>` and the title `review: <the task's title>`. With a reviewer named it carries them in `assignees` and is in work (a `start`/`day`, a `sprint`); with NOBODY it is `parked: true`, `assignees` empty, the reviewed task's `team`, `zone: gray`, ranked at the top of the order — the Backlog's "needs a reviewer" item. A writer that puts a task on review writes that card in the same commit (or names a reviewer); a writer that deletes a review card takes its task off review (no stage, progress kept within 10–90), since no task stays on review without its card; a writer that clears the reviewer of a task still on review parks the review card as above.

## Ids

ULIDs: 48-bit millisecond time, 80 random bits. Every card a writer creates gets a fresh one; the only derived form is the migration's (`aeman migrate`): derived from the old item id; not the plugin's concern.

## Commits

One action = one commit per touched repository. The message is a summary line, a blank line, then trailers:

```
Aeman-Action: <name>            # the action: update, create, delete, move, carry-over, note, …
Aeman-Action-Id: <ulid>         # shared by every commit of one action, across repositories
Aeman-Actor: <login>            # absent when the server acted on its own behalf
Aeman-Cards: <id> <id> …        # every card the commit touched
Aeman-Change: <card> <kind> <from> <to>   # one per change the file diff cannot express; "-" = empty
Aeman-Moved-From: <domain>      # on the destination commit of a move
Aeman-Moved-To: <domain>        # on the source commit of a move
```

Author = the actor (email from the server's `--author-email` template); committer = the server identity. A commit that changes nothing is not made. The card's activity feed **is** this history: every field change in a commit is one event with the commit's actor and time; a creation or deletion is one event; a move is the fields it changed. A plugin that writes without trailers still produces a correct feed (the diff says what changed) but without `Aeman-Actor` the event is attributed to the commit author.

**A day is read by COMMITTER time.** "The board as of that evening" is the newest commit whose committer time is at or before it, walked along first parents — so a commit must be stamped with the moment it is WRITTEN, not with the moment its content was authored. A replay (a rejected push re-applied) keeps the original author and takes a fresh committer time, as git's own rebase does. A writer pushing a commit back-dated in its committer field silently makes the record of that day read from another day's tree, with no error anywhere.

## Domains

A card's domain (repository) is never chosen per card; it follows one rule, linked cards first (G14):

1. a review card (`reviewOf`) lives where the reviewed card lives;
2. a subtask — a task whose `parent` is a TASK — where its parent lives; a `parent` that is a goal, an initiative or an epic places nothing (H6);
3. else where its team is declared; the no-team group and anything unresolved → the primary. A planning card is placed by this clause like any other card.

Teams are declared in the domain the **caller** picks (API body field `domain`, MCP argument `domain`), default the primary. A visitor sees the union of the domains they can read; a domain they cannot read is absent, not empty (G17). A plugin writing directly must respect where a file belongs: putting a closed repository's card into the shared one leaks it. (A step between 2 and 3 — a card under a project lived where the project was declared — went with the Project board on 2026-11-07.)

### Moves

A write that changes what the rule evaluates to is a **move**: the file is created in the new domain first — with `movedFrom: <old domain>`, `movedAt: <time>` and the `Aeman-Moved-From` trailer — then deleted in the old one (`Aeman-Moved-To`), same id, same `Aeman-Action-Id` (G22). A move cascades to the card's review card and subtasks. A card present in two domains is the copy whose `movedFrom` names the other; the other is a ghost that maintenance removes once the destination has landed.

## The × on a team card

**The grid × DELETES a team card** (P10). It used to demote — `start`, `day` and `sprint` moved to the previous sprint's start, with `leftAt: <today>` written beside them so a record of that day could give the card back. It does not any more: a card left alive in a sprint two behind is on no live board and no carry-over ever takes it, which is how the production board came to hold three hundred open cards nobody could see. The × now removes the card's file, together with the files of the subtasks that were pieces of it. The day's record is what keeps the card: reading a day gives the tree that day ended with PLUS every card file the day's own commits deleted, each from the commit that deleted it. A plugin doing this by hand deletes the file — writing `leftAt` on a team card is neither needed nor read. Cards demoted by older versions still carry the mark, and a record still gives those back by it.

## The team/project pair (G46) — removed

Removed 2026-11-07 with the Project board: there are no projects for a card's team to disagree with, and `metadata.projectDomains` is gone. `metadata.teamDomains` still names the repository each team outside the primary was declared in.
