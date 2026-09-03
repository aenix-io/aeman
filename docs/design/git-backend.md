# Git as the storage backend

aeman stores boards in GitHub Projects v2 today. This document replaces
that with git repositories: a board is a set of files in a repository,
every change is a commit, and the forge — GitHub, GitLab, Gitea, or a
bare repository on a server — is just where the repository lives. The
old backend is removed, not kept as an option.

The measurements this design rests on are in
[git-backend-research.md](git-backend-research.md). Read that first if a
number below looks surprising.

## Why

Board 37 — the live board — has 2481 draft cards, 7 issues and no pull
requests among 2488 items. Nothing that makes Projects v2 worth its cost
(issues, pull requests, a link to code, a second UI) is in use. What it
costs, counting production code only: `pkg/ghprojects` is 3316 lines
mapping our domain onto someone else's field model, and
`internal/server/boardstore.go` is 2603 lines hiding a backend that
takes tens of seconds to answer. The pure domain, `pkg/board`, is 1704
lines. We spend three times more code working around the store than we
spend on the rules.

Against a git clone the same board loads in **2.6 s cold** (shallow
clone from GitHub) instead of ~50–110 s, reads in **~100 ms**, and takes
a write in **1.5 ms**. And a whole class of problems disappears by
construction: option ids that regenerate, a 200-line cap on the event
log, "the cache lost my card" — a commit is durable, attributed, and
ordered, and there is nothing to reconcile between "what we wrote" and
"what the store shows".

## Model

### Repository = domain, board = domains

A **board** is an ordered list of **domains**. A domain is one git
repository. The first domain is the **primary**: it holds the board's
own file (`board.yaml`) and is the domain everything falls into when
there is only one.

Several domains exist for one reason: **visibility**. A user sees the
domains they can read — that is decided by the forge, not by us — and
the board they get is the union. A closed project lives in a second
repository that four people can clone; everybody else's board simply
does not contain it. Authorization is thereby handed back to the forge,
which is where it already lives for the code.

A domain has a **name** — the one given on the command line (`--repo
name=url`) — and every entry the reader hands over is stamped with the
name of the repository it was read from, the primary's included. Nothing
on disk carries that stamp: it is where the file *is*. A reader that
leaves it out for the primary, or a rule that compares a stamped name
against an empty one, has two names for one repository — and every "same
repository?" question (the domain rule below, mirrors, the column
guards) then answers no where it should answer yes. In this codebase the
board carries its primary's name (`board.Board.Primary`) and every
domain reader normalizes through it, so an unstamped entry and the
primary are one answer; "nothing declares this" stays a separate,
empty answer that decides nothing.

### One axis of inheritance

Every object lives in exactly one domain, and nothing is chosen per
card. A card's domain is decided by one rule, evaluated in this order —
**linked cards first**, because a linked card has no domain of its own:

1. a **review card** (`reviewOf` set) lives where the card it reviews
   lives — a review is part of the reviewed work. Its `team` is the
   original's team and its `project` is empty (`sendToReview`), which is
   exactly why this rule must come before the team rule: a review of a
   closed-project card would otherwise land in the shared team
   repository;
2. a **subtask** (`parent` set) lives where its parent lives, whatever
   column it carries itself;
3. a process **iteration** (`task` set) lives where its task lives,
   which is where the task's process lives;
4. an unlinked card filed under a **project** (an epic column, or a
   `project` without one) lives where that project lives;
5. any other card lives where its **team** lives (the no-team group is a
   team for this purpose and lives in the primary domain).

Project before team because that is what a closed project needs: a
closed project's cards must not sit in a shared team repository, or the
closed project is not closed. A project that owns a column is the unit
of sensitivity; a team is an organizational unit that spans projects.

Teams and projects are declared in the domain the user picks when
creating them — asked only when more than one writable domain exists,
labelled by access ("shared — everyone", "closed — 4 people"), never by
repository name. A **process** that belongs to a project lives with that
project and asks nothing; a process without a project (allowed today,
`AddProcess` takes an empty project) is declared like a team, in a
picked domain. Epics and deadlines live with their project; tasks with
their process.

Teams normally live in the **primary** domain. A team declared in a
closed domain is visible only to that domain's readers; the server,
which reads every domain, still evaluates its sprint pointer for
everyone's visibility rules, so filtering does not depend on who is
looking.

### Moving between domains

A write that changes what rule 1–5 evaluates to — filing a team card
under a closed project, taking a closed card out of its project, moving
a team or a project to another domain — is a **move**. A move is not a
rename inside one repository (there is no such thing across two); it is
a create in the new domain and a delete in the old one, **with the same
id**, in two commits that share one `Aeman-Action-Id`; the create
carries `Aeman-Moved-From: <domain>`, the delete `Aeman-Moved-To:
<domain>`. The card keeps its history: the log walker follows
`Aeman-Moved-From` into the old domain (within the horizon) and shows
one continuous log. A move of a card **cascades** along the links whose
files FOLLOW it — rules 1 and 2: its review card and its subtasks move
with it, in the same action, and so does whatever follows THEM. Rule 3
is not one of them: an iteration's task link never moves, since a turn
cannot be re-tied.

**Create before delete, always.** The destination commit is written to
disk first, then the source delete; a crash between the two leaves a
duplicate, never a missing card. The moved file records where it came
from in its front-matter — `movedFrom: <domain>`, `movedAt: <time>` —
because state must be readable from the tree alone: a fresh depth-1
clone has no commit history to consult. A torn move — one push landed,
the other not yet — therefore resolves without history: a card present
in two domains is current in the one whose file says `movedFrom: <the
other>`, and the other file is a ghost, hidden by every reader and
deleted by the next maintenance tick once the destination has been
pushed. Until both commits are pushed, the cache is authoritative for
every reader of this replica, and both commits are on disk.

Mass moves (a team or a project to another domain) are one explicit
action with one `Aeman-Action-Id` across two domains, never a drag.

### References never cross a domain boundary

A card may carry `mirrors:` — a YAML list of `{project, epic}` entries, the project half optionally empty (a column of no project) — and
then stands in every named column as well as its own: the same file, the
same log, the same dates, shown in more than one project. The home pair
(`project:`/`epic:`) keeps deciding everything beyond being shown — the
domain rule reads it, never a mirror — and every mirror must name a column
of the card's own repository. A writer renaming an epic or a project must
rewrite matching mirror entries the same way it rewrites the home fields.

