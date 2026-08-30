# aeman API and MCP server

aeman exposes its board service three ways: the embedded UI, a JSON HTTP API under `/api/v1`, and an MCP (Model Context Protocol) server for AI agents. All three call the same board logic over a shared in-memory store, so they behave identically — and every change any of them makes is committed to the board's git repository and pushed to all connected clients over the WebSocket watch stream.

The API is Kubernetes-style: a small set of **resources** (`Board`, `Card`, `Sprint`, `Note`, `Ordering`) shaped as `{kind, metadata, spec, status}`, LIST with selectors that reproduce the UI's views, PATCH for edits, and **actions** for everything with board-level rules. Clients state intent; the server applies the rules (clamps, review links, the date model of [dates.md](dates.md)) and streams the results. The design rationale lives in [design/api-redesign.md](design/api-redesign.md); the storage in [design/git-backend.md](design/git-backend.md).

## Authentication and access

The server holds **a credential of its own** — `AEMAN_GIT_TOKEN`, or `AEMAN_GIT_TOKEN_<NAME>` per repository — for fetching and pushing the board's repositories, for the membership checks behind the assignee pickers, and for resolving issue/PR titles named in card descriptions. A board spanning two organisations gives each repository its own token: one narrow enough for either cannot reach both. Visitors never push; the server commits on their behalf, authored with their login.

The board lives on one **forge** — GitHub or GitLab (gitlab.com or self-hosted) — picked by `--forge`/`AEMAN_FORGE` or, unset, by the primary repository's host (see [Configuration](#configuration)). A visitor is identified by their forge login: from the session in the self-hosted OAuth mode (one client id/secret pair — `AEMAN_GITHUB_CLIENT_ID`/`_SECRET` or `AEMAN_GITLAB_CLIENT_ID`/`_SECRET`), or from the forge's CLI signed in on the machine in the default local mode (`gh` on GitHub, `glab` on GitLab). Access follows the visitor's **own rights on each repository** (see Domains below), asked of the forge with the visitor's token. GitHub: the repository's `permissions` block — `pull` reads, `push`/`maintain`/`admin` write. GitLab: the project's access level — Reporter (20) reads, Developer (30) and above write; a Guest sees the project but not its board; a public or internal project reads for anyone signed in. Read access to a repository shows its part of the board, write access allows changes to it. The decision is made per request from the forge and cached briefly. A visitor who cannot read the primary repository has no board at all (403); a mutation on a card whose repository the visitor cannot write is refused (403). Linking a personal board checks the same way that the visitor can push to their repository (GitHub `push`, GitLab Developer or above).

Board members — the people the assignee and reviewer pickers offer — are those who can read the repository, resolved with the server's token: GitHub asks each login's collaborator permission; GitLab reads the project's member list, inherited group members included, which also supplies display names and avatars. On GitHub an avatar is built from the login (the avatars CDN) and there are no display names; on GitLab both come from the forge's user directory, so `metadata.members[].name` is filled on GitLab boards.

The stdio MCP server (`aeman mcp`) is a local, single-user process on its own clone: it identifies the actor through the forge CLI's login (`gh api user` / `glab api user`) and pushes with `AEMAN_GIT_TOKEN`, falling back to `GITHUB_TOKEN`/`GH_TOKEN` (GitHub) or `GITLAB_TOKEN` (GitLab) and then to the CLI's own token (`gh auth token` / `glab config get token --host <host>`). In the self-hosted mode the same tool set is mounted over HTTP at `/mcp`, authenticated with per-user OAuth tokens, and the rights above apply to every tool call.

## The board

A server serves **one board**: the repositories it was started with (`--repo name=url`, repeatable, the primary first). There is no board addressing on the API — `owner`/`board` query parameters from earlier versions are ignored — and MCP tools take no board argument. The board's name is its primary repository's name.

**"Project" means aeman's own planning entity** — a group of epic columns on the Project board — never a repository or a GitHub board. `project` is a card filter (`?view=project&project=cozystack`) and the subject of its own endpoints.

### Domains

Each repository of the board is a **domain** — a visibility boundary. A card's domain is never chosen per card; it follows one rule, linked cards first: a review card lives with the card it reviews, a subtask with its parent, a process iteration with its task; otherwise a card under a project lives where the project is declared, else where its team is declared; the no-team group and anything unresolved live in the primary. A change that moves a card across that rule (re-filing it under another project, say) moves the file between repositories — and its review card and subtasks with it.

Teams, projects and processes are declared in the domain the caller picks: the optional `domain` field on `POST /projects`, `POST /processes` and `PATCH /sprints` (a team is declared by its first sprint write), default the primary. A process with a project lives with the project.

**Names are one namespace across the board.** A team, project or process name is what cards refer to (`team: portal`) and what the domain rule resolves, so the same name cannot be declared twice — in one repository or in two. A create or a rename into a name any domain already carries is refused (422), whether or not the caller can read that domain; the UI checks the same against the roster it sees before sending. Repositories that already collide when the server starts — two boards connected as one — are refused at start-up, with both files named, so the fix is one rename away. A collision that reaches the files of a running server (a direct git write, two replicas racing) resolves on read to the oldest declaration; the other is an alias whose cards still count, listed by `GET /api/healthz` for a maintainer to rename by hand.

