# Writing a board's repositories directly

A board is files in git, so a tool can drive one **without this server** — committing to the repositories itself. This document is what such a writer has to know. It was written when the storage moved from GitHub Projects v2 to git repositories, which invalidated every `gh api graphql` write path against project items, fields and draft bodies: there is no project any more, only files. Its other reader is whoever changes these rules — what is listed here is what a direct writer will get wrong if the rules move and this page does not.

Everything below is final for this PR series and pinned by tests (`docs/design/behavior-matrix.md`, rows G1–G26, M1–M5); the API surface it lands with is in `docs/api.md`.

## What is gone

- The GitHub Project, its fields, single-select options and option ids. Nothing on a board is a GraphQL node any more.
- `PVTI_…` item ids. Cards, teams, projects, columns, deadlines, processes and tasks have **ULIDs** (26 Crockford-base32 characters). The migration kept the old item id in a card's `github:` front-matter key. The API and MCP tools accept a legacy `PVTI_` uid for one major version by looking it up through that key (matrix M5) — a convenience for old links, not something to build on: new state must name ULIDs.
- The draft-issue body as the note/event log. Notes live in the card file; **events are commits** — there is no event line to append.
- Issue and PR cards. A card that was an issue is a draft card with `link: <url>`; the issue itself is untouched.
- The `Status` field. Done and In Progress were never stored; they are derived (progress 100 with no stage; progress in (0, 100) with no stage).

## What a board is now

A board is one or more git repositories (**domains**), listed in order; the first is the **primary** and is what "" names. The server clones them shallow (`--depth 1`), reads the tree, and commits every action. A plugin that writes directly must clone (or fetch) the repository, edit files, commit with the trailers below, and push — the server picks pushed commits up on its fetch tick (15 s default) and re-applies its own unpushed commits over them field by field.

Repository layout (schema 1; `board.yaml` says `schema: 1`, a newer number is refused by older servers):

```
board.yaml                         # schema, title (primary only counts)
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

The path never encodes mutable state: renaming, re-zoning, re-teaming or moving a card in the order keeps its path. Empty directories do not exist (git); a delete removes the file.

## File formats

### Card (`cards/<a>/<b>/<id>.md`)

YAML front-matter between `---` fences, then the description, then an optional `## Notes` section. Empty fields are omitted. Unknown keys are preserved by the server; do not rely on their order.

Front-matter keys, in the order the server writes them: `title, assignees, author, team, zone, stage, progress, doneFrom, doneAt, start, day, sprint, plan, week, project, epic, mirrors, parent, reviewOf, reviewRound, recurrence, process, task, accumulate, link, github, movedFrom, movedAt, rank, created`.

- `zone`: `gray | green | yellow | red` (empty = none). `stage`: `review | locked | recurrent` (empty = none). `plan`: `wed | fri`.
- `progress`: 0–100; review/locked clamp to [10, 90] (S1, S7). `doneFrom` is written when progress reaches 100 (the value before) and cleared when it drops below (G3, G23); `Reopen` restores it. `doneAt` is written alongside — the board day (yyyy-mm-dd) the card reached 100 — and cleared the same way (P6); the personal board shows a done card that day and hides it the next.
- Dates are `yyyy-mm-dd`: `start` (startDate), `day` (end of the visible range), `sprint` (sprintStart), `week` (a Monday). Timestamps (`created`, `movedAt`) are RFC 3339 UTC.
- `assignees` is a YAML list of logins. `author` is the creator's login.
- `parent`, `reviewOf`, `task` are ULIDs of other files. `project`, `epic`, `team`, `process` are **names**, resolved against the roster on read.
- `rank` orders the card in its list (see Ordering). `created` is the creation time.
- `movedFrom` / `movedAt` appear on a card that was moved between domains (see Moves).
- `mirrors` is a YAML list of `{project, epic}` pairs — additional columns the SAME card stands in (one file, one log, one set of dates). The home `project`/`epic` pair keeps deciding the card's domain (linked cards first, as everywhere — a parent, reviewOf or task link outranks the pair, G14); every mirror must name a column of the card's own repository, and a rename of an epic or a project must rewrite matching mirror entries too. Because a link outranks the pair, a writer must never give a mirrored card a `reviewOf` that moves it to another repository, nor mirrors to a card whose link already holds its file elsewhere — the server refuses both. A subtask (`parent` set) carries no mirrors: grouping clears them.

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