The domain is full of links — `parent`, `reviewOf`, `task`, the
per-team sprint pointer, and the card mirrors (`mirrors:`). A link that crosses a visibility boundary
would show one side an orphan, exactly the "card vanished" class of bug.
Each link is closed by its own mechanism, so an orphan cannot be created:

| link | how it stays inside |
| --- | --- |
| `parent`, `task`, `reviewOf` | the inheritance rule above places the child where the referenced card is |
| `team` on a project-domain card | the one reference that legitimately points across: a closed project's card names a team whose file lives in the primary. The **server** reads every domain, so the visibility rules (`CurrentSprint(b, team)` in `MeView`/`TeamGrid`, `sendToReview`, carry selection) see the pointer for everyone. For a **visitor**: the primary domain must be readable or there is no board at all (403 — `board.yaml` lives there); a card whose team file the visitor cannot read is still shown, under its team name, with the team's controls (sprint, carry) unavailable to that visitor |
| `mirrors` (for card mirrors) | a guard in `boardservice.Mirror`: the target column must be in the same domain, `ErrCrossDomain`. Pinned in behavior-matrix G15: home column required, the home itself refused, the target must exist, then the same-repository check; already mirrored is a no-op |
| the reviewer | not a stored link but a choice: the reviewer picker offers only people who can read the card's domain. You cannot review what you cannot see; the UI says so instead of failing later |

Splitting goes by **sensitivity**, never by team or by project as such:
`sendToReview` gives the review card the *reviewer* as assignee and the
*original's* team, so team-split boards would orphan every cross-team
review; mirrors link columns of different projects by definition.

### Rosters are fragments, merged on read

Teams and projects are declared by files in their own domain — a
centralized roster would leak the name of a team created in a closed
domain to everyone. Order is a **rank key** on each file (see
"Ordering"); rank keys are comparable across domains, so the merged
roster has one consistent order without any central file.

Names are unique across the board: creation checks the merged roster
the creator can see. Two writers, or a writer who cannot see a domain
where the name already exists, can still race a duplicate into being.
The reader resolves duplicates deterministically: the file with the
oldest `created` is the team (or project, or process) — its rank and
attributes win — and the others are **aliases** of it: their cards
count as its cards, an alias project's epics and deadlines show as the
winner's columns and lines, nothing is dropped. `/api/healthz` reports the
duplicates so a maintainer can merge them by hand; the server never
renames or deletes on its own.

## Layout

The one principle: **a path never encodes mutable state.** A state
change that moved a file would be a rename — more conflicts, a broken
`git log --follow`, and a commit that says nothing. Paths carry identity;
files carry state. The only "move" is the cross-domain move above, and
it is a delete plus a create, named as such.

```
board.yaml                         primary domain only: schema, title
teams/<id>.yaml                    one team: name, rank, sprint pointers
projects/<id>/project.yaml         one project: name, rank; a file with no
                                   `name` is the NO-PROJECT bucket, written
                                   on demand (`projects/_/project.yaml`)
projects/<id>/epics/<id>.yaml      one column: name, rank
projects/<id>/deadlines/<id>.yaml  one deadline line: week
processes/<id>/process.yaml        one process: name, project, paused, rank
processes/<id>/tasks/<id>.md       one task: front-matter + body
cards/<a>/<b>/<id>.md              one card: front-matter + body
```

Every `<id>` is a **ULID** minted by the server (26 chars, Crockford
base32, time-ordered). Cards are sharded under `cards/<a>/<b>/` where
`a` and `b` are the **last two characters** of the id, one per level.
The research fixes this shape:

- Every commit rewrites the tree object of each directory on the changed
  file's path. A flat `cards/` with 2388 entries is a 140 KB tree
  rewritten 20 000 times: 4.8 ms per commit, 189 MiB of history. So
  every directory that changes per commit stays small.
- Measured on the same 20 169-commit replay: one level of 16 shards
  rewrites ~9 KB of trees per commit (34 MiB pre-gc history); two
  levels of 32 rewrite ~1.2 KB but add one tree object per commit (+20 %
  objects, 40 MiB pre-gc). The two-level layout buys **scale**: at
  10 000 cards a single level has 19 KB leaf trees — the size that
  produced a 170 MiB history in the research — while two levels stay
  flat. Shard depth is part of the schema version, so it can change
  with a migration if a board ever proves the trade wrong.
- The *tail* of the id, not the head: a ULID's head is a timestamp, so
  head-sharding puts every card of the month in one directory.

The hidden `aeman:*-state` cards stop existing. They were configuration
disguised as cards because Projects v2 had nowhere else to put it; here
it is configuration.

### Files

A card:

```markdown
---
title: Carry-over ignores a deferred card
assignees: [kitsunoff]
author: kvaps
team: portal
zone: yellow
stage: locked
progress: 40
doneFrom: 0
start: 2026-08-26
day: 2026-08-28
sprint: 2026-08-24
week: 2026-08-24
project: portal
epic: Bugs
parent: 01JB4K2E7QZMX3R8V0N5T9WYC1
reviewOf: ""
reviewRound: 0
recurrence: ""
process: ""
task: ""
accumulate: false
link: https://github.com/aenix-io/cozystack/issues/1234
github: PVTI_lADOCiHiS84Bbkpzzg4EvCo
rank: a0m
created: 2026-08-26T09:14:03Z
---

Free-form description, Markdown.

## Notes

- 01JB4K9P2R5T7VXY8Z0A1B2C3D [2026-08-26T10:02:11Z] kitsunoff — reproduced on board 37
```

Rules:

- Empty fields are omitted on write (the example shows them for the
  schema). Unknown keys are preserved on rewrite — a newer server must
  not strip what an older one does not know.
- `zone` and `stage` are the domain keys (`red|yellow|green|gray`,
  `locked|review|done|recurrent`), not display names. Derived states
  (In Progress, Done-by-100%) are **not stored**, exactly as today.
- `doneFrom` is the progress the card had when a write took it to 100:
  written by that write — the slider, or a review passing (`applyStage`
  takes 90 → 100, so `doneFrom: 90`) — and cleared by a reopen. `Reopen`
  reads it. Today it walks the card's event log for the last jump to
  ≥100 (`service.go` `Reopen`), and a log behind a time horizon cannot
  be relied on for a value the action depends on. Stored, it is
  horizon-independent and costs nine bytes. The migration seeds it for
  every card that is done at migration time, by the same walk `Reopen`
  does today, so no card regresses to the nudge on its first reopen.
