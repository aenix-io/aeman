# Git storage — impact on the companion plugin

The Claude Code plugin for aeman boards drives boards **without this server**, by replicating the domain rules documented in its `model.md` / `reference.md`. With the move from GitHub Projects v2 to git repositories, every write path the plugin has — `gh api graphql` mutations against project items, fields and draft bodies — stops working: there is no project any more, only files in a repository. This document lists what the plugin must know to keep driving boards, and what its two docs must say. It accompanies the storage PR; the plugin repository needs a paired PR and a version bump (breaking).

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
.aeman/migration.yaml              # written once by aeman migrate
```

The path never encodes mutable state: renaming, re-zoning, re-teaming or moving a card in the order keeps its path. Empty directories do not exist (git); a delete removes the file.

## File formats

### Card (`cards/<a>/<b>/<id>.md`)

YAML front-matter between `---` fences, then the description, then an optional `## Notes` section. Empty fields are omitted. Unknown keys are preserved by the server; do not rely on their order.

Front-matter keys, in the order the server writes them: `title, assignees, author, team, zone, stage, progress, doneFrom, start, day, sprint, plan, week, project, epic, parent, reviewOf, reviewRound, recurrence, process, task, accumulate, link, github, movedFrom, movedAt, rank, created`.

- `zone`: `gray | green | yellow | red` (empty = none). `stage`: `review | locked | recurrent` (empty = none). `plan`: `wed | fri`.
- `progress`: 0–100; review/locked clamp to [10, 90] (S1, S7). `doneFrom` is written when progress reaches 100 (the value before) and cleared when it drops below (G3, G23); `Reopen` restores it.
- Dates are `yyyy-mm-dd`: `start` (startDate), `day` (end of the visible range), `sprint` (sprintStart), `week` (a Monday). Timestamps (`created`, `movedAt`) are RFC 3339 UTC.
- `assignees` is a YAML list of logins. `author` is the creator's login.
- `parent`, `reviewOf`, `task` are ULIDs of other files. `project`, `epic`, `team`, `process` are **names**, resolved against the roster on read.
- `rank` orders the card in its list (see Ordering). `created` is the creation time.
- `movedFrom` / `movedAt` appear on a card that was moved between domains (see Moves).

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

**Names are one namespace across every domain**: a team, project or process name may be declared once on the whole board, whichever repository it lands in — the server refuses a create or a rename into a taken name (G38), and a plugin writing files must check every domain before declaring one. Duplicate names that reach the files anyway resolve on read to the **oldest** `created` (ties by id); the others are aliases whose cards still count and which `GET /api/healthz` lists for a maintainer to rename (G13). A `teams/_.yaml` outside the primary is ignored. Renaming a team is one action: the team file's `name` changes and every card's `team:` follows in the same commit.

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

### Moves

A write that changes what the rule evaluates to is a **move**: the file is created in the new domain first — with `movedFrom: <old domain>`, `movedAt: <time>` and the `Aeman-Moved-From` trailer — then deleted in the old one (`Aeman-Moved-To`), same id, same `Aeman-Action-Id` (G22). A move cascades to the card's review card and subtasks. A card present in two domains is the copy whose `movedFrom` names the other; the other is a ghost that maintenance removes once the destination has landed.

## Behaviour the plugin used to replicate

Unchanged rules (dates, visibility, clamps, carry-over, smart remove, review linkage, processes) are the same as before — see `docs/dates.md` and the D/S/A rows of the matrix. What changed is only **where they are written**:

- Carry-over: rewrite the team's `sprint` pointer and every carried card's dates in one commit (`Aeman-Action: carry-over`); a team already on today's sprint makes no commit; a team with nothing to carry still advances the pointer (G4).
- Sending to review: create the review card (`reviewOf`, the original's team, no project) in the original's domain and set the original's `stage: review`.
- Notes: append a `- <ulid> [ts] author — text` line under `## Notes`.
- Events: none to write. Set the fields; the commit is the event. For a change the diff cannot express (a review sent to someone, a reviewer removed), add an `Aeman-Change` trailer.

## The API surface that changed with it

For a plugin that talks to a running server (REST or MCP) instead of the repository:

- **Board addressing.** A server serves one board — its configured repositories. The `owner` + `board` query parameters are gone (and ignored if sent); MCP tools take no `owner`/`board` (an optional `board` name is accepted and ignored under the server's lock). Breaking for any caller that addressed boards.
- **Card fields.** `metadata.contentId`, `isDraft`, `url`, `number`, `repository` left the card resource; `metadata` is `{uid, author, createdAt}`. `status.domain` names the repository the card lives in.
- **Members.** `GET /board` `metadata.members` is `[{login, avatarUrl}]`; `metadata.domains` lists the visitor's readable domains, primary first, each with a `writable` flag and the logins that can read it.
- **Log.** `GET /cards/{uid}/log` carries `truncatedBefore` when the loaded history is cut by the clone's horizon; events come from the commits, so a change made by a direct git write shows up too.
- **Domain choice.** `POST /projects`, `POST /processes` and `PATCH /sprints` take an optional `domain`; the MCP `add_project`, `add_process` and the team declaration take the same argument.

## What the plugin docs must change

`model.md`: replace the Projects v2 field/option model with the file formats above; state the domain rule and the move protocol; state that events are commits.

`reference.md`: replace every `gh api graphql` recipe with the git equivalent (clone/fetch, edit the file, commit with trailers, push) or with the aeman API/MCP call; replace `PVTI_` ids with ULIDs; document the rank rules and the iteration id derivation; document the board addressing once it lands.

Bump the plugin's version (major).
