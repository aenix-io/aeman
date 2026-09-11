# Writing a board's repositories directly

A board is files in git, so a tool can drive one **without this server** — committing to the repositories itself. This page is the contract such a writer has to keep. The design behind it, and the reasoning for every shape here, is [git-backend.md](git-backend.md); the rules are pinned by [behavior-matrix.md](behavior-matrix.md) and the API surface is in [../api.md](../api.md). Its other reader is whoever CHANGES these rules: what is listed here is what a direct writer gets wrong if a rule moves and this page does not.

Everything else a writer needs is already written down elsewhere and is not repeated here: the dates, visibility, clamps, carry-over and review rules are in [../dates.md](../dates.md) and the matrix, and a personal board's own rules in [personal-board.md](personal-board.md).

## The layout

Schema 1 (`board.yaml` says `schema: 1`; a newer number is refused by older servers). One repository is a **domain**; a board is one or more, the first being the primary.

```
board.yaml                         # schema, title (primary only)
teams/<ulid>.yaml                  # one team; teams/_.yaml is the no-team group (primary only)
projects/<ulid>/project.yaml       # a project
projects/<pid>/epics/<ulid>.yaml   # a column of that project
projects/<pid>/deadlines/<ulid>.yaml
processes/<ulid>/process.yaml
processes/<pid>/tasks/<ulid>.md    # a task: card-shaped file, title in the body's first line
cards/<a>/<b>/<ulid>.md            # a card; a, b = the id's LAST two characters, lower-cased
users/<login>.yaml                 # a person's link to their personal repository (primary only)
.aeman/migration.yaml              # written once by aeman migrate
```

The path never encodes mutable state: renaming, re-zoning, re-teaming or moving a card in the order keeps its path. Empty directories do not exist (git); a delete removes the file. A writer clones (or fetches) the repository, edits files, commits with the trailers below and pushes; the server picks pushed commits up on its fetch tick (15 s default) and re-applies its own unpushed commits over them field by field.

## File formats

### Card (`cards/<a>/<b>/<id>.md`)

YAML front-matter between `---` fences, then the description, then an optional `## Notes` section. Empty fields are omitted. Unknown keys are preserved by the server; do not rely on their order.

Front-matter keys, in the order the server writes them: `title, assignees, author, team, zone, size, stage, progress, doneFrom, doneAt, leftAt, start, day, sprint, week, project, epic, mirrors, parent, reviewOf, reviewRound, recurrence, process, task, accumulate, link, github, movedFrom, movedAt, rank, created`.

One key is READ and dropped rather than written or preserved: `plan` (`wed | fri`), the weekly plan's band, retired in v0.20. An unknown key rides through untouched, so leaving it unknown would have every board re-commit a band nothing means; a writer should not emit it, and may leave an old one where it is — the next write to that card takes it off.

