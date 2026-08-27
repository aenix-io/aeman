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
costs is `pkg/ghprojects` (4349 lines mapping our domain onto someone
else's field model) and `internal/server/boardstore.go` (2210 lines
hiding a backend that takes tens of seconds to answer). The pure domain,
`pkg/board`, is 2828 lines. We spend more code working around the store
than we spend on the product.

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

Everything below a domain **inherits** it: a project's epics, deadlines
and cards live where the project lives; a team's cards live where the
team lives. Nothing is chosen per card. The user is asked once, when
creating a team or a project, and only when more than one writable
domain exists; the choice is labelled by access ("shared — everyone",
"closed — 4 people"), not by repository name.

### References never cross a domain boundary

The domain is full of links — `parent`, `reviewOf`, `task`, `mirrors`,
the per-team sprint pointer. A link that crosses a visibility boundary
would show one side an orphan, exactly the "card vanished" class of bug.
Each link is closed by its own mechanism, so an orphan cannot be created:

| link | how it stays inside |
| --- | --- |
| `parent`, `task` | inherited domain — a subtask is filed where its parent is, an iteration where its task is |
| `mirrors` | a guard in `boardservice.Mirror`: the target column must be in the same domain (next to the three guards it already has) |
| `reviewOf` | not forbidden — cross-team review is routine — but the reviewer picker offers only people who can read the card's domain. You cannot review what you cannot see; the UI says so instead of failing later |

Splitting goes by **sensitivity**, never by team or by project:
`sendToReview` gives the review card the *reviewer* as assignee and the
*original's* team, so team-split boards would orphan every cross-team
review; mirrors link columns of different projects by definition.

### Rosters are fragments, merged on read

Teams and projects are declared by files in their own domain — a
centralized roster would leak the name of a team created in a closed
domain to everyone. Order is a **rank key** on each file (see
"Ordering"); rank keys are comparable across domains, so the merged
roster has one consistent order without any central file.

## Layout

The one principle: **a path never encodes mutable state.** A state
change that moved a file would be a rename — more conflicts, a broken
`git log --follow`, and a commit that says nothing. Paths carry identity;
files carry state.

```
board.yaml                         primary domain only: schema, title
teams/<id>.yaml                    one team: name, rank, sprint pointers
projects/<id>/project.yaml         one project: name, rank
projects/<id>/epics/<id>.yaml      one column: name, rank
projects/<id>/deadlines/<id>.yaml  one deadline line: week
processes/<id>/process.yaml        one process: name, project, paused, rank
processes/<id>/tasks/<id>.md       one task: front-matter + body
cards/<a>/<b>/<id>.md              one card: front-matter + body
```

Every `<id>` is a **ULID** minted by the server (26 chars, Crockford
base32, time-ordered). Cards are sharded under `cards/<a>/<b>/` where
`a` and `b` are the **last two characters** of the id, one per level.
Two facts from the research fix this shape:

- Every commit rewrites the tree object of each directory on the changed
  file's path. A flat `cards/` with 2388 entries is a 140 KB tree
  rewritten 20 000 times: 4.8 ms per commit, 189 MiB of history. 16
  shards: 1.46 ms and 34 MiB. The rewritten intermediate tree *is* the
  cost, so every directory that changes per commit stays at a few
  hundred entries. Two levels of 32 give 1024 leaves with a handful of
  files each and a 1 KB intermediate tree, and stay flat up to tens of
  thousands of cards.
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
start: 2026-08-26
day: 2026-08-28
sprint: 2026-08-24
plan: fri
week: 2026-08-24
project: portal
epic: Bugs
mirrors:
  - [infra, Reliability]