- `teams/<id>.yaml`: `name`, `rank`, `created`, `sprint: {current, previous}` (dates). `teams/_.yaml` has no `name`.
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

## Domains

A card's domain (repository) is never chosen per card; it follows one rule, linked cards first (G14):

1. a review card (`reviewOf`) lives where the reviewed card lives;
2. a subtask (`parent`) where its parent lives;
3. an iteration (`task`) where its task lives;
4. else a card under a project (`project`/`epic`) where that project is declared;
5. else where its team is declared; the no-team group and anything unresolved → the primary.

Teams, projects and processes are declared in the domain the **caller** picks (API body field `domain`, MCP argument `domain`), default the primary; a process with a project lives with the project. A visitor sees the union of the domains they can read; a domain they cannot read is absent, not empty (G17). A plugin writing directly must respect where a file belongs: putting a closed project's card into the shared repository leaks it.

### Personal domains

`users/<login>.yaml` in the primary (`personal: <url>`, `created`) links a person to a repository of their own, served as the domain `~<login>` to that person alone (P1–P5). A plugin must treat such a repository as **pinned**: a card in it stays there whatever its `team:` or `project:` say — the home rule above does not apply — and a card whose `parent`/`reviewOf` is there belongs there too. Writing to someone's personal repository means holding their credential; the server never uses its own for it.

A personal board holds **cards only** (P9): no `teams/`, `projects/` or `processes/` entry belongs in one, and the server refuses a request to declare any there. A plugin writing a roster file into a personal repository produces something the board can never show.

A personal board has no carry-over, so **the reader turns its day over** (P7): a plugin listing the owner's personal board must first reseed every recurrent card (`stage: recurrent`, progress 100) whose cycle is due — a fresh card with the same title, zone and body, progress 0, `stage: recurrent`, the same `recurrence`, assigned to the owner, `start`/`day` = today — exactly as the server does, or the two will disagree on what the board holds. The turn is always as of the real today, whatever day is being looked at. The default cycle (`recurrence` empty) means **every day** there: due when `doneAt` is before today; `week`/`month` (and `2weeks`/`quarter`) are due when that much has passed since the card's `start` **and** `doneAt` is before today. Never reseed a card finished today, a card without `doneAt`, or one that already has a fresh copy (a recurrent card of the same title with a later `start` in the same repository). Done cards are hidden from the view the next day, never deleted. The view also holds back a card whose `start` is past the day of the read — planning there is dates alone (P8): the calendar and the defer move a personal card's `start`/`day` as on a team card, but write **no `sprintStart`**; a plugin re-dating a personal card must leave it sprint-less too. The × on a personal card is not a delete when the card has been worked on (P9): a card with progress above 0 that did not `start` today gets **`leftAt: <yesterday>`** — a new card field — on itself and on its subtasks, and the view holds a left card on its `leftAt` day and before, not after; an untouched card, or one that started today, is deleted. Re-dating a left card clears `leftAt` (on the subtasks too). A plugin removing or re-dating personal cards must follow the same rule, or a card the server would keep as history vanishes — or one it would hide keeps showing.

### Moves

A write that changes what the rule evaluates to is a **move**: the file is created in the new domain first — with `movedFrom: <old domain>`, `movedAt: <time>` and the `Aeman-Moved-From` trailer — then deleted in the old one (`Aeman-Moved-To`), same id, same `Aeman-Action-Id` (G22). A move cascades to the card's review card and subtasks. A card present in two domains is the copy whose `movedFrom` names the other; the other is a ghost that maintenance removes once the destination has landed.