- `movedFrom` / `movedAt` name the domain a card was moved out of and
  when (see "Moving between domains"); absent on a card that never
  moved.
- `team`, `project`, `epic`, `process` are **names**, as in the domain
  and the API (a column is `(project, epic)`). Renaming an epic rewrites
  every card that names it plus the epic file — in **one commit**, which
  is what today's `RenameEpic` does across N GraphQL calls.
- `parent`, `reviewOf`, `task` are **ids**.
- `link` is the only trace of the 7 issue-backed cards: a URL in the
  card, nothing else. There is no issue/PR integration.
- `github` is the Projects v2 item id the card was migrated from, kept
  for traceability and for the legacy-id lookup (below). Absent on cards
  born after the migration.
- `rank` is the ordering key (below). `created` is the creation time.
- Notes live in the body under `## Notes`, one list item each, first
  token the note's ULID, then the timestamp, the author, an em dash, the
  text; continuation lines are indented two spaces. The id is what the
  notes API addresses; it never changes on edit.
- **Events are not in the file.** A commit is the event.

Fields that existed only to round-trip GitHub are deleted:
`Card.ContentID`, `IsDraft`, `ZoneOptionID`, `EventLogID`, `URL`,
`Number`, `Repository`, `State`, `SprintTitle`, `Status`, and
`Board.Fields`, `ID`, `Number`, `URL`, `Owner`. `Card.Events` goes too —
the log is read from history (below). `Card.ItemID` stays as the field
name for the ULID so the API's `uid` does not change meaning.

A team:

```yaml
name: portal
rank: a0
created: 2026-06-01T08:00:00Z
sprint:
  current: 2026-08-24
  previous: 2026-08-21
```

A project, an epic, a deadline, a process, a task follow the same
pattern: a name where it has one, a `rank`, a `created`, the fields the
domain already gives them (`Deadline.Week`, `Process.Paused`, task
front-matter = card front-matter minus the placement fields). The
no-team group is the team file named `_.yaml` in the primary domain.

`board.yaml`, primary domain only:

```yaml
schema: 1
title: aeman board
```

`schema` is the layout version; the server refuses a repository whose
schema is newer than it knows and migrates one that is older, in a
commit.

### First start

A board begins with `aeman init --repo <url>`: it writes `board.yaml`
and `teams/_.yaml` in one commit and pushes it. `aeman serve` against
an unborn or empty repository refuses to start and names that command —
go-git reports an unborn remote as `ErrEmptyRemoteRepository`, and a
server that silently invents a board on a typo in `--repo` is worse
than one that stops. Secondary domains need no init: their first file
is their first commit.

### Ordering

Order is a **rank key** per file, in the LexoRank style: a string
compared bytewise, with room to insert between any two neighbours by
appending. Moving one card rewrites one file; nothing is renumbered;
there is no shared index file to fight over. When two neighbours have no
room left (the key would exceed a length cap), the mover rebalances the
run between the nearest neighbours that have room — a rare, bounded
rewrite in the same commit, **confined to the mover's domain**: a run
never reaches into a repository the mover cannot write. When the only
roomy neighbours sit in another domain (a merged roster), the cap is
soft — the key grows past it rather than any file being written across
the boundary. The key logic is pure and lives in `pkg/board`
(`rank.go`).

The same key orders teams, projects, epics, processes, tasks and
deadlines. Because it is a plain string, roster fragments from different
domains merge into one order.

## Commits

**One action, one commit per touched domain.** An HTTP request or MCP
call that changes N cards — Carry Over over twelve cards — produces one
commit touching N files. That is what makes the history readable and a
mistake revertible with one command. Actions are *not* bundled per
author or per time window: that would glue unrelated edits together and
make revert hit things it should not.

When an action touches **two domains** — Carry Over moves the team's
sprint pointer (team file, team domain) and carries cards that may live
in a project domain; `RenameEpic`, `DeleteTeam` and the
process sweep can do the same — it produces one commit **per domain**,
all carrying the same `Aeman-Action-Id`. Two repositories cannot share
a commit; the id is what makes them one action for the log, for revert
("revert everything with this id") and for the reader. Atomicity is per
domain: each domain's push succeeds or is retried on its own, and until
both have landed the cache is authoritative for this replica's readers
and both commits are on disk. `/api/healthz` shows the oldest unpushed
commit's age so a stuck half is visible, not silent.

The exception to "one request, one commit" is the **coalesced field
write**. Dragging a progress slider fires many writes for one intent;
the write queue already collapses them (DeltaFIFO). Those become **one
commit carrying the final value**, cut when the coalescing window closes
(500 ms after the last write to that key). Only progress is coalescable;
a `PATCH` that changes anything else is an action and commits at once.
The coalescing key gains the **actor**: two people dragging the same
slider today silently overwrite each other; here they produce two
commits, both attributed.

Order is preserved per card: **an action commit on a card flushes that
card's pending coalesced writes first**, so slider→100 followed by
send-to-review (which clamps to 90) commits progress 100, then the
review with its clamp — never the review first and a stale 100 on top.
This is the FIFO the queue keeps today, carried over.

Message format:

```
carry over 12 cards to 2026-08-28

Aeman-Action: carry-over
Aeman-Action-Id: 01JB4KA0M2P4R6T8V0X2Z4B6D8
Aeman-Actor: kvaps
Aeman-Cards: 01JB4K2E7QZMX3R8V0N5T9WYC1 01JB4K3M8XTR…
Aeman-Change: 01JB4K2E7QZMX3R8V0N5T9WYC1 review-sent - timur
```

The first line is for humans. The trailers are the machine-readable
part: `Aeman-Action` is the action name (today's event kinds — `create`,
`progress`, `stage`, `carry-over`, … — plus the actions that never had an
event), `Aeman-Action-Id` ties the commits of one action together,
`Aeman-Actor` the login, `Aeman-Cards` the affected ids.
`Aeman-Change: <card> <kind> <from> <to>` carries a change whose payload
is **not a field diff** — `review-sent`/`review-passed` name the
reviewer, `reviewer-removed`, `subtask` names the child's title,
`reopened` — one line per such change; an empty side is written as `-`
so the four tokens always split. A card's activity log is the
list of commits whose `Aeman-Cards` names it or whose diff touches its
file; each log line's *from → to* comes from an `Aeman-Change` trailer
when there is one for that card and kind, else from the diff of the
card's front-matter — which is why the file format keeps one field per
line.