parent: 01JB4K2E7QZMX3R8V0N5T9WYC1
reviewOf: ""
reviewRound: 0
recurrence: ""
process: ""
task: ""
accumulate: false
link: https://github.com/aenix-io/cozystack/issues/1234
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
- `team`, `project`, `epic`, `process` are **names**, as in the domain
  and the API (`ColRef` is a name pair; a column is `(project, epic)`).
  Renaming an epic rewrites every card that names it plus the epic file
  — in **one commit**, which is what today's `RenameEpic` does across N
  GraphQL calls.
- `parent`, `reviewOf`, `task` are **ids**.
- `link` is the only trace of the 7 issue-backed cards: a URL in the
  card, nothing else. There is no issue/PR integration.
- `rank` is the ordering key (below). `created` is the creation time.
- Notes live in the body under `## Notes`, one list item each, first
  token the note's ULID, then the timestamp, the author, an em dash, the
  text; continuation lines are indented two spaces. The id is what the
  notes API addresses; it never changes on edit.
- **Events are not in the file.** A commit is the event.

Card fields that existed only to round-trip GitHub are deleted from
`board.Card`: `ContentID`, `IsDraft`, `ZoneOptionID`, `EventLogID`,
`URL`, `Number`, `Repository`, `State`. `Card.Events` goes too — the log
is read from history (below). `Card.ItemID` stays as the field name for
the ULID so the API's `uid` does not change meaning.

A team:

```yaml
name: portal
rank: a0
sprint:
  current: 2026-08-24
  previous: 2026-08-21
```

A project, an epic, a deadline, a process, a task follow the same
pattern: a name where it has one, a `rank`, the fields the domain
already gives them (`Deadline.Week`, `Process.Paused`, task front-matter
= card front-matter minus the placement fields). The no-team group is the
team file named `_.yaml`.

`board.yaml`, primary domain only:

```yaml
schema: 1
title: aeman board
```

`schema` is the layout version; the server refuses a repository whose
schema is newer than it knows and migrates one that is older, in a
commit.

### Ordering

Order is a **rank key** per file, in the LexoRank style: a string
compared bytewise, with room to insert between any two neighbours by
appending. Moving one card rewrites one file; nothing is renumbered;
there is no shared index file to fight over. When two neighbours have no
room left (the key would exceed a length cap), the mover rebalances the
run between the nearest neighbours that have room — a rare, bounded
rewrite in the same commit.

The same key orders teams, projects, epics, processes, tasks and
deadlines. Because it is a plain string, roster fragments from different
domains merge into one order.

## Commits

**One action, one commit.** An HTTP request or MCP call that changes N
cards — Carry Over over twelve cards — produces one commit touching N
files. That is what makes the history readable and a mistake revertible
with one command. Actions are *not* bundled per author or per time
window: that would glue unrelated edits together and make revert hit
things it should not.

The exception is the coalesced field write. Dragging a progress slider
fires many writes for one intent; the write queue already collapses them
(DeltaFIFO). Those become **one commit carrying the final value**, cut
when the coalescing window closes (500 ms after the last write to that
key). The coalescing key gains the **actor**: two people dragging the
same slider today silently overwrite each other; here they produce two
commits, both attributed.

Message format:

```
carry over 12 cards to 2026-08-28

Aeman-Action: carry-over
Aeman-Actor: kvaps
Aeman-Cards: 01JB4K2E7QZMX3R8V0N5T9WYC1 01JB4K3M8XTR…
```

The first line is for humans. The trailers are the machine-readable
part: `Aeman-Action` is the action name (today's event kinds — `create`,
`progress`, `stage`, `carry-over`, … — plus the actions that never had an
event), `Aeman-Actor` the login, `Aeman-Cards` the affected ids. A card's
activity log is the list of commits whose `Aeman-Cards` names it or
whose diff touches its file; the log line's *from → to* is read from
the diff of the card's front-matter — which is why the file format keeps
one field per line.

**Author is the user, committer is the server.** Author name is the
login; author email defaults to `<login>@users.noreply.github.com` when
the primary remote is on GitHub (so the forge UI shows the right face)
and to `<login>@aeman` otherwise; both are configurable. Committer is
the server's identity from configuration. The commit date is the action
time.