## Behaviour the plugin used to replicate

Unchanged rules (dates, visibility, clamps, carry-over, review linkage, processes) are the same as before — see `docs/dates.md` and the D/S/A rows of the matrix. What changed is only **where they are written**:

- Carry-over: rewrite the team's `sprint` pointer and every carried card's dates in one commit (`Aeman-Action: carry-over`); a team already on today's sprint makes no commit; a team with nothing to carry still advances the pointer (G4).
- Sending to review: create the review card (`reviewOf`, the original's team, no project) in the original's domain and set the original's `stage: review`.
- Notes: append a `- <ulid> [ts] author — text` line under `## Notes`.
- Events: none to write. Set the fields; the commit is the event. For a change the diff cannot express (a review sent to someone, a reviewer removed), add an `Aeman-Change` trailer.

## The smart remove (A1/A2/W2)

The × no longer "never deletes". A card has two homes — the working area (a sprint and its days) and the weekly plan (a band and its week) — and each × empties one of them: leaving the working area clears assignee, sprint AND dates (a slot keeps its dates), leaving the plan clears the band. A card with the other home, or a Project-board column, stays there; a card with nowhere else to be is **deleted**, whatever it carries — the UI asks first when there is progress or a linked review card to lose; subtasks are freed into standalone cards. A plugin replicating the old rule would hand a card back into this week's plan (the server no longer does) or refuse a delete that now happens. `model.md` must carry the two-homes rule; a plugin driving `remove_card` should ask its user before removing a worked card from its last home.

## The API surface that changed with it

For a plugin that talks to a running server (REST or MCP) instead of the repository:

- **Board addressing.** A server serves one board — its configured repositories. The `owner` + `board` query parameters are gone (and ignored if sent); MCP tools take no `owner`/`board` (an optional `board` name is accepted and ignored under the server's lock). Breaking for any caller that addressed boards.
- **Card fields.** `metadata.contentId`, `isDraft`, `url`, `number`, `repository` left the card resource; `metadata` is `{uid, author, createdAt}`. `status.domain` names the repository the card lives in.
- **Members.** `GET /board` `metadata.members` is `[{login, avatarUrl}]`; `metadata.domains` lists the visitor's readable domains, primary first, each with a `writable` flag and the logins that can read it.
- **Log.** `GET /cards/{uid}/log` carries `truncatedBefore` when the loaded history is cut by the clone's horizon; events come from the commits, so a change made by a direct git write shows up too.
- **Domain choice.** `POST /projects`, `POST /processes` and `PATCH /sprints` take an optional `domain`; the MCP `add_project`, `add_process` and the team declaration take the same argument.

## The team/project pair (G46)

A card's team and its project must be declared in the SAME repository. The domain rule reads the project first, so a card filed under a project of one repository and handed to a team of another lives where the team's people cannot read it — the server refuses that pair (422) on every door: setting a team, filing under a column, creating a card, moving an epic between projects.

A plugin writing the repositories directly must refuse it too, or it will produce cards this server would never create: a founders card sitting in the shared repository under a founders team name, visible to everyone the shared repository is visible to. The check is local — resolve the team's and the project's declaring repository and compare — and a name no roster declares yet is not a conflict, since it is declared in the card's own repository on the way.

Over the API the two maps are served for exactly this: `GET /board` `metadata.teamDomains` and `metadata.projectDomains` name the repository of the entries outside the primary, so a client narrows its pickers instead of offering a pair the server rejects.

## What the plugin docs must change

`model.md`: replace the Projects v2 field/option model with the file formats above; state the domain rule and the move protocol; state that a card's team and project must live in one repository (G46); state that events are commits.

`reference.md`: replace every `gh api graphql` recipe with the git equivalent (clone/fetch, edit the file, commit with trailers, push) or with the aeman API/MCP call; replace `PVTI_` ids with ULIDs; document the rank rules and the iteration id derivation; document the board addressing once it lands.

Bump the plugin's version (major).