- `zone`: `gray | green | yellow | red` (empty = none). `stage`: `review | locked | recurrent | refuse` (empty = none) — the KEY, one letter from the word: a file written with `refused` carries a stage the board does not have, which this server now refuses to write and no board draws. `done` is not among them: a finished card is `progress: 100` with no stage. `size`: `S | M | L | XL` (empty = unsized) — the letter somebody said; the points (1/2/4/8) are derived and never written, a card with subtasks weighs them rather than itself, and a card with no `size` weighs M wherever a board sums points (B18).
- `progress`: 0–100; review/locked/refuse clamp to [10, 90] (S1, S7). `doneFrom` is written when progress reaches 100 (the value before) and cleared when it drops below (G3, G23); `Reopen` restores it. `doneAt` is written alongside — the board day (yyyy-mm-dd) the card reached 100 — and cleared the same way (P6); the personal board shows a done card that day and hides it the next. **A writer that closes a card must write it.** The day boards draw finished work by that day and no other (D2), and nothing is guessed from the card's dates, so a card closed without one stands on no day board at all. The exception is a day already gone: there the record is read from the history, and a card that the day OPENED with unfinished and CLOSED with done is taken as finished that day (G60) — which is also why a writer that leaves no `Aeman-Cards` trailer loses the card from the record of the day it closed it on: the trailer is what says which cards a day may have changed at all.
- Dates are `yyyy-mm-dd`: `start` (startDate), `day` (end of the visible range), `sprint` (sprintStart), `week` (a Monday — the week the card is scheduled for, which is its column on the Triage board; for a card in a Project-board column it is the row, derived from `start`). Timestamps (`created`, `movedAt`) are RFC 3339 UTC.
- `assignees` is a YAML list of logins. `author` is the creator's login.
- `parent`, `reviewOf`, `task` are ULIDs of other files. `project`, `epic`, `team`, `process` are **names**, resolved against the roster on read.
- `rank` orders the card in its list (see Ordering). `created` is the creation time.
- `movedFrom` / `movedAt` appear on a card that was moved between domains (see Moves).
- `mirrors` is a YAML list of `{project, epic}` entries — the PROJECT half may be empty, since a column of no project is a home like any other and this server both writes and keeps such an entry; the epic half is what names the column — additional columns the SAME card stands in (one file, one log, one set of dates). The home `project`/`epic` pair keeps deciding the card's domain (linked cards first, as everywhere — a parent, reviewOf or task link outranks the pair, G14); every mirror must name a column of the card's own repository, and a rename of an epic or a project must rewrite matching mirror entries too. Because a link outranks the pair, a writer must never give a mirrored card a `reviewOf` that moves it to another repository, nor mirrors to a card whose link already holds its file elsewhere — the server refuses both. A mirror never duplicates the home pair and no pair appears twice, and a subtask (`parent` set) carries none at all; this server drops half-written entries, duplicates, home twins and subtask mirrors on read, and mirrors into another repository or onto an undeclared column when the board is assembled, so a writer producing them is silently corrected, not honoured. A `process` tie obeys the same domain closure from both sides: a writer must not point a card at a process of another repository, and must not move a tied card's file into one. A subtask (`parent` set) carries no mirrors: grouping clears them. The ONE column it may carry must name the repository that holds its file — which is its PARENT's repository, since a link outranks the project (G14); a column stays in the repository it was declared in whatever project owns it — a writer must not re-file a column's stub across repositories — and the column's own repository is the one its epic stub was declared in, which for the no-project bucket is the only thing there is to read. The same holds for a review card (its file follows its original), and it holds in both directions: a writer must not move a card whose subtasks or review card stand in columns of the repository it is leaving: a writer that files a subtask under a column of another repository produces a card the Project board there draws and counts while no reader of that repository holds its file, and the column cannot be deleted while it counts that card — clearing the card's column frees it, and this server repairs the state itself when a deleted parent releases such a child.

Notes section:

```
## Notes

- <ulid> [<rfc3339 utc>] <author> — <text>
  <continuation lines are indented by two spaces>
```

Only the **last** `## Notes` heading in the file is the notes section; a description may contain the words freely. Note ids are ULIDs.

### Task (`processes/<pid>/tasks/<id>.md`)

Same shape as a card. Its `title` field is the marker `aeman:process-task`; the task's **name is the first line of the body**, prefixed `# `; the rest of the body is the iteration's description. `recurrence`, `start` (the cycle anchor), `team`, `assignees`, `accumulate`, `rank` as before.

### Roster files (YAML)

- `teams/<id>.yaml`: `name`, `rank`, `created`, `sprint: {current, previous}` (dates) and `capacity: {points: N}` — the points a week somebody SET for the team; absent means nobody has, and no board derives one (B20). Unknown keys under `capacity:` are preserved. `teams/_.yaml` has no `name`. The NO-PROJECT bucket is the same shape one level down: its columns hang under a project file with **no `name`** — `projects/_/project.yaml` by convention, though any nameless project file is read as the bucket. This server writes it on demand, when the first column is filed outside every project or an existing column is unbound into the bucket; a writer adding such a column must create or reuse one.
- `projects/<id>/project.yaml`: `name`, `rank`, `created`.
- `projects/<pid>/epics/<id>.yaml`: `name`, `rank`, `created`. Column names are unique within a project.
- `projects/<pid>/deadlines/<id>.yaml`: `week`, `created`. One deadline per project per week.
- `processes/<id>/process.yaml`: `name`, `project` (name, optional), `paused`, `rank`, `created`.