The push credential is the **server's**, not the user's: several
replicas each pushing many users' commits cannot use per-user tokens.
Authorization is ours to check at the API boundary (below).

## Storage engine

### go-git, filesystem storer, no worktree

The production image is `distroless/static`: no git binary, so the
client is **go-git** (v5.19). Objects are written and read through the
plumbing API — blob, trees along the path, commit — never through a
worktree: a checkout is a second copy of the data with nothing to do.

The clone lives in a directory under `--data` (the existing `/data`
volume that already holds the session file). Not in memory: the same
RAM, but a container restart keeps unpushed commits, and `git log` on a
live instance is inspectable — the property that made the "cache lost"
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
2. enqueue the write; the queue coalesces as today, keyed by
   `(operation, card, actor)`;
3. **commit locally** when the entry's window closes (immediately for
   actions, 500 ms for coalesced field writes) — ~1.5 ms, disk, no
   network;
4. **push in the background**: the push worker sends every unpushed
   commit in one push, with backoff and jitter on rejection.

Durability changes in one place: from step 3 on, the change is on disk,
and a crash or OOM-kill between "the user saw OK" and "the store has it"
no longer loses it — today it does, because the queue is in memory. The
existing graceful-shutdown drain (`waitDrained`) is kept and becomes
cheaper: one push instead of N mutations.

What goes away because there is no longer a gap between "what we wrote"
and "what the store returns": the replay of pending ops onto a reloaded
board (`apply`), the 90-second `recentCards`/`recentGone` guards,
provisional `local-…` ids and their aliasing (`resolvingBackend`), the
stale-while-revalidate tiers, the startup warmer (a cold clone is 2.6 s;
the warm roster is not needed). The watch hub, presence, the diff
broadcast and the coalescing queue stay.

### Remote changes

Another replica — or a person with a text editor — may push. The
server fetches on a timer (`--sync-interval`, default 15 s) and on a
`POST /api/hooks/sync` that any forge webhook can hit (no payload is
read; the call means "fetch now"). A new tip is applied to the cache by
diffing trees old→new and reloading only the touched files; the
resulting changes go out over the watch stream like any other. The
everyday fetch is a plain fetch (no depth), which brings new commits
down to what we have without adding shallow boundaries.

### Push, rejection, retry

Push rejected → fetch → if nothing new arrived, the push really failed
(log, keep the commits, retry later) → otherwise **re-apply the local
queue on the new tip**: the queue holds each card's intended final state
(that is what DeltaFIFO keeps), so re-application is "write these files
again on top of the new tree" — no merge machinery, last write wins per
card, exactly today's semantics. Then push again.

Rank keys keep concurrent reorders from colliding on a shared file;
concurrent edits to *one* card resolve last-write-wins, as they do today.
Backoff with jitter prevents the starvation the research produced in a
tight loop (one writer losing 40 races in a row).

## History

### Horizon