**A day is read by committer time.** The board of a past day is the tree of the newest commit whose COMMITTER time is at or before that day's last moment (`LoadAsOf`, G60), walked along first parents. So every commit carries the moment it was written: a replayed one (a rejected push re-applied on the remote's tip) keeps its author and takes a fresh committer time, exactly as git's own rebase does. Kept, the old stamp would sit a 23:58 commit on top of the next morning's and a day's record would silently read another day's tree.

**Author is the user, committer is the server.** Author name is the
login; author email defaults to `<login>@users.noreply.github.com` when
the primary remote is on GitHub (so the forge UI shows the right face)
and to `<login>@aeman` otherwise; both are configurable. Committer is
the server's identity from configuration. The commit date is the action
time. Writers that are nobody in particular — the process sweep, the
asynchronous title resolution (`resolveTitleAsync`), the migration's
import commit, schema migration — are authored `aeman`; reviewers of
the history should expect exactly those.

The push credential is the **server's**, not the user's: several
replicas each pushing many users' commits cannot use per-user tokens.
Authorization is ours to check at the API boundary (below).

## Storage engine

### go-git, filesystem storer, no worktree

The production image is `distroless/static`: no git binary, so the
client is **go-git** (v5.19). Objects are written and read through the
plumbing API — blob, trees along the path, commit — never through a
worktree: a checkout is a second copy of the data with nothing to do.

The clone lives in a directory under `--data`, next to the session file;
**each replica has its own `--data`** — a clone is a working copy with
local commits, never shared between processes. **On a persistent
volume**, which is what the shipped compose file mounts: a container restart *and* a node reboot keep unpushed commits.
A tmpfs volume is acceptable and is the same code — the trade is
stated, not hidden: it survives a container restart but not a reboot,
and the loose objects (below) live in RAM. Either way the clone is
inspectable with `git log`, the property that made the "cache lost"
hunts survivable. The storer is a one-line swap if that ever changes.

Six things the research found in go-git, each closed in code over its
public API — none is optional:

1. `Repository.Log` ignores the shallow list and errors at the boundary.
   Our walker visits the boundary commit and does not cross it.
2. `Fetch` applies the server's `shallow` lines but not its `unshallow`
   lines. The deepen path applies both.
3. `FetchOptions` has no time-based depth. Deepening drives one
   upload-pack session by hand with `packp.DepthSince`: advertise →
   request `{Wants, Haves, Shallows, Depth: since}` → sideband demux →
   `packfile.UpdateObjectStorage` → `SetShallow(old + shallows −
   unshallows)`. ~80 lines, exact to the day, one round-trip.
4. A rejected push from a shallow clone surfaces as `object not found`,
   not as a non-fast-forward error. The retry loop never classifies by
   error type (below).
5. go-git never packs. A maintenance tick runs `RepackObjects` +
   `Prune` (in-process, no binary) when loose objects exceed a
   threshold; one day of a busy board packs in well under a second.
6. SSH host keys are not pinned to the stored algorithm. The default
   transport is **HTTPS with a token** — faster on every operation (a
   no-op fetch is 0.2 s vs 1.4 s), no host keys, and the same shape on
   every forge. SSH remains possible with an explicit `known_hosts`.

### Cache and write queue

The optimistic cache **stays**. The order of a write is:

1. mutate the in-memory board (as today) and answer the client — the
   same milliseconds as now;
2. enqueue the write; a progress write coalesces with the pending
   progress write of the same `(card, actor)` — today every field
   setter coalesces, here only the one continuous gesture does; every
   other request is an action and commits at once;
3. **commit locally** when the entry's window closes (immediately for
   actions — after flushing the touched cards' coalesced entries — and
   500 ms after the last write for a coalesced one) — ~1.5 ms, disk, no
   network;
4. **push in the background**: the push worker sends every unpushed
   commit of a domain in one push, with backoff and jitter on rejection.

Durability changes in one place: from step 3 on, the change is on disk,
and a crash or OOM-kill between "the user saw OK" and "the store has it"
no longer loses it — today it does, because the queue is in memory. The
existing graceful-shutdown drain (`waitDrained`) is kept and becomes
cheaper: one push per domain instead of N mutations.

What goes away because there is no longer a gap between "what we wrote"
and "what the store returns": the replay of pending ops onto a reloaded
board (`apply`), the 90-second `recentCards`/`recentGone` guards,
provisional `local-…` ids and their aliasing (`resolvingBackend`), the
stale-while-revalidate tiers, the startup warmer (a cold clone is 2.6 s;
the warm roster is not needed). The watch hub, presence, the diff
broadcast and the coalescing queue stay.

### Remote changes, and the sweep

Another replica — or a person with a text editor — may push. The
server fetches on a timer (`--sync-interval`, default 15 s) and on a
`POST /api/hooks/sync` that any forge webhook can hit (no payload is
read; the call means "fetch now"). A new tip is applied to the cache by
diffing trees old→new and reloading only the touched files; the
resulting changes go out over the watch stream like any other. The
everyday fetch is a plain fetch (no depth), which brings new commits
down to what we have without adding shallow boundaries.

The **process sweep** — `SpawnDue`, which files the iterations each
week is owed — runs after every fetch tick, on every replica, as the
server identity. Today it rides the warmer with a session's token
(`docs/design/processes.md`); the warmer goes, and the sweep needs a
home that does not depend on anyone being logged in. Replicas will
sweep the same due task in the same minute; the iteration's id is
therefore **deterministic** — a ULID whose time is the due week's
Monday and whose random bits are a hash of `(task id, week)` — so two
replicas write the **same path**. The contents differ in `created` and
`rank`; a create whose path already exists on the new tip is a no-op on
re-apply (a create is idempotent by path), so the loser emits no second
commit and one iteration exists. `spawnIfDue`'s idempotency check then
holds across replicas, not just within one.

### Push, rejection, retry