**A card never names a team the roster lacks** (G39): a write that gives a card a `team:` no `teams/<id>.yaml` declares must create that file (`name`, `rank`, `created`; no `sprint`) in the same commit — the server does so on every path (create, sprint-less create, assignment). A plugin writing `team:` onto a card must do the same, or the card sits on a team that is on no roster and in no column.

**Names are one namespace across every domain**: a team, project or process name may be declared once on the whole board, whichever repository it lands in — the server refuses a create or a rename into a taken name (G38) and refuses to start at all when the repositories it is given already collide, and a plugin writing files must check every domain before declaring one. Duplicate names that reach the files anyway resolve on read to the **oldest** `created` (ties by id); the others are aliases whose cards still count and which `GET /api/healthz` lists for a maintainer to rename (G13). A `teams/_.yaml` outside the primary is ignored. Renaming a team is one action: the team file's `name` changes and every card's `team:` follows in the same commit.

## Ordering

Every ordered list — cards, teams, projects, a project's columns, processes, a process's tasks — orders by `rank`, a LexoRank-style base-36 key that never ends in `0`, ties broken by id. To insert between two neighbours, pick any key strictly between them (the server uses the midpoint); to append, increment the last key. A key longer than 32 characters is exhausted: the server renumbers the whole list in one commit (G12). A plugin should do the same rather than write a longer key.

## Ids

ULIDs: 48-bit millisecond time, 80 random bits. Two derived forms the plugin must reproduce to stay replica-safe:

- A **process iteration**: time = the due week's Monday, random bits = SHA-256 of `("iteration", task id, week)` (the server's `gitstore.IterationID`). Two writers spawning the same turn write the same path; the second create is a no-op.
- Migration ids (`aeman migrate`): derived from the old item id; not the plugin's concern.

## Commits

One action = one commit per touched repository. The message is a summary line, a blank line, then trailers:

```
Aeman-Action: <name>            # the action: update, create, delete, move, carry-over, sweep, note, …
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
2. a subtask (`parent`) where its parent lives;
3. an iteration (`task`) where its task lives;
4. else a card under a project (`project`/`epic`) where that project is declared;
5. else where its team is declared; the no-team group and anything unresolved → the primary.

Teams, projects and processes are declared in the domain the **caller** picks (API body field `domain`, MCP argument `domain`), default the primary; a process with a project lives with the project. A visitor sees the union of the domains they can read; a domain they cannot read is absent, not empty (G17). A plugin writing directly must respect where a file belongs: putting a closed project's card into the shared repository leaks it.

### Personal domains

`users/<login>.yaml` in the primary (`personal: <url>`, `created`, and `capacity: N` — the points a week somebody set for the person, absent meaning nobody has, B19) links a person to a repository of their own, served as the domain `~<login>` to that person alone (P1–P5). A plugin must treat such a repository as **pinned**: a card in it stays there whatever its `team:` or `project:` say — the home rule above does not apply — and a card whose `parent`/`reviewOf` is there belongs there too. Writing to someone's personal repository means holding their credential; the server never uses its own for it.

A personal board holds **cards only** (P9): no `teams/`, `projects/` or `processes/` entry belongs in one, and the server refuses a request to declare any there. A plugin writing a roster file into a personal repository produces something the board can never show.

A personal board has no carry-over, so **the reader turns its day over** (P7): a plugin listing the owner's personal board must first reseed every recurrent card (`stage: recurrent`, progress 100) whose cycle is due — a fresh card with the same title, zone and body, progress 0, `stage: recurrent`, the same `recurrence`, assigned to the owner, `start`/`day` = today — exactly as the server does, or the two will disagree on what the board holds. The turn is always as of the real today, whatever day is being looked at. The default cycle (`recurrence` empty) means **every day** there: due when `doneAt` is before today; `week`/`month` (and `2weeks`/`quarter`) are due when that much has passed since the card's `start` **and** `doneAt` is before today. Never reseed a card finished today, a card without `doneAt`, or one that already has a fresh copy (a recurrent card of the same title with a later `start` in the same repository). Done cards are hidden from the view the next day, never deleted. The view also holds back a card whose `start` is past the day of the read — planning there is dates alone (P8): the calendar and the defer move a personal card's `start`/`day` as on a team card, but write **no `sprintStart`**; a plugin re-dating a personal card must leave it sprint-less too. The × on a personal card is not a delete when the card has been worked on (P9): a card with progress above 0 that did not `start` today gets **`leftAt: <yesterday>`** — a new card field — on itself and on its subtasks, and the view holds a left card on its `leftAt` day and before, not after; an untouched card, or one that started today, is deleted. Re-dating a left card clears `leftAt` (on the subtasks too). A plugin removing or re-dating personal cards must follow the same rule, or a card the server would keep as history vanishes — or one it would hide keeps showing.

### Moves

A write that changes what the rule evaluates to is a **move**: the file is created in the new domain first — with `movedFrom: <old domain>`, `movedAt: <time>` and the `Aeman-Moved-From` trailer — then deleted in the old one (`Aeman-Moved-To`), same id, same `Aeman-Action-Id` (G22). A move cascades to the card's review card and subtasks. A card present in two domains is the copy whose `movedFrom` names the other; the other is a ghost that maintenance removes once the destination has landed.

## The × on a team card

**The grid × DELETES a team card** (P10). It used to demote — `start`, `day` and `sprint` moved to the previous sprint's start, with `leftAt: <today>` written beside them so a record of that day could give the card back. It does not any more: a card left alive in a sprint two behind is on no live board and no carry-over ever takes it, which is how the production board came to hold three hundred open cards nobody could see. The × now removes the card's file, together with the files of the subtasks that were pieces of it (a subtask standing in a column of its own is freed into that column instead). The day's record is what keeps the card: reading a day gives the tree that day ended with PLUS every card file the day's own commits deleted, each from the commit that deleted it. A plugin doing this by hand deletes the file — writing `leftAt` on a team card is neither needed nor read. Cards demoted by older versions still carry the mark, and a record still gives those back by it.

## A turn's week (G66)

A process iteration's `week` belongs to the OCCURRENCE it is a turn of: the weeks from the one its task came due in through the week before the next due date. A turn written outside that stands where the NEXT turn belongs, and the projection that asks whether an occurrence has a turn then answers wrongly for both — the same process reads as running twice in one cycle and not at all in the other. Two exceptions: a task with `accumulate` piles its turns up and bounds none of them, and a turn whose occurrence is already past may be brought forward into the CURRENT week (its days ran out, so it is drawn on no day board, and this is the only way back). A task with no calendar at all (per-sprint recurrence) has no occurrence, and its turn does not move in time.

## The team/project pair (G46)

A card's team and its project must be declared in the SAME repository. The domain rule reads the project first, so a card filed under a project of one repository and handed to a team of another lives where the team's people cannot read it — the server refuses that pair (422) on every door: setting a team, filing under a column, creating a card, moving an epic between projects.

A plugin writing the repositories directly must refuse it too, or it will produce cards this server would never create: a founders card sitting in the shared repository under a founders team name, visible to everyone the shared repository is visible to. The check is local — resolve the team's and the project's declaring repository and compare — and a name no roster declares yet is not a conflict, since it is declared in the card's own repository on the way.

Over the API the two maps are served for exactly this: `GET /board` `metadata.teamDomains` and `metadata.projectDomains` name the repository an entry was declared in — a server spanning several repositories names every entry, the primary's included, so read them as names rather than as "absent means primary", so a client narrows its pickers instead of offering a pair the server rejects.