On a board of more than one repository, `GET /board` lists the visitor's readable domains as `metadata.domains` (primary first, each with a `writable` flag and the logins that can read it) and every card carries `status.domain`; a single-repository board shows nothing of this. A domain the visitor cannot read is simply absent — no cards, teams or projects from it, no watch frames about it — not empty.

### The personal board

A person may link a repository of their own as a **personal board**: it is attached as a domain named `~<login>`, served to them alone — whatever the forge says about who else can read that repository — and cloned, committed and pushed with their own credential, never the server's. The link is `users/<login>.yaml` in the primary; the repository is attached the first time its owner shows up after a server start. Cards there are created with `personal: true` and listed by `view=personal`: every open card scheduled for today or earlier plus the ones finished today (no carry-over sweeps a personal board — a done card is seen that day and gone the next; a card planned for a later day waits until then). A personal card takes no team, column or plan band, and stays in its repository whatever it is later given. A **recurrent** personal card turns with the calendar instead of the sprint: its default cycle means every day, and reading `view=personal` is what turns the day over — a finished recurrent card whose cycle came due is reseeded as a fresh copy (0%, same title and body) before the list answers, once, never twice in one day. Design: [design/personal-board.md](design/personal-board.md).

### The planning entities

A project carries **deadlines**: a line across the grid on a given week. One project holds at most one per week, so dragging one of its lines onto another merges them — but two projects can each have something due the same week, and those are two lines.

A slot's **row is the week of its `dates.start`** — always derived, never stored beside the dates where the two could drift. `plan.week` belongs to weekly-plan cards (those created with a band and no dates); setting it on a card filed under an epic is refused (422).

A project also holds **processes** — recurring work the team keeps doing and wants to see itself doing. A process groups **tasks**; each task says what every iteration is called and says, its cycle (counted on the calendar from its start date, not from when the last iteration closed), the team whose weekly plan the iterations land in, and the standing owner. The server files what each week is owed by itself, as the week arrives — nothing to call. An open iteration holds the next one back — the stuck card *is* the process, and it goes overdue where it is — unless the task `accumulate`s, for work where unpaid months must pile up as separate cards. Every iteration is a fresh copy of the task: a renamed live card stays renamed. See [`docs/design/processes.md`](design/processes.md).

A **column** of the Project board is the pair `(project, epic)`. Epic names are unique only *within* a project, so every project can have its own `Docs` or `Auth`, and a card names both halves. Anything acting on a column — filing a card, deleting, renaming, reordering — names both.

## HTTP API