Push rejected → fetch → if nothing new arrived, the push really failed
(log, keep the commits, retry later) → otherwise **re-apply the local
commits on the new tip**: the branch is reset to the tip and every
commit the remote has not seen is replayed, oldest first, as a
field-level change — for each file the commit touched, the fields it
changed (front-matter keys, the body, notes by id) are set on the file
as it now is on the tip; a create whose path already exists is a no-op,
an edit of a card the tip deleted is dropped. That is **field-level**:
two writers on different fields of one card both land, as they do today
with per-field GraphQL mutations; two writers on the same field resolve
last write wins, also as today. The replayed commit keeps its message,
trailers, author and date. The commits are found on the branch, not in
memory: what a run left unpushed when it stopped is replayed by the next
run, and a move — a create in one repository, a delete in another — is
replayed per repository. (The first design replayed the queue's mutation
closures instead; that loses commits across a restart and cannot replay
one repository of a move without the other, so the commit replay took
its place.) Then push again.

Rank keys keep concurrent reorders from colliding on a shared file;
concurrent edits to *one* card resolve last-write-wins, as they do today.
Backoff with jitter prevents the starvation the research produced in a
tight loop (one writer losing 40 races in a row).

A push that keeps failing is **visible**: `/api/healthz` carries the age
of the oldest unpushed commit per domain, the same number is a metric,
and the UI shows a banner once it passes `--unpushed-warn` (default
5 min) — a revoked or expired `AEMAN_GIT_TOKEN` must not be discovered
a week later. Rotating the credential is an environment change and a
restart; the drain pushes what it can with the old token first.

## History

### Horizon

The clone starts at **depth 1** (the board's current state) and deepens
in the background to a **time horizon** (`--history`, default `2w`,
i.e. roughly four sprints). One knob serves both consumers of history:

- **A card's log** — the commits within the horizon that touch it,
  including across an `Aeman-Moved-From`. The API says where the horizon
  is (`truncatedBefore` on the log) so the UI can show "older history
  not loaded" instead of nothing.
- **State on a day** — the tree as of the last commit before that day,
  for the planned day-state replay.

Nothing an *action* depends on is read from history: `Reopen` reads
`doneFrom` from the file. History is for people, and it may be cut.

Deeper on demand: a log or replay request for a date past the horizon
deepens to that date first (bounded by `--history-max`, default `1y`),
so a user who digs gets the history and everyone else does not pay for
it. The horizon is also the **memory bound** per board: the research
measured +32 MB heap for 11 600 commits of history; the defaults are
chosen to keep a board in tens of MB.

### Maintenance

Loose objects accumulate at ~8 KB per commit. A daily tick repacks and
prunes in-process and deletes torn-move ghosts whose destination has
landed. There is no gc of history: a board's history is the product.

## Multiple repositories

```
aeman serve \
  --repo shared=https://github.com/aenix-org/aeman-db.git \
  --repo closed=https://github.com/aenix-org/aeman-secret.git
```

`--repo name=url` is repeatable; order matters; the first is the primary
domain. Env: `AEMAN_REPOS="shared=…,closed=…"`. The push credential is
`AEMAN_GIT_TOKEN` (HTTPS basic auth, user `x-access-token` or as the
forge wants), never a flag.

### Who sees, who writes

| mode | identity | visible domains | write check | push credential |
| --- | --- | --- | --- | --- |
| local (`gh` token) | `gh api user` login, as today | all configured | none beyond the forge's own on push | `AEMAN_GIT_TOKEN` if set, else `gh auth token` |
| self-hosted (OAuth) | the session's login, as today | those the visitor `CanRead` | the target domain must be `CanWrite` for the visitor | `AEMAN_GIT_TOKEN` |

The forge adapter provides **two** probes, `CanRead(ctx, user, repo)`
and `CanWrite(ctx, user, repo)`, answered from the forge's permission
API with the visitor's token (GitHub: the repository's `permissions`
block — one request per domain, cached **60 s per visitor**, the same
freshness `authFreshFor` gives access today), not from a git
advertisement — which would need the broad `repo` scope for what is a
permission question. Every mutation checks `CanWrite` on the domain it
targets (both domains of a move); a read-only collaborator sees the
board and cannot change it, whatever the server credential can do.

The watch stream is filtered the same way: a subscription carries the
visitor's readable-domain set, every frame carries the card's domain,
and a closed-domain change never reaches a socket that cannot read it.
The snapshot rule (an unreadable domain is absent, not empty) and the
stream rule are one rule.

### Board identity

A board is named by its primary repository; the `owner`/`board` pair of
the API becomes one `board` string (default: the only configured board;
`--lock-board` behaves as before). This is a breaking change to the REST
and MCP surfaces and is called out in `docs/api.md`.

### The Board resource, the SPA and domains

`GET /board` gains `domains`: the visitor's readable domains, with a
`writable` flag and the access label used in the creation dialogs.
`metadata.members` gains `avatarUrl`, resolved by the forge adapter —
the SPA's own avatar lookup (`web/src/users.ts`) goes through the GitHub
GraphQL proxy today and assembles `github.com` URLs itself; both go, and
the `/api/github/` proxy is deleted only after that.

## Forge-agnostic by construction

Nothing forge-specific enters the repository: ids are ours, avatars are
resolved at read time, `assignees` are logins scoped to the board. The
remote is a URL and a credential; the code never assembles
`github.com`. The forge sits behind three interfaces — "who is this
visitor" (OAuth provider), "may they read / write this repository", and
"what is their avatar" — and GitHub is the first implementation.
GitLab, Gitea and a bare repository on a server (with static users) are
additional adapters, not redesigns. GitLab is now the second adapter,
beside GitHub in the package `internal/forge`; Gitea and the bare
repository are not built, the design just does not prevent them.

## Configuration

| flag | env | default | meaning |
| --- | --- | --- | --- |
| `--repo name=url` (repeatable) | `AEMAN_REPOS` | — | the board's domains, primary first |
| — | `AEMAN_GIT_TOKEN` | — | push/fetch credential (HTTPS) |
| `--data` | `AEMAN_DATA` | `/data` | clones and session file |
| `--history` | `AEMAN_HISTORY` | `2w` | background deepening horizon |
| `--history-max` | `AEMAN_HISTORY_MAX` | `1y` | cap for on-demand deepening |
| `--sync-interval` | `AEMAN_SYNC_INTERVAL` | `15s` | fetch cadence for remote changes |
| `--unpushed-warn` | `AEMAN_UNPUSHED_WARN` | `5m` | age of the oldest unpushed commit that turns healthz red |
| `--committer` | `AEMAN_COMMITTER` | `aeman <aeman@localhost>` | committer identity |
| `--author-email` | `AEMAN_AUTHOR_EMAIL` | forge-dependent template | author email template, `{login}` substituted |