The clone starts at **depth 1** (the board's current state) and deepens
in the background to a **time horizon** (`--history`, default `8w`,
i.e. roughly four sprints). One knob serves both consumers of history:

- **A card's log** — the commits within the horizon that touch it.
  The API says where the horizon is (`truncatedBefore` on the log) so
  the UI can show "older history not loaded" instead of nothing.
- **State on a day** — the tree as of the last commit before that day,
  for the planned day-state replay.

Deeper on demand: a log or replay request for a date past the horizon
deepens to that date first (bounded by `--history-max`, default `1y`),
so a user who digs gets the history and everyone else does not pay for
it. The horizon is also the **memory bound** per board: the research
measured +32 MB heap for 11 600 commits of history; the defaults are
chosen to keep a board in tens of MB.

### Maintenance

Loose objects accumulate at ~8 KB per commit. A daily tick repacks and
prunes in-process. There is no gc of history: a board's history is the
product.

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

**Who sees which domain.** In self-hosted mode every visitor signs in
(OAuth, as today) and the server probes each domain with the *visitor's*
token — one advertise-refs request per domain, 0.2 s, cached per
session. Readable domains form the visitor's board; the write path
checks the target domain is among them. In local mode (`gh` token) all
configured domains are visible. The probe lives behind a small forge
interface (`CanRead(ctx, user, repo)`), which is the whole of what a
forge adapter must provide besides OAuth.

**Board identity.** A board is named by its primary repository; the
`owner`/`board` pair of the API becomes one `board` string (default: the
only configured board; `--lock-board` behaves as before). This is a
breaking change to the REST and MCP surfaces and is called out in
`docs/api.md`.

## Forge-agnostic by construction

Nothing forge-specific enters the repository: ids are ours, avatars are
resolved at read time from the login, `assignees` are logins scoped to
the board. The remote is a URL and a credential; the code never
assembles `github.com`. The forge sits behind two interfaces — "who is
this visitor" (OAuth provider) and "may they read this repository" — and
GitHub is the first implementation. GitLab, Gitea and a bare repository
on a server (with static users) are additional adapters, not redesigns.
None of them is built now; the design just does not prevent them.

## Configuration

| flag | env | default | meaning |
| --- | --- | --- | --- |
| `--repo name=url` (repeatable) | `AEMAN_REPOS` | — | the board's domains, primary first |
| — | `AEMAN_GIT_TOKEN` | — | push/fetch credential (HTTPS) |
| `--data` | `AEMAN_DATA` | `/data` | clones and session file |
| `--history` | `AEMAN_HISTORY` | `8w` | background deepening horizon |
| `--history-max` | `AEMAN_HISTORY_MAX` | `1y` | cap for on-demand deepening |
| `--sync-interval` | `AEMAN_SYNC_INTERVAL` | `15s` | fetch cadence for remote changes |
| `--committer` | `AEMAN_COMMITTER` | `aeman <aeman@localhost>` | committer identity |
| `--author-email` | `AEMAN_AUTHOR_EMAIL` | forge-dependent template | author email template, `{login}` substituted |

`--owner` and `--board <number>` disappear. `aeman mcp` takes the same
`--repo`/`--data`/`--history` flags. Everything above is documented in
`docs/api.md` under configuration.

## Migration

A one-way, idempotent command:

```
aeman migrate --owner aenix-org --board 37 \
  --repo https://github.com/aenix-org/aeman-db.git [--dry-run] [--report out.md]
```

1. **Export** the Projects v2 board in full (items, fields, bodies).
2. **Snapshot**: build every file of the layout from the current state.
   Ids are ULIDs derived deterministically from the GitHub item id
   (hash → ULID randomness bits, time = item creation) so a re-run
   produces identical files; the GitHub id is kept as `github: PVTI_…`
   in the front-matter for traceability.
3. **History**: the event lines in every draft body become **synthetic
   commits**, ordered by time, dated and authored from the event, with
   the same trailers a live action would carry. Their file changes are
   best-effort (`progress`, `stage`, `zone`, `sprint`, … applied where
   the event names a field) — the research showed the log is an
   annotation, not a journal: 1282 of 2488 cards are *not*
   reconstructible from their events (derived states logged as if
   stored, parents by title, moves without events).
4. **Truth**: the final commit writes the exact snapshot and the
   migration **verifies** that the tree equals the snapshot byte for
   byte before pushing. History is nice to have; the current state is
   not negotiable.
5. **Report**: what was dropped or approximated — the vestigial `Status`
   field (`Todo` on every card), the 7 issue cards reduced to a `link`,
   unattributed notes, events whose field could not be applied.

Idempotence: a repository that already carries the migration's final
commit for this board (marked in its message) is left alone unless
`--force`. The Projects v2 board is never written. The migration is the
last code that reads Projects v2; it stays in the tree as long as it is
useful and is deleted after.

## What changes in the code

- **New** `pkg/gitstore`: the git backend — layout read/write, commit
  building, push/fetch/deepen, repack, the history walker. Satisfies
  `boardservice.Backend`. Tested against an in-process go-git server
  transport (no git binary on CI needed) and, in one integration test
  behind a flag, against a real remote.
- **New** `cmd/aeman migrate` (reads Projects v2 via a trimmed copy of
  the export query, writes `pkg/gitstore`).
- **`pkg/boardservice.Backend`** changes: `LoadBoard(ctx, board string)`;
  `AppendEvent` is removed (the action is passed on the context and
  becomes the commit); the state-card setters (`SetSprintState`,
  `SetProcess`, `SetProject`, `SetPaused`, `SetAccumulate` on state
  cards) become roster operations on teams/projects/epics/processes.
  The service's `logEvent` goes; `WithActor` stays and feeds the
  author.
- **`pkg/board`**: the eight GitHub fields and `Card.Events` deleted;
  `Domain string` added to `Card`, `EpicCol`, `Process`, `Deadline` and
  the team roster; `SprintStates`/`TeamOrder`/`ProjectStates` replaced by
  a typed roster; `ColRef`/`Columns`/`OnColumn` unchanged.
- **`internal/server`**: `boardstore.go` shrinks to the cache, the
  coalescing queue, the watch hub and the diff broadcast; the commit,
  push, fetch and deepen workers are new; the OAuth flow is unchanged;
  `/api/github/` proxy removed (the SPA no longer calls it — verified,
  zero callers).
- **`pkg/apiserver`, `docs/api.md`**: `board` becomes a string; the log
  gains `truncatedBefore`; the Board resource gains `domains`.
- **Deleted**: `pkg/ghprojects`, `internal/server/startupwarm.go`,
  `resolving.go`, the provisional-id machinery.
- **`web/`**: `owner`/`board` handling → `board`; the log view shows the
  horizon; team/project creation asks for a domain when there is more
  than one; the reviewer picker filters by domain. The mirrored domain
  rules (`date.ts`, `sprint.ts`) are untouched — dates and sprints do
  not change.
- **`docs/design/behavior-matrix.md`** gets a section for the storage
  rules (below); **`docs/dates.md`** is unchanged in substance.
- **The companion plugin** drives boards through `gh` against the
  Projects schema. This design invalidates its access path entirely, not
  just its docs: it must move to the REST/MCP surface or to reading the
  repository. That cost is part of this migration's cost and is tracked
  separately.

## Rules, and the tests that pin them

Every rule below lands as a failing test first, in `pkg/gitstore` or
`pkg/boardservice`, before the code that makes it pass. The tests are
the second documentation: each names its edges.

| # | Rule | Edges the test must spell out |
| --- | --- | --- |
| G1 | A card's path is `cards/<a>/<b>/<id>.md` with `a`,`b` = the id's last two chars; the path never changes while the card exists | rename, move, re-zone, re-team: same path; two ids differing only in the tail land in different shards |
| G2 | Empty fields are omitted; unknown keys survive a rewrite | a file with `foo: bar` rewritten by a setter still has `foo: bar`; a cleared field disappears from the file |
| G3 | Derived states are never written | 100% progress writes `progress: 100`, never `stage: done`; In Progress is absent from every file |
| G4 | One action = one commit touching all its files | Carry Over of N cards → one commit, `Aeman-Cards` lists N ids, trees of N files changed; zero cards → no commit |
| G5 | Coalesced writes commit once with the final value, keyed by actor | 5 slider writes by A → 1 commit `progress: 80`; A and B interleaved → 2 commits, each attributed |
| G6 | Author = actor, committer = server, date = action time | trailers present; unattributed action → author `aeman`; email template applied |
| G7 | The activity log of a card = commits touching it within the horizon; from/to read from the front-matter diff | a commit touching two cards appears in both logs; a commit past the horizon is absent and `truncatedBefore` is set |
| G8 | The history walker stops AT the shallow boundary | depth-1 clone: log has exactly 1 entry, no error; after deepening: the boundary commit is included, its parent is not walked |
| G9 | Deepening applies `unshallow` and lands exactly at the horizon | deepen to date T: oldest reachable commit ≤ T, all commits > T present; a second deepen further back; deepening past the root leaves no shallow entry |
| G10 | Push rejection is detected by fetching, not by error type | rejected push whose fetch brings nothing → reported as failed, commits kept; rejected push whose fetch brings new commits → re-applied and pushed |
| G11 | Re-apply on a new tip is per-card last-write-wins | two writers, disjoint cards: both changes present; same card: the later re-application's content wins, history has both commits |
| G12 | Rank insertion touches one file; rebalancing is bounded to the run | insert between neighbours: one file changes; exhausted key space: only the run between the nearest roomy neighbours is rewritten, in the same commit |
| G13 | Roster fragments merge into one order across domains | teams from two domains with interleaved ranks come out interleaved; identical ranks tie-break by id |
| G14 | A domain is inherited, never chosen per card | creating a card under a project in the closed domain writes it to the closed repository; a subtask is written where its parent is |
| G15 | Mirror refuses a target column in another domain | `ErrCrossDomain`; same-domain mirror still works; the guard order (exists, not own, not mirrored, same domain) is pinned |
| G16 | The reviewer picker offers only readers of the card's domain | a login without access to the closed domain is absent; with access present |
| G17 | Unreadable domains are absent from the board, not empty | a visitor who can read one of two domains gets exactly that domain's teams, projects and cards; nothing from the other, no placeholders |
| G18 | A newer `schema` is refused; an older one is migrated in a commit | `schema: 99` → clear error at startup; `schema: 0` → one commit, `schema: 1`, files rewritten |
| G19 | Remote changes reach the cache by diff | a commit pushed from elsewhere touching one card updates that card only and is broadcast once |
| G20 | Restart keeps unpushed commits | commit, no push, reopen the store → the commit is in the queue and pushed |
| G21 | Repack keeps every object reachable | after RepackObjects+Prune every commit, tree and blob of the history still reads |
| M1 | Migration: the final tree equals the snapshot byte for byte | a card whose events contradict its state ends up as the snapshot says |
| M2 | Migration is idempotent | second run on the migrated repository is a no-op without `--force` |
| M3 | Migration ids are deterministic | the same GitHub item id yields the same ULID on every run |
| M4 | Migration reports what it dropped | the 7 issue cards, the `Status` field, unattributed notes, unapplied events are all named in the report |

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

- REST and MCP: `owner`+`board <number>` → `board <string>`; the Board
  resource shape changes; the `/api/github/` proxy is gone.
- `board.Card` loses eight fields and `Events`; `boardservice.Backend`
  changes as listed. External importers of `pkg/` (see
  `docs/embedding.md`) get a major version bump.
- The companion plugin's `gh`-based access path stops working.
- A board can no longer be viewed in the GitHub Projects UI. This is
  accepted: nobody is using it.

## Decisions taken here, for the record

- Names, not ids, for `team`/`project`/`epic` references in cards (API
  compatibility; one commit per rename anyway).
- Ids for `parent`/`reviewOf`/`task` (they are ids today).
- One file per team/project/epic/deadline/process, not one roster file
  (concurrent sprint-pointer updates per team are the common write).
- Notes in the body with an explicit ULID per note (stable ids across
  edits; readable in a diff).
- No archive directory for closed sprints: it would be a mass rename,
  and the horizon already bounds what the server holds.
- Filesystem storer over in-memory (restart survival, inspectability;
  performance is not the bottleneck either way).
- HTTPS + token over SSH (speed, no host keys, forge-agnostic).