Base path: `/api/v1`. All requests and responses are JSON. Errors are returned as `{"error": "..."}` with an appropriate status code (400 bad request — including an unknown `domain`; 401 not authenticated; 403 no read access to the board or no write access to the card's domain; 404 card/note not found; 422 missing field or a rule refused the change; 502 the forge could not be reached while resolving a link).

`GET /api/v1` itself is a public, machine-readable catalog of every endpoint below.

### Resources

| Method & path | Purpose |
| --- | --- |
| `GET /api/v1/board` | The board: title, the team roster, the Project board's structure (`metadata.projects` in board order, `metadata.epics` as `{name, project}`, `metadata.deadlines` as `{week, project}`, `metadata.processes`), the people (`metadata.members`: `{login, avatarUrl}`) and the visitor's domains (`metadata.domains`). |
| `GET /api/v1/cards` | LIST cards (selectors below), in board order. A listing is the **board-row shape**: no `spec.description` — `status.links` carries the refs extracted from it (capped at 50), and the body itself is one `GET /cards/{uid}` away. `?fields=full` opts a genuine bulk reader into complete cards. |
| `POST /api/v1/cards` | Create a card (201). A title that is nothing but a GitHub issue/PR URL becomes that item's real title, with the link moved into the description (one-time, never re-synced). `personal: true` files the card on the caller's personal board instead — with a team, column or plan band it is a 422. |
| `GET /api/v1/cards/{uid}` | One card. |
| `PATCH /api/v1/cards/{uid}` | Edit spec fields; only present fields apply, empty clears. |
| `DELETE /api/v1/cards/{uid}` | Hard delete (cascades to the linked review card). |
| `GET /api/v1/cards/{uid}/links` | URLs from the card's description: GitHub issue/PR references first (resolved to their live titles and states), plain links after. Only `github.com` references are recognised; GitLab issue and merge-request links are plain links for now. |
| `GET /api/v1/cards/{uid}/log` | The card's activity feed, read from the repository's history: every commit that touched the card is one or more events (who changed the stage/progress/assignee/review/plan, when — the commit's author and time), merged chronologically with its work notes. A change made by a direct git write shows up like any other. `truncatedBefore` says when the loaded history is cut (a shallow clone); the server deepens on demand up to `--history-max`. This is one card's whole history — for a day's feed across many cards, ask `GET /logs`. |
| `GET /api/v1/logs?day=&uids=` | One day's feed for many cards at once — what the day boards show. `uids` is a comma-separated list (at most 200), `day` a board day (default today). The answer is `{kind: "DayLogList", day, cards: {uid: LogEntry[]}}` with the same entries the card log returns, oldest first: a card that was quiet that day is present with an empty list, a card the visitor cannot read is absent. The history walk stops at the day's first moment, so this costs a fraction of a log per card — asking a whole log per card is what made a board open fire dozens of slow requests. |
| `GET /api/v1/cards/{uid}/notes` | The card's work notes. |
| `POST /api/v1/cards/{uid}/notes` | Append a note `{text}` (201). |
| `PATCH /api/v1/cards/{uid}/notes/{noteId}` | Edit a note `{text}`. |
| `DELETE /api/v1/cards/{uid}/notes/{noteId}` | Delete a note. |
| `GET /api/v1/sprints` | Per-team sprint pointers. |
| `PATCH /api/v1/sprints` | Set a pointer directly `{team, current, previous, domain?}`; a team not yet declared is declared in `domain` (default the primary). |
| `POST /api/v1/projects` | Declare a project `{name, domain?}` (201) — the Project board's top grouping, which owns epic columns. It may be created empty. |
| `POST /api/v1/epics` | Declare an epic column `{name, project}` (201). The project is required and must exist; the column lives with its project. |
| `GET /api/v1/processes` | The Process tab: every process with its tasks and each task's history (`?project=` filters). |
| `POST /api/v1/processes` | Declare a process `{name, project, domain?}` (201); with a project it lives with the project. |
| `POST /api/v1/processes/tasks` | Add what a process iterates on `{process, title, description, recurrence, start, team, assignee, accumulate}` (201, returns `{uid}`). |
| `PATCH /api/v1/processes/tasks/{uid}` | Change what the NEXT iterations will be; the running one is untouched. |
| `DELETE /api/v1/processes/tasks/{uid}` | Delete a task; its past iterations stay as the record. |
| `POST /api/v1/deadlines` | Mark a week with a project's deadline `{week, project}` (201). Any day resolves to its Monday; asking twice changes nothing. |
| `GET /api/v1/ordering` | The board-level manual card order (a uid list). |
| `GET /api/v1/me/personal` | The caller's personal board: `{domain, url}`, or 404 when none is linked. |
| `PUT /api/v1/me/personal` | Link a repository as the caller's personal board `{url}`. The caller must be able to push to it (checked with the forge in the self-hosted mode: 403 otherwise); an empty repository is given a board. |
| `DELETE /api/v1/me/personal` | Unlink the caller's personal board; the repository is left as it is. |
| `GET /api/v1/watch` | WebSocket stream of resource events (below). |
| `GET /api/healthz` | Liveness and the storage's state (below). |

Note mutations return the card's full `NoteList`, so clients converge on the server's view of the thread.

### Actions

Actions carry the board rules — the client never reimplements them.

| Method & path | Body | Purpose |
| --- | --- | --- |
| `POST /api/v1/cards/{uid}/actions/remove` | `{from: "grid"\|"plan"}` | The smart ×. On a personal card: one that has been worked on (progress above 0) and did not start today is left behind on yesterday's board — `status.leftAt` set on it and its subtasks, history kept, off the personal view from today — and an untouched one, or one that started today, is deleted; re-dating a left card (`dates`, defer) brings it back. On a team card, a card has two homes — the working area (a sprint and its days) and the weekly plan (a band and its week) — and the × empties one of them. A current-sprint card that is in no plan band demotes to the previous sprint (a card in a band never demotes: `from=grid` sends it back to its band). Otherwise the card leaves that home: `from=grid` clears assignee, sprint and dates (a slot keeps its dates — they are its row), `from=plan` clears the band and, for a card that is not a slot, its week. It then lands wherever it still belongs. With nowhere else to be — no band, no column — that home was its only one, and the × deletes it, whatever the card carries: a client should ask first when there is progress or a linked review card to lose (the UI does); subtasks survive as standalone cards. A card filed under a Project-board **column** (an epic) is never deleted by either × — that column is where it goes home. A card that merely carries a project name has no column and is treated like any other. Use `DELETE /cards/{uid}` to delete deliberately. |
| `POST /api/v1/cards/{uid}/actions/move` | `{after}` | Reorder after another card (`""` = to the top). |
| `POST /api/v1/cards/{uid}/actions/defer` | `{days}` | Push the scheduled day N days ahead of today (presses stack; a card created today relocates fully). |
| `POST /api/v1/cards/{uid}/actions/in-progress` | `{}` | The implicit In Progress status. |
| `POST /api/v1/cards/{uid}/actions/mirror` | `{project, epic}` | Show the card in a second Project-board column — the same card, one file and one log, standing in both projects. The card must already be in a column; the target must exist, in the card's own repository, and must not be the card's own column; a subtask cannot be mirrored (all 422). Mirroring where it already stands is a no-op. Mirror and unmirror answer with the card resource; remove-from-project answers `{"ok": true}` — its card may no longer exist. |
| `POST /api/v1/cards/{uid}/actions/unmirror` | `{project, epic}` | Take one mirror column away; the home and everything else stay. Both halves are required (422) — a mirror always names a full pair. |
| `POST /api/v1/cards/{uid}/actions/remove-from-project` | `{project, epic}` | The Project board's ×: a mirror goes; the home with mirrors left hands its role to the first mirror; the last column drops the card from the weekly plan always, keeps a worked card (assignee + progress) as an orphan of the working area, and deletes the rest — cascading the linked review card. `project` may be empty — the no-project bucket is a real column; `epic` is required (422 without it). |
| `POST /api/v1/cards/{uid}/actions/send-to-review` | `{reviewer, day}` | Create the linked review card (201) — or reassign the existing one (200). The reviewer must be able to read the card's domain. |
| `POST /api/v1/cards/{uid}/actions/remove-reviewer` | `{}` | Delete the linked review card. |
| `POST /api/v1/cards/{uid}/actions/take-into-plan` | `{engineer, zone, day}` | Take a weekly-plan card into work. |
| `POST /api/v1/cards/{uid}/actions/release-from-plan` | `{}` | Release a card from the weekly plan — the same gesture as the × with `from=plan`, and literally the same code: a card that is somewhere else (a Project-board **column** — the epic side — or the working area by its sprint or dates) only loses its band and week; otherwise the plan was its last home and the card is deleted with its linked review card, whatever work or assignee it carries. |
| `POST /api/v1/sprints/actions/carry-over` | `{team, dryRun}` | Advance the team's sprint to today and carry its unfinished cards; finished recurrent cards reseed fresh copies. |
| `POST /api/v1/epics/actions/delete-epic` | `{epic, project}` | Delete an EMPTY column; 422 while cards still sit under it. |
| `POST /api/v1/epics/actions/reorder-epics` | `{project, epics:[...]}` | Apply one project's column order. |
| `POST /api/v1/epics/actions/rename` | `{project, epic, to}` | Rename a column in place; its cards are rewritten with it. A name already used inside the SAME project is refused (422); the same name in another project is fine. |
| `POST /api/v1/epics/actions/set-project` | `{epic, from, project}` | Move a column from one project to another; an empty target detaches it. Its cards are rewritten too — and move domains if the projects live in different ones. |
| `POST /api/v1/projects/actions/delete-project` | `{project}` | Delete an EMPTY project; 422 while it still owns columns. |
| `POST /api/v1/projects/actions/reorder-projects` | `{projects:[...]}` | Apply the shared project order. |
| `POST /api/v1/projects/actions/rename` | `{project, to}` | Rename a project in place; its columns and their cards follow. |
| `POST /api/v1/teams/actions/rename` | `{team, to}` | Rename a team in place: its declaration keeps its sprint pointer, and every card and process task that names it follows. A name another team has is refused (422); the no-team group cannot be renamed. |
| `POST /api/v1/processes/actions/delete-process` | `{process}` | Delete an EMPTY process; 422 while it has tasks. |
| `POST /api/v1/processes/actions/rename` | `{process, to}` | Rename a process; its tasks follow. |
| `POST /api/v1/processes/actions/set-project` | `{process, project}` | Move a process to another project (`""` = the no-project bucket). |
| `POST /api/v1/processes/actions/set-paused` | `{process, paused}` | Stop a process filing turns, or start it again; resuming files what the current week is owed. |
| `POST /api/v1/deadlines/actions/delete` | `{week, project}` | Clear that project's deadline on the week. |
| `POST /api/v1/deadlines/actions/move` | `{project, from, to}` | Drag its deadline to another week; landing where it already has one leaves a single line. Another project's line on that week is untouched. |

The carry actions return `{carried, reseeded}` counts; with `dryRun: true` they report the counts without changing anything — that backs the UI's confirm dialogs.

Every mutating request is **one action**: whatever it writes — a card and its review card, a column and every card under it — lands in one commit per touched repository, authored by the visitor, with `Aeman-Action`/`Aeman-Action-Id` trailers tying the commits together. Field writes that arrive in quick succession from the same visitor (a progress slider) coalesce into one commit with the final value.

### Card shape

```json
{
  "kind": "Card",
  "metadata": {
    "uid": "01JB4KA0M2P4R6T8V0X2Z4B6D8",
    "author": "octocat",
    "createdAt": "2026-06-20T10:00:00Z"
  },
  "spec": {
    "title": "Wire up the API",
    "description": "Free-form details — on full resources only: a LIST omits it (that IS the \"not loaded\" marker; a full resource always carries it, even empty)",
    "team": "platform",
    "zone": "urgent",
    "assignees": ["octocat"],
    "progress": 40,
    "stage": "review",
    "dates": { "start": "2026-07-01", "end": "2026-07-04", "sprint": "2026-07-01" },
    "plan": { "band": "wed", "week": "2026-06-29" },
    "epic": "Auth",
    "project": "cozystack",
    "reviewOf": "01JB4KA0M2P4R6T8V0X2Z4B6E1"
  },
  "status": {
    "complete": false,
    "inProgress": false,
    "reviewedBy": "lllamnyp",
    "domain": "aeman-db",
    "links": [
      { "kind": "pull", "url": "https://github.com/acme/repo/pull/7", "owner": "acme", "repo": "repo", "number": 7 }
    ]
  }
}
```

- **`metadata.uid`** is a ULID, assigned at creation and never changed by a rename, a move or a re-filing. A card migrated from GitHub Projects v2 also answers to its old `PVTI_…` id for one major version (an unknown legacy id is a 404).
- **Zones are semantic**: `urgent`, `unplanned`, `planned`, `niceToHave` (or empty). The UI's colours are presentation.
- **`spec.dates`** is the date model of [dates.md](dates.md): `start` (the scheduled day), `end` (the visible range's end), `sprint` (sprint membership). PATCHing `dates.start` runs the calendar rule — the sprint follows the sprint that was active on the start day; patch only `dates.end` or `dates.sprint` for a granular change.
- **`spec.mirrors`** lists the additional Project-board columns the same card stands in (`[{project, epic}]`) — always the card's own repository.
- **PATCH `process`** ties the card to an existing process ("" clears) — the recurring shelf's counterpart of a column. The process must live in the card's own repository; only a RECURRENT card takes a tie, and a process TURN (a card copied from a task) is refused outright (both 422): a turn's process is its task's.
- **`spec.stage`** is `locked`, `review`, `recurrent` or empty. Done is **derived** (`status.complete`): 100% with no stage. Patching `stage: "done"` clears the stage and fills 100%; review/locked clamp progress to [10, 90]. Taking a card off review cancels its unfinished linked review card server-side. Reopening a done card restores the progress it had when it was marked done.
- **`status`** is server-derived and read-only: `complete`, `inProgress`, `overdue`, `reviewedBy` (the assignee of the unfinished linked review card), `domain` (the repository the card lives in), `doneAt` (the board day the card reached 100, cleared on reopen), and `links` — the references extracted from the description (unresolved; `GET /cards/{uid}/links` resolves GitHub refs to live titles and states). `status.links` is what lets a listing drop the description without blinding a row's links indicator.

### Board shape

```json
{
  "kind": "Board",
  "metadata": {
    "title": "Ænix planning",
    "teams": ["platform", "portal"],
    "projects": ["cozystack"],
    "epics": [{ "name": "Auth", "project": "cozystack" }],
    "deadlines": [{ "week": "2026-09-07", "project": "cozystack" }],
    "members": [{ "login": "octocat", "name": "The Octocat", "avatarUrl": "https://avatars.githubusercontent.com/octocat?size=48" }],
    "domains": [
      { "name": "aeman-db", "writable": true, "members": ["octocat", "lllamnyp"] },
      { "name": "closed", "writable": false, "members": ["octocat"] }
    ],
    "teamDomains": { "founders": "closed" },
    "projectDomains": { "strategy": "closed" }
  }
}
```

`members` is everyone who can read some domain of the board plus every assignee on it; `domains[].members` is who can read that domain — the reviewer picker offers only those for a card in it. `members[].name` is the display name and is optional: GitLab supplies it from the forge's user directory (with the avatar); GitHub boards carry the login and an avatar built from it, no name. A visitor with a personal board also sees `"personal": {"domain": "~octocat", "url": "…"}` — plus `"problem"` and `"actionUrl"` when the server cannot attach the linked repository (typically: the board's GitHub App is not installed on it; the URL is the app's install page, and refusals of personal writes carry the same `actionUrl` beside `error`) and their `~octocat` entry among the domains, flagged `"personal": true`.

`teamDomains`, `projectDomains` and `processDomains` name the repository a team, a project or a process was declared in, and only for the entries outside the primary — the primary is the default, and a single-repository board sends neither. They exist because **a card's team and its project must live in the same repository**: the project decides where the card lives, so a team from another repository would leave the card where its own people cannot read it. A write that would pair them — `PATCH /cards/{uid}` with a team or a project, a create, or an epic moved between projects — is refused with **422** naming both repositories, so a client narrows its own pickers with these two maps rather than offering a choice the server will reject. A name the roster does not declare yet is not a conflict: it is declared in the card's own repository on the way.

### Log shape

```json
{
  "kind": "LogList",
  "items": [
    { "type": "event", "kind": "progress", "actor": "octocat", "from": "20", "to": "40", "at": "2026-07-06T10:00:00Z" },
    { "type": "note", "actor": "octocat", "text": "Deployed to staging", "at": "2026-07-06T11:00:00Z" }
  ],
  "truncatedBefore": "2026-05-01T00:00:00Z"
}
```

`truncatedBefore` is present only when the history is cut: the server's clone is shallow (`--history`), older commits exist on the remote and were not loaded. A request for a card created before the horizon deepens the clone back to its creation, up to `--history-max`.

### LIST selectors

`GET /api/v1/cards` reproduces the UI's views server-side:

- **No view** — defaults to the caller's personal **Me** board (their own cards in the active sprint). Who-am-I is resolved server-side (session/`gh` login), so no `user` is needed; an explicit `?user=` still wins. This is where everyone works day to day.
- `?view=all` — every card on the board (still honours the field/team filters).
- `?view=team&team=platform&day=2026-07-02` — the Team grid (the lead view) for a team on a day; `team=` accepts a comma-separated set (`team=platform,marketing`) so the multi-team board loads in one request. Day defaults to today.
- `?view=me&user=octocat&day=` — the personal day view for a specific user (empty user = the caller).
- `?view=personal` — the caller's personal board: every open card in their personal repository plus the ones finished today (`status.doneAt` is today). A card scheduled for a later day — dated so, or deferred there — is absent until that day, so the defer and the `dates` patch plan a personal card ahead; it joins no sprint on the way. Empty for a caller without a linked repository. The owner's read turns the board's day over — as of the real today, whatever `day` is asked for: finished recurrent cards whose cycle is due are reseeded first — a fresh copy at 0% per card, never twice in one day — so the list already holds them; a reseed that fails fails the request. `day` is a lens on the board (what tomorrow holds, what yesterday held), not a turn of its day: looking at tomorrow creates nothing early. Reading as someone else (`user=`) never reseeds. A card the × left behind (`status.leftAt`, see the remove action) is listed on that day and before, not after.
- `?view=project&project=cozystack` — the **Project board**: every card filed under one project's epic columns, all weeks at once (the client lays the weeks × epics table out itself). A card counts as filed by its own `epic` — a SUBTASK that carries one is delivered on that merit, not as a rider of its parent, because the parent commonly has no column at all (S4); a subtask without an epic belongs to no Project board and is not delivered. Without `project=` it is every project, including columns that belong to none.
- `?view=weekly&team=platform&week=2026-06-29` — the weekly plan (week = a Monday, defaults to the current week); the response also carries `weekly: {progress}` (recurrent cards excluded).
- Field selectors — `stage=`, `zone=`, `assignee=` — compose with a view or apply to all cards.
- `focus=true` — keep only cards workable right now (drops done, on-review and locked); the "what can I pick up now" filter.
- `reviews=true` — on a me/team view, append each card's linked review card so a client rendering the reviewer badge has it without a second request (the UI uses this; off by default so an agent's Me list isn't padded with review cards).
- `fields=full` — complete cards with descriptions, for genuine bulk readers (analytics over card bodies). The default is the board-row shape: reading one card's body is `GET /cards/{uid}`, not a fatter list.
- On the me / all lists, `team=` filters by a comma-separated set (`team=marketing,portal`) matching any of them.

Every listing is the visitor's projection: cards in domains they cannot read are not there.

### Live updates: list + watch

Clients follow the Kubernetes list/watch pattern:

1. LIST: `GET /api/v1/cards` (+ `/sprints`) — the current state.
2. WATCH: `GET /api/v1/watch?client=<id>` — upgrade to a WebSocket; each text frame is one event:

```json
{ "type": "ADDED" | "MODIFIED" | "DELETED", "kind": "Card" | "Sprint" | "Ordering", "object": { ... } }
```

Apply Card events by `metadata.uid`; Sprint events replace a team's pointer; an Ordering event carries the full uid list to re-sort by. On reconnect, re-list to reconcile. Frames about a domain the visitor cannot read are never sent.

The optional `client` id keys **echo suppression**: send the same value in the `X-Aeman-Client` header on your own mutations and the server will not stream your own changes back on that watch connection (your optimistic state and the mutation responses already carry them).

**Scoped watch**: pass the same selector parameters as LIST (`view=`, `team=`, `stage=`, ...) and the subscription tracks that selection — a card entering it arrives as `ADDED` and one leaving it as `DELETED`, so a thin client can mirror a single view without knowing the board rules. Memberships are re-diffed when a sprint pointer moves and when the local day rolls over. `resources=cards,sprints,ordering` picks the kinds.

Changes that reach the repository from elsewhere — another aeman replica, a plugin committing directly — arrive the same way: the server fetches on its sync tick (`--sync-interval`, 15 s), reads exactly the cards the new commits touched, and streams them.

### Health

`GET /api/healthz` answers `{"status": "ok"}` and, in addition, what the storage has to say:

```json
{
  "status": "degraded",
  "unpushedAgeSeconds": 412,
  "cacheAgeSeconds": 9,
  "aliases": [{ "kind": "project", "name": "Docs", "domain": "closed", "id": "01JB…", "winner": "01JB…" }],
  "ghosts": [{ "id": "01JB…", "domain": "aeman-db", "current": "closed" }]
}
```

- `unpushedAgeSeconds` is the age of the oldest commit not yet pushed; past `--unpushed-warn` (5 min) the status is `degraded` — a push that cannot land must not be discovered a week later. Commits are never lost: they stay in the clone and are pushed when the remote is reachable again.
- `cacheAgeSeconds` is how long ago the cache was last known to be the remote — a full read, or a fetch that found nothing new. It should stay within the fetch interval; a number that keeps growing means the sync is not running, and the next visitor after a break pays for a blocking re-read.
- `aliases` names roster entries (teams, projects, processes) declared under the same name in two domains; the oldest wins, the others' cards still count. A maintainer merges them by hand.
- `ghosts` are cards left behind by a move that landed in the destination but whose source-side delete has not yet; maintenance removes them.

### Examples

```sh
# All cards, in board order
curl 'http://127.0.0.1:8765/api/v1/cards?view=all'

# The team grid for today
curl 'http://127.0.0.1:8765/api/v1/cards?view=team&team=platform'

# Create an urgent card assigned to octocat
curl -X POST 'http://127.0.0.1:8765/api/v1/cards' \
  -H 'Content-Type: application/json' \
  -d '{"title":"Fix the build","zone":"urgent","assignees":["octocat"]}'

# Bump readiness to 80%
curl -X PATCH 'http://127.0.0.1:8765/api/v1/cards/01JB4KA0M2P4R6T8V0X2Z4B6D8' \
  -H 'Content-Type: application/json' -d '{"progress":80}'

# Add a note
curl -X POST 'http://127.0.0.1:8765/api/v1/cards/01JB4KA0M2P4R6T8V0X2Z4B6D8/notes' \
  -H 'Content-Type: application/json' -d '{"text":"Deployed to staging"}'

# Preview a carry-over, then run it
curl -X POST 'http://127.0.0.1:8765/api/v1/sprints/actions/carry-over' \
  -H 'Content-Type: application/json' -d '{"team":"platform","dryRun":true}'
curl -X POST 'http://127.0.0.1:8765/api/v1/sprints/actions/carry-over' \
  -H 'Content-Type: application/json' -d '{"team":"platform"}'

# Plan a project in the closed domain: declare it there, give it a column,
# then file a card in that column — the card lands in the closed repository
curl -X POST 'http://127.0.0.1:8765/api/v1/projects' \
  -H 'Content-Type: application/json' -d '{"name":"cozystack","domain":"closed"}'
curl -X POST 'http://127.0.0.1:8765/api/v1/epics' \
  -H 'Content-Type: application/json' -d '{"name":"Auth","project":"cozystack"}'
curl -X POST 'http://127.0.0.1:8765/api/v1/cards' \
  -H 'Content-Type: application/json' \
  -d '{"title":"SSO for the console","epic":"Auth","project":"cozystack","plan":{"week":"2026-08-24"},"dates":{"end":"2026-09-11"}}'
```

## Configuration

`aeman serve` and `aeman mcp` share the storage flags; every flag has an environment variable, the flag wins.

| Flag | Environment | Default | Meaning |
| --- | --- | --- | --- |
| `--repo name=url` | `AEMAN_REPOS` (comma-separated) | — (required) | A domain of the board; repeatable, the primary first. The name is the domain's name on the API and the board's name for the primary. |
| `--forge` | `AEMAN_FORGE` | from the primary repository's host: `github.com` → `github`, a host containing `gitlab` → `gitlab`, else `github` unless `AEMAN_GITLAB_URL` is set | The forge that signs visitors in and answers who may read which repository: `github` or `gitlab`. |
| `--gitlab-url` | `AEMAN_GITLAB_URL` | `https://<host of the primary repository>` | Base URL of a self-hosted GitLab. |
| — | `AEMAN_GIT_TOKEN` | GitHub: `GITHUB_TOKEN`, `GH_TOKEN`, then `gh auth token`; GitLab: `GITLAB_TOKEN`, then `glab config get token --host <host>` | The server's own credential: fetch, push, membership checks, and resolving issue titles. Required in the OAuth mode. |
| — | `AEMAN_GIT_TOKEN_<NAME>` | `AEMAN_GIT_TOKEN` | One repository's own credential, named after its domain in `AEMAN_REPOS` — upper-cased, anything but a letter or a digit an underscore (`founders` → `AEMAN_GIT_TOKEN_FOUNDERS`). A board across two organisations holds one token per organisation; issue titles are resolved with the primary's. |
| `--data` | `AEMAN_DATA` | `/data` if it exists, else the user cache dir | Where the clones live (`<data>/repos/<name>`) and the session file. |
| `--history` | `AEMAN_HISTORY` | `2w` | How far back the history is loaded in the background after start-up. The cold start is a depth-1 clone; the log fills in behind it. |
| `--history-max` | `AEMAN_HISTORY_MAX` | `1y` | Cap for on-demand deepening when a card's log is cut by the horizon. |
| `--sync-interval` | `AEMAN_SYNC_INTERVAL` | `15s` | How often other replicas' and direct commits are fetched (and the weekly process turns filed). |
| `--unpushed-warn` | `AEMAN_UNPUSHED_WARN` | `5m` | Age of the oldest unpushed commit that turns `/api/healthz` degraded. |
| `--committer` | `AEMAN_COMMITTER` | `aeman <aeman@localhost>` | The committer identity; also the author of the server's own actions (the weekly sweep, a schema migration). |
| `--author-email` | `AEMAN_AUTHOR_EMAIL` | `{login}@aeman` | How a visitor's login becomes the commit author's email. |

`aeman serve` adds `--addr` (default `127.0.0.1:8765`), `--open` and `--verbose`; `AEMAN_TZ` is the board's day time zone. The self-hosted OAuth mode is enabled by one client id/secret pair — `AEMAN_GITHUB_CLIENT_ID`/`AEMAN_GITHUB_CLIENT_SECRET` or `AEMAN_GITLAB_CLIENT_ID`/`AEMAN_GITLAB_CLIENT_SECRET`, never both — together with `AEMAN_BASE_URL` (required; the redirect URL registered at the forge is `<AEMAN_BASE_URL>/auth/callback`), `AEMAN_SCOPES` (default `repo` on GitHub, `read_user read_api write_repository` on GitLab), `AEMAN_SESSION_FILE` and `AEMAN_SESSION_KEY`; the server credential in this mode is `AEMAN_GIT_TOKEN` — or, on GitHub, an App that mints it: `AEMAN_GITHUB_APP_ID` with `AEMAN_GITHUB_APP_KEY`/`AEMAN_GITHUB_APP_KEY_FILE`. Registering the application at either forge is walked through in [deploy.md](deploy.md).

Bootstrapping: `aeman init --repo <url> [--title …]` writes an empty board (one commit) into an unborn repository on either forge — the URL is the HTTPS clone URL, e.g. `https://gitlab.com/<group>/<project>.git`; `serve` refuses an unborn remote and names that command. A repository written by a newer aeman (a higher `schema` in `board.yaml`) is refused at start-up; an older one is migrated in one commit.

## MCP server

`aeman mcp --repo name=url` starts a Model Context Protocol server on **stdio** (the right transport for a local, single-user MCP), on its own clone of the board. In the self-hosted mode the same tool set is served over HTTP at `/mcp`. The tools are a one-to-one projection of the HTTP API — same resources, same actions, same semantic zone names, item ids called `uid`:

| Tool | Purpose |
| --- | --- |
| `get_board` | The board: team roster, the Project board's structure (`metadata.projects`, `metadata.epics`, `metadata.deadlines`), the people and the domains. |
| `list_cards` | LIST with the same selectors (`view`, `team`, `day`, `user`, `week`, `stage`, `zone`, `assignee`, `focus`); `view=personal` is your own personal board (your read turns its day over: due recurrent cards are reseeded before the list answers). Returns board ROWS (no descriptions; `status.links` carries the extracted refs). `title=<substring>` resolves a card someone mentioned by name to its uid in one cheap call; `full=true` opts a bulk reader into complete cards. No view defaults to your own Me board (who-am-i resolved server-side); `view=all` is the whole board, `view=team` the lead view. |
| `get_card` / `list_notes` / `list_links` | One card IN FULL — the detail pane, and the way to read a body after a `list_cards` row; its notes; its description links (GitHub refs resolved with titles). |
| `list_log` | The card's activity feed from the repository's history: events (stage/progress/review/plan changes with actor) + notes, one chronological list — read a card's delta instead of asking for morning reports. `truncatedBefore` says when the loaded history is cut. |
| `create_card` | Create a card (joins or starts its team's sprint; plan cards via `plan`+`week`; `personal=true` files it on your personal board). A title that is only a GitHub issue/PR URL is auto-filled from that item. |
| `update_card` | The PATCH: only provided fields apply, empty clears. The `description` is the card's shared body — and the place for reference links: include full URLs of related open PRs/issues in free form (encouraged); they are surfaced on the card and GitHub refs resolve to live titles/states (`list_links`). |
| `delete_card` / `remove_card` | Hard delete; the smart × (`from: grid\|plan`). |
| `move_card` / `defer_card` | Reorder; push the scheduled day ahead. |
| `send_to_review` / `remove_reviewer` | The review-card cycle (send reassigns when a review card exists). |
| `mirror_card` / `unmirror_card` | Show the card in a second column ({uid, project, epic}) — the same card, one file and one log, the card's own repository only — and take one away. |
| `remove_from_project` | The Project board's ×: a mirror goes; the home hands over to its first mirror; the last column keeps only a worked card, as a working-area orphan, and deletes the rest. |
| `take_into_plan` / `release_from_plan` | Weekly-plan membership. |
| `carry_over` | Advance a team's sprint and carry its unfinished day cards forward (`dryRun` reports the counts). There is no weekly counterpart: the server files each week's process turns by itself, and an unfinished plan card is a debt that stays where it was owed and shows on the current week as overdue. |
| `add_note` / `edit_note` / `delete_note` | The note thread. |
| `list_processes` | The Process tab in one call: processes, tasks, and each task's history (done / open / late per iteration). |
| `add_process` / `delete_process` / `rename_process` | The process roster inside a project; `add_process` takes an optional `domain`. |
| `set_process_project` / `set_process_paused` | Move a process between projects; pause it (it files no turns) or resume it. |
| `add_process_task` / `update_process_task` / `delete_process_task` | What a process iterates on: title, body, cycle (`week` / `2weeks` / `month` / `quarter`), start, team, owner, `accumulate`. |
| `add_deadline` / `delete_deadline` / `move_deadline` | A project's deadline lines: mark a week, clear it, drag one to another week (two of the same project on one week become one). |
| `add_project` / `delete_project` / `rename_project` / `reorder_projects` | The project roster: declare one (it may start empty; optional `domain`), delete an EMPTY one, rename one (its columns and cards follow), set the chip order. |
| `rename_team` | Rename a team in place (`team`, `to`); its cards and process tasks follow, a taken name is refused. |
| `add_epic` / `delete_epic` / `rename_epic` / `set_epic_project` | The columns of a project, each named by the `(project, epic)` pair: `add_epic` requires `project=`, `delete_epic` refuses a column with cards, `rename_epic` rewrites the column and its cards, `set_epic_project` moves one between projects (an empty target detaches it). |

The tools act on the configured board; a `board` argument, if a client passes one, is ignored. Changes made by agents are committed and streamed to every open board over the watch, like any other write; the stdio server pushes what accumulated when the client closes the pipe.

### Flags and environment

`aeman mcp` takes the storage flags of the configuration table above (including `--forge` and `--gitlab-url`) plus `--verbose`. The actor is the forge CLI's login — `gh` on GitHub, `glab` on GitLab; the push credential is `AEMAN_GIT_TOKEN`, else `GITHUB_TOKEN`/`GH_TOKEN` (GitHub) or `GITLAB_TOKEN` (GitLab), else the CLI's own token (`gh auth token` / `glab config get token --host <host>`).

Logs go to stderr, never stdout, so they never corrupt the JSON-RPC stream.

### Configuring it in an MCP client

Claude Code / Claude Desktop style config:

```json
{
  "mcpServers": {
    "aeman": {
      "command": "aeman",
      "args": ["mcp", "--repo", "aeman-db=https://github.com/acme/aeman-db.git"],
      "env": { "AEMAN_GIT_TOKEN": "ghp_..." }
    }
  }
}
```

Or add it from the command line:

```sh
claude mcp add aeman --env AEMAN_GIT_TOKEN=ghp_... -- aeman mcp --repo aeman-db=https://github.com/acme/aeman-db.git
```

The same for a board on GitLab — the forge follows the repository URL's host, so only the URL and the token change; a self-hosted GitLab whose host does not contain `gitlab` also needs `AEMAN_GITLAB_URL` (or `--forge gitlab`):

```json
{
  "mcpServers": {
    "aeman": {
      "command": "aeman",
      "args": ["mcp", "--repo", "aeman-db=https://gitlab.com/acme/aeman-db.git"],
      "env": { "AEMAN_GIT_TOKEN": "glpat-..." }
    }
  }
}
```

If you omit the token, the server falls back to the forge CLI's token (`gh auth token` / `glab config get token --host <host>`), so an authenticated `gh` or `glab` is enough for local use.