`--owner` and `--board <number>` disappear. Everything above is
documented in `docs/api.md` under configuration.

### `aeman mcp`

The stdio MCP server takes the same `--repo`/`--data`/`--history`
flags and **owns its own store**: its own clone under `--data`, cache,
commit and push workers. It pushes with `AEMAN_GIT_TOKEN`, else with
`gh auth token` (GitHub only, the local mode of today). On exit it
drains like the server does (`waitDrained`), so a client that closes the
pipe right after a mutation does not lose it; a kill leaves the commits
on disk for the next start to push. The `/mcp` endpoint inside `aeman
serve` shares the server's store, as it does now.

## Migration

A one-way, idempotent command:

```
aeman migrate --owner aenix-org --board 37 \
  --repo https://github.com/aenix-org/aeman-db.git [--dry-run] [--report out.md]
```

1. **Export** the Projects v2 board in full (items, fields, bodies).
2. **Ids**: every item gets a ULID derived deterministically from its
   GitHub item id (hash → the ULID's random bits; time = the item's
   creation), so a re-run produces identical files. The item id is kept
   as `github:` in the front-matter. **Every id-valued field is remapped
   through the same table** — `parent`, `reviewOf`, `task`, the
   sprint-state/epic-state/project-state/process-state links — and a
   reference to an item that is not on the board is cleared and
   reported.
3. **Snapshot**: build every file of the layout from the current state.
   Note ids are ULIDs derived the same way from `(item id, line index)`,
   so a re-run is byte-identical. Every card that is done gets its
   `doneFrom` seeded by the walk `Reopen` does today (the last progress
   event to ≥100 → its from-side); the report counts how many were
   seeded and how many done cards had no such event.
4. **History**: the event lines in every draft body become **synthetic
   commits**, ordered by time, dated and authored from the event, with
   the same trailers a live action would carry (`Aeman-Change` for the
   non-diff kinds). Their file changes are best-effort (`progress`,
   `stage`, `zone`, `sprint`, … applied where the event names a field) —
   the research showed the log is an annotation, not a journal: 1282 of
   2488 cards are *not* reconstructible from their events (derived states
   logged as if stored, parents by title, moves without events).
5. **Truth**: the final commit writes the exact snapshot and the
   migration **verifies** that the tree equals the snapshot byte for
   byte before pushing. History is nice to have; the current state is
   not negotiable.
6. **Report**: what was dropped or approximated — the vestigial `Status`
   field (`Todo` on every card), the 7 issue cards reduced to a `link`,
   unattributed notes, events whose field could not be applied, dangling
   references — and the full old-id → new-id table.

Idempotence: a repository that already carries the migration's final
commit for this board (marked in its message) is left alone unless
`--force`, which writes the new history over the remote's (a forced
push — the earlier import and anything written since are replaced by the
verified snapshot). The Projects v2 board is never written. The migration is the
last code that reads Projects v2; it stays in the tree as long as it is
useful and is deleted after.

**Legacy ids on the API.** Every card's `uid` changes from `PVTI_…` to
a ULID. For one major version the server accepts a legacy id wherever a
`uid` is taken and resolves it through the `github:` keys (an index
built at load); responses always carry the new id. Clients and
pipelines that hold ids get the table from the migration report and a
grace period, not a cliff.

## What changes in the code

- **New** `pkg/gitstore`: the git backend — layout read/write, commit
  building, push/fetch/deepen, repack, the history walker, domain
  composition. Satisfies `boardservice.Backend`.
- **New** `cmd/aeman migrate` and `cmd/aeman init`.
- **`pkg/board`**: the GitHub-only fields listed above and `Card.Events`
  deleted; `Card.DoneFrom` and `Card.Domain` added (also `Domain` on
  `EpicCol`, `Process`, `Deadline` and the team roster);
  `SprintStates`/`TeamOrder`/`ProjectStates` replaced by a typed roster;
  `rank.go` added.
- **`pkg/boardservice`**: `Backend.LoadBoard(ctx, board string)`;
  `LoadCards` stays (a partial read from the tree); `AppendEvent` is
  removed — the action name, id and `Aeman-Change` payloads travel on
  the context (`WithAction`, next to today's `WithActor`) and become the
  commit; the state-card setters (`SetSprintState`, and `SetProcess`,
  `SetProject`, `SetPaused`, `SetAccumulate` on state cards) become
  roster operations. **Every `Service` method** changes from
  `(owner string, project int)` to `(board string)` — roughly sixty
  signatures; `docs/embedding.md` calls the service "the contract", so
  this is a major version of the public packages, not a footnote.
  `Reopen` reads `DoneFrom`; `logEvent` goes; `Mirror` (on its branch)
  gains the domain guard.
- **`internal/server`**: `boardstore.go` shrinks to the cache, the
  coalescing queue (actor in the key, flush-before-action), the watch
  hub (domain-filtered) and the diff broadcast; the commit, push, fetch,
  deepen, sweep and maintenance workers are new; the OAuth flow is
  unchanged; the forge adapter (`CanRead`/`CanWrite`/`AvatarURL`) is
  new; `/api/github/` proxy removed after `users.ts` moves; healthz
  gains unpushed age and duplicate names.
- **`pkg/apiserver`, `docs/api.md`**: `board` becomes a string; the log
  gains `truncatedBefore`; the Board resource gains `domains` and
  members gain `avatarUrl`; `metadata.contentId/isDraft/url/number/
  repository` disappear; note ids change format; legacy `PVTI_` uids
  accepted for one major version.
- **Deleted**: `pkg/ghprojects`, `internal/server/startupwarm.go`,
  `resolving.go`, the provisional-id machinery.
- **`web/`**: `owner`/`board` handling → `board`; `users.ts` avatar
  lookup → `metadata.members[].avatarUrl`; the log view shows the
  horizon; team/project creation asks for a domain when there is more
  than one; the reviewer picker filters by domain; the unpushed banner.
  The mirrored domain rules (`date.ts`, `sprint.ts`) are untouched —
  dates and sprints do not change.
- **`docs/design/behavior-matrix.md`** gets a section for the storage
  rules (below); **`docs/dates.md`** is unchanged in substance.
- **Any tool driving boards through `gh` against the Projects schema**
  loses its access path entirely, not just its docs: it has to move to the
  REST/MCP surface or to reading the repository. `plugin-impact.md` is
  what such a writer needs.

## Testing strategy

Unit and behaviour tests for the layout, commits, rank keys, domain
composition, coalescing and the retry loop run against an **in-process
go-git server transport** (`plumbing/transport/server`) — no git binary,
fast, hermetic. That server **does not implement shallow**
(`server.go`: "shallow not supported"), so the shallow paths —
clone at depth 1, deepen-since, unshallow, push from a shallow clone,
push from a shallow clone (G9–G11) — run against the real
`git-upload-pack`/`git-receive-pack` binaries over the file transport.
Locally they `t.Skip` when the binaries are absent; in CI
`AEMAN_TEST_REQUIRE_GIT=1` turns that skip into a failure, so a
misconfigured runner cannot silently drop them. The walker at the
boundary (G8) needs no server at all: `SetShallow` on a local repository
pins it hermetically. One integration test, behind an environment flag,
runs the same paths against a real remote.

## Rules, and the tests that pin them

Every rule below lands as a failing test first, in `pkg/gitstore`,
`pkg/board` or `pkg/boardservice`, before the code that makes it pass.
The tests are the second documentation: each names its edges.

| # | Rule | Edges the test must spell out |
| --- | --- | --- |
| G1 | A card's path is `cards/<a>/<b>/<id>.md` with `a`,`b` = the id's last two chars; the path never changes while the card exists in a domain | rename, move, re-zone, re-team: same path; two ids differing only in the tail land in different leaves |
| G2 | Empty fields are omitted; unknown keys survive a rewrite | a file with `foo: bar` rewritten by a setter still has `foo: bar`; a cleared field disappears from the file |
| G3 | Derived states are never written | 100% progress writes `progress: 100` and `doneFrom`, never `stage: done`; a review passing (90 → 100) writes `doneFrom: 90`; In Progress is absent from every file |
| G4 | One action = one commit per touched domain, sharing `Aeman-Action-Id`; `Aeman-Cards` lists every id; zero changes → no commit | Carry Over of N cards in one domain → one commit; the same with the team file in another domain → two commits, one id; zero cards but the pointer advances → one commit touching the team file only; already on today's sprint → no commit |
| G5 | Coalesced writes commit once with the final value, keyed by actor; an action on a card flushes its pending coalesced writes first | 5 slider writes by A → 1 commit `progress: 80`; A and B interleaved → 2 commits; slider→100 then send-to-review → progress commit precedes the review commit, final progress 90 |
| G6 | Author = actor, committer = server, date = action time; the sweep, title resolution, import and schema migration are authored `aeman` | trailers present; email template applied; a sweep commit has author `aeman` and no `Aeman-Actor` |
| G7 | The activity log of a card = commits touching it within the horizon, across `Aeman-Moved-From`; from/to from `Aeman-Change` first, front-matter diff second | a commit touching two cards appears in both logs; `review-sent` shows the reviewer; a commit past the horizon is absent and `truncatedBefore` is set; a moved card's log spans both domains |
| G8 | The history walker stops AT the shallow boundary | depth-1 clone: log has exactly 1 entry, no error; after deepening: the boundary commit is included, its parent is not walked |
| G9 | Deepening applies `unshallow` and lands exactly at the horizon | deepen to date T: oldest reachable commit ≤ T, all commits > T present; a second deepen further back; deepening past the root leaves no shallow entry |
| G10 | Push rejection is detected by fetching, not by error type | rejected push whose fetch brings nothing → reported as failed, commits kept, healthz age grows; rejected push whose fetch brings new commits → re-applied and pushed |
| G11 | Re-apply on a new tip replays the unpushed commits field by field onto the files as they now are: last write wins per field; a create is idempotent by path; the commits are found on the branch, so a restart between commit and push loses nothing | two writers, disjoint cards: both changes present; same card, different fields: both land; same field: the later re-application wins, history has both commits; two replicas spawning the same iteration → one file, the loser's create is a no-op with no second commit; a commit made before a restart is pushed by the next run |
| G12 | Rank insertion touches one file; rebalancing is bounded to the run and to the mover's domain | insert between neighbours: one file changes; exhausted key space: only the run between the nearest roomy neighbours is rewritten, in the same commit; a run that would cross into another domain stops at the boundary |
| G13 | Roster fragments merge into one order across domains; duplicate names resolve to the oldest, the rest are aliases | interleaved ranks come out interleaved; identical ranks tie-break by id; two fragments declaring `portal` → one team, the older file's rank and sprint, both fragments' cards, healthz names the duplicate |
| G14 | Domain follows the inheritance rule, linked cards first, and is never chosen per card | a card under a closed project → closed repository; a team card without a project → the team's domain; a review card of a **closed-project** original whose `team` lives in shared → closed, not shared (the review card carries the original's team and no project, so the team rule would leak it); a subtask carrying its own column → its parent's domain regardless; an iteration → its task's; moving a card moves its review card and subtasks in the same action |
| — | The `reviewOf` link on a MIRRORED card cannot change the card's repository | the link is a re-file in disguise (linked cards first); it is refused while mirrors stand and the card would move (`ErrCrossDomain`), and a card whose link already holds its file elsewhere cannot be mirrored |
| — | A card's `process:` tie (SetCardProcess) stays in the card's own repository | the tie is refused across domains (`ErrCrossDomain`) — a closed card naming a shared process would hand its existence to readers who may not have it. The tie is a reference by name and lives like one: a renamed process rewrites its ties (logged per card), a process with standing ties will not delete (`ErrProcessInUse`), and a process cannot move to a project of another repository while ties stand (`ErrCrossDomain`) — the move would strand every tie at once. The card side is closed the same way: every re-file that would carry a tied card into another repository (a new team, project, column or review link) is refused, and grouping under a parent clears the tie the way it clears mirrors |
| G57 | A card's COLUMN names the repository that holds its file | when a link outranks the project (a subtask's parent, a review card's original, G14) the file stays with the link while the column could be dragged anywhere: attaching, re-filing, grouping, ungrouping, re-teaming or review-linking the card — and moving the card whose file it follows (a parent, a reviewed original) — is refused (`ErrCrossDomain`) rather than left naming a repository that does not hold the card. A column never changes repository — a stub is written back to the backend that holds it — so moving one to a project of another repository is refused rather than performed. A column's repository is the one its own epic stub was declared in — the no-project bucket has no project to ask |
| G15 | Mirror refuses a target column in another domain | `ErrCrossDomain`; a same-domain mirror works; the guard order — home column required, the home itself refused, the target must exist, then the same-repository check; already mirrored is a no-op — is pinned in behavior-matrix G15. The invariant holds through re-files too: the home moving onto a mirror drops the duplicate, a cleared column clears the mirrors, and neither a mirrored card nor a mirrored column may cross repositories |
| G16 | The reviewer picker offers only readers of the card's domain | a login without access to the closed domain is absent; with access present |
| G17 | An unreadable domain is absent from the snapshot AND the watch stream; the primary is required | a visitor who can read one of two domains gets exactly that domain's teams, projects and cards, no placeholders; a closed-domain commit applied from remote is not delivered to that visitor's socket; a visitor who cannot read the primary gets 403 and no board; a card whose team file is unreadable is still served, under its team name, with the team's sprint controls unavailable |
| G18 | A newer `schema` is refused; an older one is migrated in a commit | `schema: 99` → clear error at startup; `schema: 0` → one commit, `schema: 1`, files rewritten |
| G19 | Remote changes reach the cache by diff | a commit pushed from elsewhere touching one card updates that card only and is broadcast once, to readers of its domain only |
| G20 | Restart keeps unpushed commits | commit, no push, reopen the store → the commit is in the queue and pushed; the same for `aeman mcp` exiting right after a mutation |
| G21 | Repack keeps every object reachable | after RepackObjects+Prune every commit, tree and blob of the history still reads |
| G22 | A cross-domain move is create-then-delete with the same id and one action id; a torn move shows the card once, from the tree alone | filing a team card under a closed project → two commits, same ULID, the create carries `Aeman-Moved-From` and `movedFrom:` in the file, the delete `Aeman-Moved-To`; the create is committed to disk before the delete; only the create pushed → a fresh **depth-1** clone of both domains shows the card once (the source file is the ghost); only the delete pushed → the card is still served from this replica's cache; maintenance removes the ghost after the destination landed |
| G23 | `Reopen` restores `doneFrom` regardless of history depth | a card done past the horizon reopens to its pre-done progress; a card with no `doneFrom` falls back to the in-progress nudge |
| G24 | An unborn remote is refused with the init hint; `aeman init` bootstraps in one commit | `serve` against an empty repository exits naming `aeman init`; `init` writes `board.yaml` + `teams/_.yaml`, pushes, and a second `init` is a no-op |
| G25 | Mutations require `CanWrite` on the target domain(s) | a read-only collaborator gets 403 on every mutation and 200 on every read; a move checks both domains |
| G26 | Healthz reports the oldest unpushed age per domain and turns red past `--unpushed-warn` | after a failing push the age grows; after a successful push it is zero |
| M1 | Migration: the final tree equals the snapshot byte for byte | a card whose events contradict its state ends up as the snapshot says |
| M2 | Migration is idempotent | second run on the migrated repository is a no-op without `--force` |
| M3 | Migration ids are deterministic and every id-valued field is remapped | the same GitHub item id yields the same ULID on every run; `parent`/`reviewOf`/`task` point at migrated cards; a reference to an absent item is cleared and reported |
| M4 | Migration reports what it dropped | the 7 issue cards, the `Status` field, unattributed notes, unapplied events, dangling references and the id table are all in the report |
| M5 | Legacy ids resolve for one major version | `GET /cards/PVTI_…` returns the migrated card with its new `uid`; an unknown legacy id is 404 |

Behaviour that does *not* change — dates, sprints, visibility, the
review cycle, carry-over, processes — keeps its existing rows and tests
in `behavior-matrix.md`; those tests run unchanged against the new
backend through the `boardservicetest` fake, which is why the backend
interface change is kept as small as it is.

## Rollout

1. Land `pkg/gitstore` behind the `Backend` interface with its tests.
2. Land the migration; run it against board 37 into
   `aenix-org/aeman-db`; verify; **the production board keeps running
   on Projects v2** throughout.
3. Run a second aeman against `aeman-db` for the team to try — same
   binary, `--repo` flag.
4. Switch production: point the deployment at `aeman-db`; the Projects
   v2 board becomes read-only history. Rollback is the previous image
   and the previous `.env`; nothing in Projects v2 was written.
5. Remove `pkg/ghprojects` and the old wiring; keep `aeman migrate`
   until the last board has moved, then delete it.

## Backward incompatibility

- REST and MCP: `owner`+`board <number>` → `board <string>`; every card
  `uid` changes from `PVTI_…` to a ULID at migration (legacy ids
  accepted for one major version); `metadata.contentId`, `isDraft`,
  `url`, `number`, `repository` disappear; note ids change format; the
  Board resource gains `domains`; the `/api/github/` proxy is gone.
- `pkg/board`, `pkg/boardservice`, `pkg/apiserver`: the fields and
  signatures listed above — a major version for the public packages
  (`docs/embedding.md`).
- Any `gh`-based access path against the Projects schema stops working.
- A board can no longer be viewed in the GitHub Projects UI. This is
  accepted: nobody is using it.

## Decisions taken here, for the record

- Project before team as the domain axis; review cards, subtasks and
  iterations follow what they belong to.
- Cross-domain re-filing is a move (delete + create, same id, one action
  id), not a refusal — "remove from project" is an everyday action and
  it changes the domain.
- Names, not ids, for `team`/`project`/`epic` references in cards (API
  compatibility; one commit per rename anyway). Duplicates resolve to the
  oldest; the server never renames on its own.
- Ids for `parent`/`reviewOf`/`task` (they are ids today), remapped by
  the migration.
- `doneFrom` stored rather than a history read: actions never depend on
  a log that may be cut.
- One file per team/project/epic/deadline/process, not one roster file
  (concurrent sprint-pointer updates per team are the common write).
- Notes in the body with an explicit ULID per note (stable ids across
  edits; readable in a diff).
- Two-level sharding, measured, for scale; depth is part of the schema.
- No archive directory for closed sprints: it would be a mass rename,
  and the horizon already bounds what the server holds.
- Filesystem storer on a persistent volume (restart and reboot
  survival, inspectability; tmpfs allowed with its loss window named).
- HTTPS + token over SSH (speed, no host keys, forge-agnostic).
- Permission probes via the forge API, not git advertisement (narrower
  scope, answers "write" as well as "read").
